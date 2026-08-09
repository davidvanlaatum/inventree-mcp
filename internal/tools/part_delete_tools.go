package tools

import (
	"context"
	"fmt"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PartDeleteClient is deliberately read-heavy: delete_part must report every
// known dependency before it will consider deleting a part, and refuses
// outright while any of the blocking categories below are non-empty.
type PartDeleteClient interface {
	GetPart(context.Context, int) (inventree.Part, error)
	GetPartCategory(context.Context, int) (inventree.Category, error)
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
	Validation        *ValidationFailure            `json:"validation,omitempty"`
	RecoveryPlan      string                        `json:"recovery_plan,omitempty"`
	Clarification     *ClarificationResponse        `json:"clarification,omitempty"`
}

// PartDeleteBlockingReferences reports the stable IDs of every record that
// refused this deletion, so an operator can inspect them without the tool
// ever cascading a removal on their behalf.
type PartDeleteBlockingReferences struct {
	StockItemIDs         []int `json:"stock_item_ids,omitempty"`
	BomAsAssemblyIDs     []int `json:"bom_as_assembly_ids,omitempty"`
	BomAsComponentIDs    []int `json:"bom_as_component_ids,omitempty"`
	BuildIDs             []int `json:"build_ids,omitempty"`
	PurchaseOrderLineIDs []int `json:"purchase_order_line_ids,omitempty"`
	SalesOrderLineIDs    []int `json:"sales_order_line_ids,omitempty"`
}

func (b PartDeleteBlockingReferences) empty() bool {
	return len(b.StockItemIDs) == 0 && len(b.BomAsAssemblyIDs) == 0 && len(b.BomAsComponentIDs) == 0 &&
		len(b.BuildIDs) == 0 && len(b.PurchaseOrderLineIDs) == 0 && len(b.SalesOrderLineIDs) == 0
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
				return nil, PartDeleteOutput{}, safePartDeleteError("part delete lookup")
			}

			preview, err := loadPartDeletePreview(ctx, client, record)
			if err != nil {
				return nil, PartDeleteOutput{}, err
			}

			blocking, err := loadPartDeleteBlockingReferences(ctx, client, record, preview.SupplierParts)
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

			if err := client.DeletePart(ctx, input.ID); err != nil {
				if validation, ok := safeValidationFailure(err); ok {
					return TextResult(StatusValidationFailed), PartDeleteOutput{Status: StatusValidationFailed, Validation: validation}, nil
				}
				return nil, PartDeleteOutput{}, safePartDeleteError("part deletion")
			}

			_, err = client.GetPart(ctx, input.ID)
			if err == nil {
				return partDeletePartial(record, "part still exists after deletion; read the exact stable ID before retrying")
			}
			if !isNotFound(err) {
				return partDeletePartial(record, "deletion read-back could not prove absence; read the exact stable ID before retrying")
			}
			output := partDeletePreviewOutput(StatusOK, record, preview, nil, nil)
			output.Verified = true
			return TextResult(StatusOK), output, nil
		})
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
			return partDeletePreview{}, safePartDeleteError("category context lookup")
		}
		preview.Category = &category
	}

	supplierParts, err := client.SearchSupplierParts(ctx, inventree.SupplierPartQuery{Part: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("supplier-part preflight")
	}
	preview.SupplierParts = supplierParts

	manufacturerParts, err := client.SearchManufacturerParts(ctx, inventree.ManufacturerPartQuery{Part: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("manufacturer-part preflight")
	}
	preview.ManufacturerParts = manufacturerParts

	parameters, err := client.SearchPartParameters(ctx, inventree.PartParameterQuery{PartID: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("parameter preflight")
	}
	preview.Parameters = parameters

	attachments, err := client.ListAttachments(ctx, inventree.AttachmentQuery{ModelType: "part", ModelID: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("attachment preflight")
	}
	preview.Attachments = attachments

	relatedParts, err := client.SearchPartRelations(ctx, inventree.PartRelationQuery{Part: record.PK})
	if err != nil {
		return partDeletePreview{}, safePartDeleteError("related-part preflight")
	}
	preview.RelatedParts = relatedParts

	return preview, nil
}

func loadPartDeleteBlockingReferences(ctx context.Context, client PartDeleteClient, record inventree.Part, supplierParts []inventree.SupplierPart) (PartDeleteBlockingReferences, error) {
	var blocking PartDeleteBlockingReferences

	stockItems, err := client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("stock preflight")
	}
	blocking.StockItemIDs = stockItemIDs(stockItems)

	bomAsAssembly, err := client.SearchBomItems(ctx, inventree.BomItemQuery{Part: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("bill-of-materials preflight")
	}
	blocking.BomAsAssemblyIDs = bomItemIDs(bomAsAssembly)

	bomAsComponent, err := client.SearchBomItems(ctx, inventree.BomItemQuery{Uses: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("bill-of-materials usage preflight")
	}
	blocking.BomAsComponentIDs = bomItemIDs(bomAsComponent)

	builds, err := client.SearchBuilds(ctx, inventree.BuildQuery{Part: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("build preflight")
	}
	blocking.BuildIDs = buildIDs(builds)

	for _, supplierPart := range supplierParts {
		lines, err := client.SearchPurchaseOrderLines(ctx, inventree.PurchaseOrderLineQuery{SupplierPart: supplierPart.PK})
		if err != nil {
			return PartDeleteBlockingReferences{}, safePartDeleteError("purchase-order-line preflight")
		}
		blocking.PurchaseOrderLineIDs = append(blocking.PurchaseOrderLineIDs, purchaseOrderLineIDsFor(lines)...)
	}

	salesOrderLines, err := client.SearchSalesOrderLines(ctx, inventree.SalesOrderLineQuery{Part: record.PK})
	if err != nil {
		return PartDeleteBlockingReferences{}, safePartDeleteError("sales-order-line preflight")
	}
	blocking.SalesOrderLineIDs = salesOrderLineIDs(salesOrderLines)

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
	clarification := NewClarification("Which safe action addresses this part instead of deleting it?", "blocking", "stock, builds, bill-of-materials, purchase-order, or sales-order records still reference this part; deletion would either be rejected upstream or silently orphan those records", "id", true, candidatesFor([]inventree.Part{record}), map[string]any{"id": record.PK})
	return TextResult(StatusClarificationRequired), partDeletePreviewOutput(StatusClarificationRequired, record, preview, &blocking, &clarification), nil
}

func partDeleteClarification(question, field, reason, retry string, hardError bool, candidates []ClarificationCandidate, retryValues map[string]any) (*mcp.CallToolResult, PartDeleteOutput, error) {
	clarification := NewClarification(question, field, reason, retry, hardError, candidates, retryValues)
	return TextResult(StatusClarificationRequired), PartDeleteOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}

func partDeletePartial(record inventree.Part, recovery string) (*mcp.CallToolResult, PartDeleteOutput, error) {
	return TextResult(StatusPartialFailure), PartDeleteOutput{Status: StatusPartialFailure, Record: &record, RecoveryPlan: recovery}, nil
}

func safePartDeleteError(operation string) error {
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
