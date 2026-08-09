package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PartDeleteClient is deliberately read-heavy: delete_part must report every
// known dependency before it will consider deleting a part, and refuses
// outright while any of the blocking categories below are non-empty.
//
// Pinned against InvenTree 1.4.3 (see the "part_delete" integration
// subtest), DELETE /api/part/{id}/ is far more permissive than the shape of
// this guard might suggest, and that permissiveness is exactly why the
// guard exists rather than being redundant with upstream. Upstream itself
// enforces only two things: the part must be inactive first ("Cannot delete
// this part as it is still active"), and a part currently used as a
// component in another part's BOM is protected ("Cannot delete this part
// as it is used in an assembly"). Every other relationship this tool checks
// -- stock, the part's own BOM, builds, purchase-order lines, sales-order
// lines, variants, supplier parts, manufacturer parts, parameters,
// attachments, and related-part links -- is silently permitted by InvenTree
// once the part is inactive, and pinned evidence shows the consequences
// differ by relation: deleting a part with an existing stock item also
// destroys that stock item (not merely orphans it), while deleting a part
// referenced by a purchase-order line leaves the line behind, orphaned.
// This tool therefore refuses on every one of those categories itself,
// with exact stable IDs reported up front, rather than ever risking a
// silent cascade InvenTree would otherwise allow.
type PartDeleteClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	GetPartCategory(context.Context, int) (inventree.Category, error)
	SearchPartsByQuery(context.Context, inventree.PartQuery) ([]inventree.Part, error)
	SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error)
	SearchSupplierParts(context.Context, inventree.SupplierPartQuery) ([]inventree.SupplierPart, error)
	SearchManufacturerParts(context.Context, inventree.ManufacturerPartQuery) ([]inventree.ManufacturerPart, error)
	SearchPartParameters(context.Context, inventree.PartParameterQuery) ([]inventree.Parameter, error)
	ListAttachments(context.Context, inventree.AttachmentQuery) ([]inventree.Attachment, error)
	SearchBomItems(context.Context, inventree.BomItemQuery) ([]inventree.BomItem, error)
	SearchBuilds(context.Context, inventree.BuildQuery) ([]inventree.Build, error)
	SearchPurchaseOrderLines(context.Context, inventree.PurchaseOrderLineQuery) ([]inventree.PurchaseOrderLineItem, error)
	SearchSalesOrderLines(context.Context, inventree.SalesOrderLineQuery) ([]inventree.SalesOrderLineItem, error)
	SearchPartRelations(context.Context, inventree.PartRelationQuery) ([]inventree.PartRelation, error)
	DeletePart(context.Context, int) error
}

type DeletePartInput struct {
	ID      int  `json:"id" jsonschema:"Stable InvenTree part primary key."`
	Confirm bool `json:"confirm,omitempty" jsonschema:"Required true after reviewing the exact current part and all reported dependencies."`
}

type PartDeleteOutput struct {
	Status            string                        `json:"status"`
	Record            *inventree.Part               `json:"record,omitempty"`
	Category          *inventree.Category           `json:"category,omitempty"`
	SupplierParts     []inventree.SupplierPart      `json:"supplier_parts,omitempty"`
	ManufacturerParts []inventree.ManufacturerPart  `json:"manufacturer_parts,omitempty"`
	Parameters        []inventree.Parameter         `json:"parameters,omitempty"`
	Attachments       []inventree.Attachment        `json:"attachments,omitempty"`
	RelatedParts      []inventree.PartRelation      `json:"related_parts,omitempty"`
	Blocking          *PartDeleteBlockingReferences `json:"blocking,omitempty"`
	Verified          bool                          `json:"verified,omitempty"`
	Recovered         bool                          `json:"recovered,omitempty"`
	Validation        *ValidationFailure            `json:"validation,omitempty"`
	RecoveryPlan      string                        `json:"recovery_plan,omitempty"`
	Clarification     *ClarificationResponse        `json:"clarification,omitempty"`
}

// PartDeleteBlockingReferences reports the stable IDs of every record that
// refuses this deletion, so an operator can inspect and remove them
// explicitly instead of the tool ever cascading a removal on their behalf.
type PartDeleteBlockingReferences struct {
	Active               bool  `json:"active,omitempty"`
	StockItemIDs         []int `json:"stock_item_ids,omitempty"`
	BomAsAssemblyIDs     []int `json:"bom_as_assembly_ids,omitempty"`
	BomAsComponentIDs    []int `json:"bom_as_component_ids,omitempty"`
	BuildIDs             []int `json:"build_ids,omitempty"`
	PurchaseOrderLineIDs []int `json:"purchase_order_line_ids,omitempty"`
	SalesOrderLineIDs    []int `json:"sales_order_line_ids,omitempty"`
	VariantPartIDs       []int `json:"variant_part_ids,omitempty"`
	SupplierPartIDs      []int `json:"supplier_part_ids,omitempty"`
	ManufacturerPartIDs  []int `json:"manufacturer_part_ids,omitempty"`
	ParameterIDs         []int `json:"parameter_ids,omitempty"`
	AttachmentIDs        []int `json:"attachment_ids,omitempty"`
	RelatedPartIDs       []int `json:"related_part_ids,omitempty"`
}

func (b PartDeleteBlockingReferences) empty() bool {
	return !b.Active && len(b.StockItemIDs) == 0 && len(b.BomAsAssemblyIDs) == 0 && len(b.BomAsComponentIDs) == 0 &&
		len(b.BuildIDs) == 0 && len(b.PurchaseOrderLineIDs) == 0 && len(b.SalesOrderLineIDs) == 0 &&
		len(b.VariantPartIDs) == 0 && len(b.SupplierPartIDs) == 0 && len(b.ManufacturerPartIDs) == 0 &&
		len(b.ParameterIDs) == 0 && len(b.AttachmentIDs) == 0 && len(b.RelatedPartIDs) == 0
}

func registerPartDeleteTool(server *mcp.Server, deps Dependencies) {
	addWriteTool(server, deps, DeletePartToolName, "Delete part", "Previews and explicitly deletes one unreferenced ordinary part with read-back verification.", deletePart(deps))
}

func deletePart(deps Dependencies) mcp.ToolHandlerFor[DeletePartInput, PartDeleteOutput] {
	return LookupHandler[PartDeleteClient, DeletePartInput, PartDeleteOutput](deps, DeletePartToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartDeleteClient, input DeletePartInput) (*mcp.CallToolResult, PartDeleteOutput, error) {
			if input.ID <= 0 {
				return partDeleteClarification("Which part should be deleted?", "id", "id must be positive", "id", true, nil, map[string]any{"id": input.ID})
			}
			record, err := client.GetPart(ctx, input.ID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), PartDeleteOutput{Status: StatusNotFound}, nil
			}
			if err != nil || record.PK != input.ID {
				return nil, PartDeleteOutput{}, safePartDeleteError("part delete lookup", err)
			}

			preview, err := loadPartDeletePreview(ctx, client, record)
			if err != nil {
				return nil, PartDeleteOutput{}, err
			}

			blocking, err := loadPartDeleteBlockingReferences(ctx, client, record, preview)
			if err != nil {
				return nil, PartDeleteOutput{}, err
			}
			if !blocking.empty() {
				return partDeleteUnsafeClarification(record, preview, blocking)
			}

			if !input.Confirm {
				clarification := NewClarification("Delete this unreferenced part?", "confirm", "delete_part requires confirm:true after reviewing the exact current part and all reported dependencies", "confirm", true, candidatesFor([]inventree.Part{record}), map[string]any{"id": input.ID, "confirm": true})
				return TextResult(StatusClarificationRequired), partDeletePreviewOutput(StatusClarificationRequired, record, preview, nil, &clarification), nil
			}

			mutationErr := client.DeletePart(ctx, input.ID)
			if errors.Is(mutationErr, context.Canceled) {
				return nil, PartDeleteOutput{}, context.Canceled
			}
			if errors.Is(mutationErr, context.DeadlineExceeded) {
				return nil, PartDeleteOutput{}, context.DeadlineExceeded
			}
			if mutationErr != nil {
				if validation, ok := safeValidationFailure(mutationErr); ok {
					return TextResult(StatusValidationFailed), PartDeleteOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				var apiErr *inventree.APIError
				if errors.As(mutationErr, &apiErr) && definiteStockMutationRejection(apiErr.StatusCode) {
					return nil, PartDeleteOutput{}, safePartDeleteError("part deletion", mutationErr)
				}
				// Ambiguous failure (timeout, 5xx, network error, or a
				// retryable 4xx such as 408/425/429): InvenTree may have
				// already applied the deletion before the response was
				// lost, so this must be verified by read-back rather than
				// assumed to have failed cleanly.
				return partDeleteVerify(ctx, client, record, preview, true)
			}
			return partDeleteVerify(ctx, client, record, preview, false)
		})
}

// partDeleteVerify reads the part back by its exact stable ID after a
// delete attempt and classifies the result: verified absence (success,
// optionally recovered from an ambiguous mutation response), confirmed
// survival, or an unverifiable read-back -- both of the latter two return
// partial_failure with read-before-retry guidance rather than a false
// success or silent data loss.
func partDeleteVerify(ctx context.Context, client PartDeleteClient, record inventree.Part, preview partDeletePreview, recovered bool) (*mcp.CallToolResult, PartDeleteOutput, error) {
	_, err := client.GetPart(ctx, record.PK)
	if isNotFound(err) {
		output := partDeletePreviewOutput(StatusOK, record, preview, nil, nil)
		output.Verified = true
		output.Recovered = recovered
		return TextResult(StatusOK), output, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, PartDeleteOutput{}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, PartDeleteOutput{}, context.DeadlineExceeded
	}
	if err == nil {
		return partDeletePartial(record, "part still exists after deletion; read the exact stable ID before retrying")
	}
	return partDeletePartial(record, "deletion read-back could not prove absence; read the exact stable ID before retrying")
}

type partDeletePreview struct {
	Category          *inventree.Category
	SupplierParts     []inventree.SupplierPart
	ManufacturerParts []inventree.ManufacturerPart
	Parameters        []inventree.Parameter
	Attachments       []inventree.Attachment
	RelatedParts      []inventree.PartRelation
}

func loadPartDeletePreview(ctx context.Context, client PartDeleteClient, record inventree.Part) (partDeletePreview, error) {
	var preview partDeletePreview

	if record.Category != nil && *record.Category > 0 {
		category, err := client.GetPartCategory(ctx, *record.Category)
		if err != nil {
			return partDeletePreview{}, safePartDeleteError("category context lookup", err)
		}
		preview.Category = &category
	}

	supplierParts, err := client.SearchSupplierParts(ctx, inventree.SupplierPartQuery{Part: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("supplier-part preflight", err)
	}
	preview.SupplierParts = supplierParts

	manufacturerParts, err := client.SearchManufacturerParts(ctx, inventree.ManufacturerPartQuery{Part: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("manufacturer-part preflight", err)
	}
	preview.ManufacturerParts = manufacturerParts

	parameters, err := client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("parameter preflight", err)
	}
	preview.Parameters = parameters

	attachments, err := client.ListAttachments(ctx, inventree.AttachmentQuery{ModelType: "part", ModelID: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("attachment preflight", err)
	}
	preview.Attachments = attachments

	relatedParts, err := client.SearchPartRelations(ctx, inventree.PartRelationQuery{Part: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("related-part preflight", err)
	}
	preview.RelatedParts = relatedParts

	return preview, nil
}

func loadPartDeleteBlockingReferences(ctx context.Context, client PartDeleteClient, record inventree.Part, preview partDeletePreview) (PartDeleteBlockingReferences, error) {
	blocking := PartDeleteBlockingReferences{
		// InvenTree 1.4.3 itself refuses to delete any active part; this
		// tool refuses on the same condition rather than letting an
		// otherwise-clean confirmed deletion fail with a generic upstream
		// validation error.
		Active:              record.Active,
		SupplierPartIDs:     supplierPartIDs(preview.SupplierParts),
		ManufacturerPartIDs: manufacturerPartIDs(preview.ManufacturerParts),
		ParameterIDs:        parameterIDs(preview.Parameters),
		AttachmentIDs:       attachmentIDs(preview.Attachments),
		RelatedPartIDs:      partRelationIDs(preview.RelatedParts),
	}

	stockItems, err := client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("stock preflight", err)
	}
	blocking.StockItemIDs = stockItemIDs(stockItems)

	bomAsAssembly, err := client.SearchBomItems(ctx, inventree.BomItemQuery{Part: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("bill-of-materials preflight", err)
	}
	blocking.BomAsAssemblyIDs = bomItemIDs(bomAsAssembly)

	bomAsComponent, err := client.SearchBomItems(ctx, inventree.BomItemQuery{Uses: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("bill-of-materials usage preflight", err)
	}
	blocking.BomAsComponentIDs = bomItemIDs(bomAsComponent)

	builds, err := client.SearchBuilds(ctx, inventree.BuildQuery{Part: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("build preflight", err)
	}
	blocking.BuildIDs = buildIDs(builds)

	// InvenTree's purchase-order-line list exposes a dedicated base_part
	// filter (the resolved underlying Part behind any of its supplier
	// parts), so this queries directly by record.PK instead of fanning out
	// over every supplier-part of the part.
	lines, err := client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{BasePart: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("purchase-order-line preflight", err)
	}
	blocking.PurchaseOrderLineIDs = purchaseOrderLineIDsFor(lines)

	salesOrderLines, err := client.SearchSalesOrderLines(ctx, inventree.SalesOrderLineQuery{Part: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("sales-order-line preflight", err)
	}
	blocking.SalesOrderLineIDs = salesOrderLineIDs(salesOrderLines)

	// A part that is a variant template for other parts cannot be safely
	// deleted without breaking those parts' variant relationship.
	variants, err := client.SearchPartsByQuery(ctx, inventree.PartQuery{VariantOf: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("variant-part preflight", err)
	}
	blocking.VariantPartIDs = partIDs(variants)

	return blocking, nil
}

func partDeletePreviewOutput(status string, record inventree.Part, preview partDeletePreview, blocking *PartDeleteBlockingReferences, clarification *ClarificationResponse) PartDeleteOutput {
	return PartDeleteOutput{
		Status:            status,
		Record:            &record,
		Category:          preview.Category,
		SupplierParts:     preview.SupplierParts,
		ManufacturerParts: preview.ManufacturerParts,
		Parameters:        preview.Parameters,
		Attachments:       preview.Attachments,
		RelatedParts:      preview.RelatedParts,
		Blocking:          blocking,
		Clarification:     clarification,
	}
}

func partDeleteUnsafeClarification(record inventree.Part, preview partDeletePreview, blocking PartDeleteBlockingReferences) (*mcp.CallToolResult, PartDeleteOutput, error) {
	clarification := NewClarification("Which safe action addresses this part instead of deleting it?", "blocking", "the part is still active, or stock, builds, bill-of-materials, purchase-order lines, sales-order lines, variant parts, supplier parts, manufacturer parts, parameters, attachments, or related-part links still reference it; InvenTree permits deleting an inactive part despite most of these, sometimes destroying the referencing record rather than merely orphaning it, so this tool refuses instead of risking that outcome; deactivate and remove the reported records explicitly first", "id", true, candidatesFor([]inventree.Part{record}), map[string]any{"id": record.PK})
	return TextResult(StatusClarificationRequired), partDeletePreviewOutput(StatusClarificationRequired, record, preview, &blocking, &clarification), nil
}

func partDeleteClarification(question, field, reason, retry string, hardError bool, candidates []ClarificationCandidate, retryValues map[string]any) (*mcp.CallToolResult, PartDeleteOutput, error) {
	clarification := NewClarification(question, field, reason, retry, hardError, candidates, retryValues)
	return TextResult(StatusClarificationRequired), PartDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func partDeletePartial(record inventree.Part, recovery string) (*mcp.CallToolResult, PartDeleteOutput, error) {
	return TextResult(StatusPartialFailure), PartDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: recovery}, nil
}

// safePartDeleteError preserves context.Canceled and context.DeadlineExceeded
// as identifiable sentinels rather than masking every failure behind the
// same generic message, matching the newer guarded mutation tools
// (stock_transfer_tools.go, stock_depletion_tools.go).
func safePartDeleteError(operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return fmt.Errorf("%s failed; inspect InvenTree availability and permissions before retrying", operation)
}

func stockItemIDs(items []inventree.StockItem) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.PK)
	}
	return ids
}

func bomItemIDs(items []inventree.BomItem) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.PK)
	}
	return ids
}

func buildIDs(builds []inventree.Build) []int {
	ids := make([]int, 0, len(builds))
	for _, build := range builds {
		ids = append(ids, build.PK)
	}
	return ids
}

func purchaseOrderLineIDsFor(lines []inventree.PurchaseOrderLineItem) []int {
	ids := make([]int, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.PK)
	}
	return ids
}

func salesOrderLineIDs(lines []inventree.SalesOrderLineItem) []int {
	ids := make([]int, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.PK)
	}
	return ids
}

func partIDs(parts []inventree.Part) []int {
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		ids = append(ids, part.PK)
	}
	return ids
}

func supplierPartIDs(parts []inventree.SupplierPart) []int {
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		ids = append(ids, part.PK)
	}
	return ids
}

func manufacturerPartIDs(parts []inventree.ManufacturerPart) []int {
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		ids = append(ids, part.PK)
	}
	return ids
}

func parameterIDs(parameters []inventree.Parameter) []int {
	ids := make([]int, 0, len(parameters))
	for _, parameter := range parameters {
		ids = append(ids, parameter.PK)
	}
	return ids
}

func attachmentIDs(attachments []inventree.Attachment) []int {
	ids := make([]int, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.PK)
	}
	return ids
}

func partRelationIDs(relations []inventree.PartRelation) []int {
	ids := make([]int, 0, len(relations))
	for _, relation := range relations {
		ids = append(ids, relation.PK)
	}
	return ids
}
