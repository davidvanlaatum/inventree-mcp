package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// purchasing_bulk_tools.go adds F-S79's bulk purchase-order tools on top of
// internal/batch (F-S76) and catalog_bulk_tools.go's shared bulk-tool
// scaffolding (F-S77): bounded, independent metadata-only updates for
// purchase orders, ordinary purchase-order lines, and extra lines.
// bulk_update_purchase_orders and bulk_update_purchase_order_lines reuse
// update_purchase_order's and update_purchase_order_line's own field
// builders and supplier/order consistency checks unchanged, so their
// allowlists stay byte-for-byte identical.
// bulk_update_purchase_order_extra_lines reuses update_purchase_order_extra_line's
// own field builder and reference-uniqueness preflight, with one deliberate
// narrowing: order_id reassignment is not offered here, matching how
// catalog_bulk_tools.go excludes other structural/relational changes (category
// reparenting, company-role removal, supplier/manufacturer-part relinking)
// from bulk tools while keeping ordinary scalar fields available.
//
// Issue, receive, complete, hold, resume, cancellation, deletion, and
// duplication remain entirely outside all three tools. See
// docs/tool-reference.md.

const bulkReasonDuplicateExtraLineReference = "this reference is requested by more than one item in this batch for the same purchase order"

func registerPurchasingBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.purchaseOrderBulkPlanStore == nil {
		deps.purchaseOrderBulkPlanStore = mustBulkStore(batch.Options[purchaseOrderBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p purchaseOrderBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.purchaseOrderLineBulkPlanStore == nil {
		deps.purchaseOrderLineBulkPlanStore = mustBulkStore(batch.Options[purchaseOrderLineBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p purchaseOrderLineBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.purchaseOrderExtraLineBulkPlanStore == nil {
		deps.purchaseOrderExtraLineBulkPlanStore = mustBulkStore(batch.Options[purchaseOrderExtraLineBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p purchaseOrderExtraLineBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	addWriteTool(server, deps, BulkUpdatePurchaseOrdersToolName, "Bulk update purchase orders", "Plans or confirms a bounded independent-item purchase-order metadata update batch, using update_purchase_order's own field allowlist. Issue, receive, complete, hold, resume, cancellation, and deletion are not supported here.", bulkUpdatePurchaseOrders(deps))
	addWriteTool(server, deps, BulkUpdatePurchaseOrderLinesToolName, "Bulk update purchase order lines", "Plans or confirms a bounded independent-item purchase-order-line metadata update batch, using update_purchase_order_line's own field allowlist and supplier consistency checks. Receiving and deletion are not supported here.", bulkUpdatePurchaseOrderLines(deps))
	addWriteTool(server, deps, BulkUpdatePurchaseOrderExtraLinesToolName, "Bulk update purchase order extra lines", "Plans or confirms a bounded independent-item purchase-order extra-line metadata update batch, using update_purchase_order_extra_line's own field allowlist and reference-uniqueness preflight. Moving a line to a different order, and deletion, are not supported here; use update_purchase_order_extra_line and delete_purchase_order_extra_line.", bulkUpdatePurchaseOrderExtraLines(deps))
}

func duplicateBulkStrings(keys []string) map[string]bool {
	seen := map[string]int{}
	for _, key := range keys {
		if key != "" {
			seen[key]++
		}
	}
	dup := map[string]bool{}
	for key, count := range seen {
		if count > 1 {
			dup[key] = true
		}
	}
	return dup
}

// ---------------------------------------------------------------------------
// Purchase orders
// ---------------------------------------------------------------------------

type PurchaseOrderBulkUpdateClient interface {
	GetPurchaseOrderDetail(context.Context, int) (inventree.PurchaseOrderDetail, error)
	UpdatePurchaseOrderDetail(context.Context, int, inventree.PatchFields) (inventree.PurchaseOrderDetail, error)
	GetStockLocation(context.Context, int) (inventree.StockLocation, error)
}

type BulkUpdatePurchaseOrderItem struct {
	ID                int     `json:"id" jsonschema:"Stable purchase-order primary key."`
	Description       *string `json:"description,omitempty" jsonschema:"Optional replacement order description, including an explicit empty string."`
	Notes             *string `json:"notes,omitempty" jsonschema:"Optional replacement Markdown notes."`
	ClearNotes        bool    `json:"clear_notes,omitempty" jsonschema:"Explicitly PATCH notes to null; mutually exclusive with notes."`
	SupplierReference *string `json:"supplier_reference,omitempty" jsonschema:"Optional replacement supplier or external order identifier, including an explicit empty string."`
	CreationDate      *string `json:"creation_date,omitempty" jsonschema:"Optional replacement creation date in YYYY-MM-DD form."`
	StartDate         *string `json:"start_date,omitempty" jsonschema:"Optional replacement scheduled start date in YYYY-MM-DD form."`
	ClearStartDate    bool    `json:"clear_start_date,omitempty" jsonschema:"Explicitly PATCH start_date to null; mutually exclusive with start_date."`
	TargetDate        *string `json:"target_date,omitempty" jsonschema:"Optional replacement target delivery date in YYYY-MM-DD form."`
	ClearTargetDate   bool    `json:"clear_target_date,omitempty" jsonschema:"Explicitly PATCH target_date to null; mutually exclusive with target_date."`
	Currency          *string `json:"currency,omitempty" jsonschema:"Optional replacement order currency, including an explicit empty string to use the supplier default."`
	DestinationID     *int    `json:"destination_id,omitempty" jsonschema:"Optional replacement receiving stock-location primary key."`
	ClearDestination  bool    `json:"clear_destination,omitempty" jsonschema:"Explicitly PATCH destination to null; mutually exclusive with destination_id."`
	Link              *string `json:"link,omitempty" jsonschema:"Optional replacement complete HTTP(S) link without userinfo; an explicit empty string clears it."`
}

type BulkUpdatePurchaseOrdersInput struct {
	Items    []BulkUpdatePurchaseOrderItem `json:"items" jsonschema:"Ordered batch of independent purchase-order updates, 1 to 25 items."`
	DryRun   bool                          `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                          `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                        `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type purchaseOrderBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.PurchaseOrderDetail
	Fields inventree.PatchFields
}

type purchaseOrderBulkPlan struct {
	Items []purchaseOrderBulkPlanItem
}

func (p purchaseOrderBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p purchaseOrderBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

// purchaseOrderValues projects the touched-by-bulk_update_purchase_orders
// fields into the same map[string]any shape patchMatches/valuesMatchForKeys
// compare against, mirroring partValues/companyValues.
func purchaseOrderValues(record inventree.PurchaseOrderDetail) map[string]any {
	return map[string]any{
		"description": record.Description, "supplier_reference": record.SupplierReference,
		"order_currency": record.OrderCurrency, "link": record.Link, "creation_date": record.CreationDate,
		"notes": record.Notes, "start_date": record.StartDate, "target_date": record.TargetDate,
		"destination": record.Destination,
	}
}

func buildPurchaseOrderBulkPlan(ctx context.Context, client PurchaseOrderBulkUpdateClient, items []BulkUpdatePurchaseOrderItem) purchaseOrderBulkPlan {
	plan := purchaseOrderBulkPlan{Items: make([]purchaseOrderBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildPurchaseOrderBulkPlanItem(ctx, client, item)
	}
	dup := duplicateBulkIDs(plan.ids())
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
	}
	return plan
}

func buildPurchaseOrderBulkPlanItem(ctx context.Context, client PurchaseOrderBulkUpdateClient, item BulkUpdatePurchaseOrderItem) purchaseOrderBulkPlanItem {
	if item.ID <= 0 {
		return purchaseOrderBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetPurchaseOrderDetail(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return purchaseOrderBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable purchase order"}}
	}
	// BulkUpdatePurchaseOrderItem mirrors UpdatePurchaseOrderInput's exact
	// field set, so this is a plain field-preserving type conversion.
	fields, err := updatePurchaseOrderPatch(UpdatePurchaseOrderInput(item))
	if err != nil {
		return purchaseOrderBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return purchaseOrderBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: before}
	}
	if item.DestinationID != nil {
		if _, err := client.GetStockLocation(ctx, *item.DestinationID); err != nil {
			return purchaseOrderBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "destination_id does not identify a readable stock location"}, Before: before}
		}
	}
	return purchaseOrderBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type purchaseOrderBulkAdapter struct {
	client PurchaseOrderBulkUpdateClient
	bulkRecordStore[inventree.PurchaseOrderDetail]
}

func (a *purchaseOrderBulkAdapter) Preflight(ctx context.Context, item purchaseOrderBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetPurchaseOrderDetail(ctx, item.ID)
	if err != nil {
		return false, bulkReasonReadFailed, err
	}
	if current.PK != item.ID {
		return false, bulkReasonIdentityMismatch, errors.New("purchase-order identity verification failed")
	}
	if patchMatches(item.Fields, purchaseOrderValues(current)) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, purchaseOrderValues(current), purchaseOrderValues(item.Before)) {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *purchaseOrderBulkAdapter) Mutate(ctx context.Context, item purchaseOrderBulkPlanItem) error {
	if _, err := a.client.UpdatePurchaseOrderDetail(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetPurchaseOrderDetail(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !patchMatches(item.Fields, purchaseOrderValues(current)) {
			return err
		}
	}
	return nil
}

func (a *purchaseOrderBulkAdapter) Verify(ctx context.Context, item purchaseOrderBulkPlanItem) error {
	current, err := a.client.GetPurchaseOrderDetail(ctx, item.ID)
	if err != nil || current.PK != item.ID || !patchMatches(item.Fields, purchaseOrderValues(current)) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, purchaseOrderDetailView(current))
	return nil
}

func bulkUpdatePurchaseOrders(deps Dependencies) mcp.ToolHandlerFor[BulkUpdatePurchaseOrdersInput, BulkUpdateOutput[inventree.PurchaseOrderDetail]] {
	return LookupHandler[PurchaseOrderBulkUpdateClient, BulkUpdatePurchaseOrdersInput, BulkUpdateOutput[inventree.PurchaseOrderDetail]](deps, BulkUpdatePurchaseOrdersToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderBulkUpdateClient, input BulkUpdatePurchaseOrdersInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.PurchaseOrderDetail], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[inventree.PurchaseOrderDetail](BulkUpdatePurchaseOrdersToolName, len(input.Items))
			}
			plan := buildPurchaseOrderBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[inventree.PurchaseOrderDetail]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[purchaseOrderBulkPlanItem, inventree.PurchaseOrderDetail](plan.Items)
				token, err := deps.purchaseOrderBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[inventree.PurchaseOrderDetail]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[inventree.PurchaseOrderDetail]()
			}
			if input.PlanHash == "" || !deps.purchaseOrderBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.PurchaseOrderDetail]()
			}
			adapter := &purchaseOrderBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[purchaseOrderBulkPlanItem, inventree.PurchaseOrderDetail](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Purchase order lines
// ---------------------------------------------------------------------------

type BulkUpdatePurchaseOrderLineItem struct {
	ID             int      `json:"id" jsonschema:"Stable purchase-order line primary key."`
	SupplierPartID *int     `json:"supplier_part_id,omitempty" jsonschema:"Optional replacement supplier-part primary key; must belong to the line's purchase-order supplier."`
	Line           *string  `json:"line,omitempty" jsonschema:"Optional replacement display line number."`
	Reference      *string  `json:"reference,omitempty" jsonschema:"Optional replacement stable line reference."`
	Notes          *string  `json:"notes,omitempty" jsonschema:"Optional replacement line notes."`
	Quantity       *float64 `json:"quantity,omitempty" jsonschema:"Optional replacement quantity. Must be greater than zero."`
	UnitPrice      *float64 `json:"unit_price,omitempty" jsonschema:"Optional replacement purchase unit price."`
	Currency       *string  `json:"currency,omitempty" jsonschema:"Optional replacement purchase price currency; required with unit_price."`
	TargetDate     *string  `json:"target_date,omitempty" jsonschema:"Optional replacement target date in YYYY-MM-DD form."`
	DestinationID  *int     `json:"destination_id,omitempty" jsonschema:"Optional replacement receiving stock-location primary key."`
	Link           *string  `json:"link,omitempty" jsonschema:"Optional replacement complete HTTP(S) link without userinfo; an explicit empty string clears it."`
	Discount       *float64 `json:"discount,omitempty" jsonschema:"Optional replacement discount value."`
}

type BulkUpdatePurchaseOrderLinesInput struct {
	Items    []BulkUpdatePurchaseOrderLineItem `json:"items" jsonschema:"Ordered batch of independent purchase-order-line updates, 1 to 25 items."`
	DryRun   bool                              `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                              `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                            `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type purchaseOrderLineBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.PurchaseOrderLineItem
	Fields inventree.PatchFields
}

type purchaseOrderLineBulkPlan struct {
	Items []purchaseOrderLineBulkPlanItem
}

func (p purchaseOrderLineBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p purchaseOrderLineBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

// purchaseOrderLineFieldsMatch compares record against fields field-by-field,
// mirroring extraLineMatchesPatch: purchase_price is stored as a
// *inventree.DecimalString upstream but is set here as a plain formatted
// string, so it needs decimalPointerMatches rather than a direct equality or
// the generic patchMatches map comparison.
func purchaseOrderLineFieldsMatch(record inventree.PurchaseOrderLineItem, fields inventree.PatchFields) bool {
	for name, field := range fields {
		value := field.Value()
		switch name {
		case "order":
			if record.Order != value.(int) {
				return false
			}
		case "part":
			if record.Part != value.(int) {
				return false
			}
		case "line":
			if record.Line != value.(string) {
				return false
			}
		case "reference":
			if record.Reference != value.(string) {
				return false
			}
		case "notes":
			if record.Notes != value.(string) {
				return false
			}
		case "quantity":
			if math.Abs(record.Quantity-value.(float64)) >= 1e-9 {
				return false
			}
		case "purchase_price":
			if value == nil {
				if record.PurchasePrice != nil {
					return false
				}
			} else if !decimalPointerMatches(stringPointer(value.(string)), record.PurchasePrice) {
				return false
			}
		case "purchase_price_currency":
			if record.PurchasePriceCurrency != value.(string) {
				return false
			}
		case "target_date":
			if value == nil {
				if record.TargetDate != nil {
					return false
				}
			} else if record.TargetDate == nil || *record.TargetDate != value.(string) {
				return false
			}
		case "destination":
			if value == nil {
				if record.Destination != nil {
					return false
				}
			} else if record.Destination == nil || *record.Destination != value.(int) {
				return false
			}
		case "link":
			if record.Link != value.(string) {
				return false
			}
		case "discount":
			if math.Abs(record.Discount-value.(float64)) >= 1e-9 {
				return false
			}
		}
	}
	return true
}

// purchaseOrderLineBeforeFields projects before's own values for exactly the
// keys touched by fields into a PatchFields value, mirroring
// categoryBeforeFields, so purchaseOrderLineFieldsMatch can also be reused to
// detect drift between the captured "before" state and fresh current state.
func purchaseOrderLineBeforeFields(before inventree.PurchaseOrderLineItem, fields inventree.PatchFields) inventree.PatchFields {
	result := inventree.PatchFields{}
	for key := range fields {
		switch key {
		case "order":
			result[key] = inventree.Set(before.Order)
		case "part":
			result[key] = inventree.Set(before.Part)
		case "line":
			result[key] = inventree.Set(before.Line)
		case "reference":
			result[key] = inventree.Set(before.Reference)
		case "notes":
			result[key] = inventree.Set(before.Notes)
		case "quantity":
			result[key] = inventree.Set(before.Quantity)
		case "purchase_price":
			if before.PurchasePrice != nil {
				result[key] = inventree.Set(string(*before.PurchasePrice))
			} else {
				result[key] = inventree.Null()
			}
		case "purchase_price_currency":
			result[key] = inventree.Set(before.PurchasePriceCurrency)
		case "target_date":
			if before.TargetDate != nil {
				result[key] = inventree.Set(*before.TargetDate)
			} else {
				result[key] = inventree.Null()
			}
		case "destination":
			if before.Destination != nil {
				result[key] = inventree.Set(*before.Destination)
			} else {
				result[key] = inventree.Null()
			}
		case "link":
			result[key] = inventree.Set(before.Link)
		case "discount":
			result[key] = inventree.Set(before.Discount)
		}
	}
	return result
}

func buildPurchaseOrderLineBulkPlan(ctx context.Context, client PurchaseOrderLineWriteClient, items []BulkUpdatePurchaseOrderLineItem) purchaseOrderLineBulkPlan {
	plan := purchaseOrderLineBulkPlan{Items: make([]purchaseOrderLineBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildPurchaseOrderLineBulkPlanItem(ctx, client, item)
	}
	dup := duplicateBulkIDs(plan.ids())
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
	}
	return plan
}

func buildPurchaseOrderLineBulkPlanItem(ctx context.Context, client PurchaseOrderLineWriteClient, item BulkUpdatePurchaseOrderLineItem) purchaseOrderLineBulkPlanItem {
	if item.ID <= 0 {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	if item.Quantity != nil && *item.Quantity <= 0 {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "quantity must be greater than zero"}}
	}
	if item.UnitPrice != nil && (item.Currency == nil || strings.TrimSpace(*item.Currency) == "") {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "currency is required when unit_price is supplied"}}
	}
	if item.TargetDate != nil && !validDate(*item.TargetDate) {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "target_date must use YYYY-MM-DD"}}
	}
	validatedLink, err := validateExternalURLPointer(item.Link)
	if err != nil {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "link must be an absolute HTTP(S) URL without userinfo or credentials"}}
	}
	line, err := client.GetPurchaseOrderLine(ctx, item.ID)
	if err != nil || line.PK != item.ID {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable purchase-order line"}}
	}
	fields := purchaseOrderLinePatch(UpdatePurchaseOrderLineInput(item), validatedLink)
	if len(fields) == 0 {
		return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved PATCH field is required"}, Before: line}
	}
	fields["order"] = inventree.Set(line.Order)
	if item.SupplierPartID == nil {
		fields["part"] = inventree.Set(line.Part)
	} else {
		order, supplierPart, err := loadOrderAndSupplierPart(ctx, client, line.Order, *item.SupplierPartID)
		if err != nil {
			return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "supplier_part_id does not identify a readable supplier part"}, Before: line}
		}
		if order.Supplier != supplierPart.Supplier {
			return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "supplier_part does not belong to the purchase-order supplier"}, Before: line}
		}
	}
	if item.DestinationID != nil {
		if _, err := client.GetStockLocation(ctx, *item.DestinationID); err != nil {
			return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "destination_id does not identify a readable stock location"}, Before: line}
		}
	}
	return purchaseOrderLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: line, Fields: fields}
}

type purchaseOrderLineBulkAdapter struct {
	client PurchaseOrderLineWriteClient
	bulkRecordStore[inventree.PurchaseOrderLineItem]
}

func (a *purchaseOrderLineBulkAdapter) Preflight(ctx context.Context, item purchaseOrderLineBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetPurchaseOrderLine(ctx, item.ID)
	if err != nil {
		return false, bulkReasonReadFailed, err
	}
	if current.PK != item.ID {
		return false, bulkReasonIdentityMismatch, errors.New("purchase-order line identity verification failed")
	}
	if purchaseOrderLineFieldsMatch(current, item.Fields) {
		return true, "already at target state", nil
	}
	if !purchaseOrderLineFieldsMatch(current, purchaseOrderLineBeforeFields(item.Before, item.Fields)) {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *purchaseOrderLineBulkAdapter) Mutate(ctx context.Context, item purchaseOrderLineBulkPlanItem) error {
	if _, err := a.client.UpdatePurchaseOrderLine(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetPurchaseOrderLine(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !purchaseOrderLineFieldsMatch(current, item.Fields) {
			return err
		}
	}
	return nil
}

func (a *purchaseOrderLineBulkAdapter) Verify(ctx context.Context, item purchaseOrderLineBulkPlanItem) error {
	current, err := a.client.GetPurchaseOrderLine(ctx, item.ID)
	if err != nil || current.PK != item.ID || !purchaseOrderLineFieldsMatch(current, item.Fields) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, sanitizePurchaseOrderLine(current))
	return nil
}

func bulkUpdatePurchaseOrderLines(deps Dependencies) mcp.ToolHandlerFor[BulkUpdatePurchaseOrderLinesInput, BulkUpdateOutput[inventree.PurchaseOrderLineItem]] {
	return LookupHandler[PurchaseOrderLineWriteClient, BulkUpdatePurchaseOrderLinesInput, BulkUpdateOutput[inventree.PurchaseOrderLineItem]](deps, BulkUpdatePurchaseOrderLinesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderLineWriteClient, input BulkUpdatePurchaseOrderLinesInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.PurchaseOrderLineItem], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[inventree.PurchaseOrderLineItem](BulkUpdatePurchaseOrderLinesToolName, len(input.Items))
			}
			plan := buildPurchaseOrderLineBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[inventree.PurchaseOrderLineItem]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[purchaseOrderLineBulkPlanItem, inventree.PurchaseOrderLineItem](plan.Items)
				token, err := deps.purchaseOrderLineBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[inventree.PurchaseOrderLineItem]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[inventree.PurchaseOrderLineItem]()
			}
			if input.PlanHash == "" || !deps.purchaseOrderLineBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.PurchaseOrderLineItem]()
			}
			adapter := &purchaseOrderLineBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[purchaseOrderLineBulkPlanItem, inventree.PurchaseOrderLineItem](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Purchase order extra lines
// ---------------------------------------------------------------------------

type BulkUpdatePurchaseOrderExtraLineItem struct {
	ID              int      `json:"id" jsonschema:"Stable purchase-order extra-line primary key."`
	Reference       *string  `json:"reference,omitempty" jsonschema:"Optional replacement nonblank reference, unique within the line's current purchase order."`
	Description     *string  `json:"description,omitempty" jsonschema:"Optional replacement description, including an explicit empty string."`
	Line            *string  `json:"line,omitempty" jsonschema:"Optional replacement supplier invoice line number, including an explicit empty string."`
	Link            *string  `json:"link,omitempty" jsonschema:"Optional replacement complete HTTP(S) link without userinfo; an explicit empty string clears it."`
	Notes           *string  `json:"notes,omitempty" jsonschema:"Optional replacement notes, including an explicit empty string."`
	Quantity        *float64 `json:"quantity,omitempty" jsonschema:"Optional replacement nonnegative quantity."`
	UnitPrice       *string  `json:"unit_price,omitempty" jsonschema:"Optional replacement exact signed unit price."`
	Currency        *string  `json:"currency,omitempty" jsonschema:"Optional replacement three-letter price currency; required with unit_price."`
	ClearUnitPrice  bool     `json:"clear_unit_price,omitempty" jsonschema:"Explicitly set price to null; conflicts with unit_price and currency."`
	TargetDate      *string  `json:"target_date,omitempty" jsonschema:"Optional replacement target date in YYYY-MM-DD form."`
	ClearTargetDate bool     `json:"clear_target_date,omitempty" jsonschema:"Explicitly set target_date to null; conflicts with target_date."`
	Discount        *float64 `json:"discount,omitempty" jsonschema:"Optional replacement discount value."`
}

type BulkUpdatePurchaseOrderExtraLinesInput struct {
	Items    []BulkUpdatePurchaseOrderExtraLineItem `json:"items" jsonschema:"Ordered batch of independent purchase-order extra-line updates, 1 to 25 items."`
	DryRun   bool                                   `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                                   `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                                 `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type purchaseOrderExtraLineBulkPlanItem struct {
	bulkPlanItemBase
	Before          inventree.PurchaseOrderExtraLine
	Fields          inventree.PatchFields
	TargetOrder     int
	TargetReference string
}

type purchaseOrderExtraLineBulkPlan struct {
	Items []purchaseOrderExtraLineBulkPlanItem
}

func (p purchaseOrderExtraLineBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p purchaseOrderExtraLineBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

// extraLineBeforeFields projects before's own values for exactly the keys
// touched by fields into a PatchFields value, mirroring
// purchaseOrderLineBeforeFields/categoryBeforeFields, so extraLineMatchesPatch
// can also be reused to detect drift between the captured "before" state and
// fresh current state.
func extraLineBeforeFields(before inventree.PurchaseOrderExtraLine, fields inventree.PatchFields) inventree.PatchFields {
	result := inventree.PatchFields{}
	for key := range fields {
		switch key {
		case "order":
			result[key] = inventree.Set(before.Order)
		case "reference":
			result[key] = inventree.Set(before.Reference)
		case "description":
			result[key] = inventree.Set(before.Description)
		case "line":
			result[key] = inventree.Set(before.Line)
		case "link":
			result[key] = inventree.Set(before.Link)
		case "notes":
			result[key] = inventree.Set(before.Notes)
		case "quantity":
			result[key] = inventree.Set(before.Quantity)
		case "price":
			if before.Price != nil {
				result[key] = inventree.Set(string(*before.Price))
			} else {
				result[key] = inventree.Null()
			}
		case "price_currency":
			result[key] = inventree.Set(before.PriceCurrency)
		case "target_date":
			if before.TargetDate != nil {
				result[key] = inventree.Set(*before.TargetDate)
			} else {
				result[key] = inventree.Null()
			}
		case "discount":
			result[key] = inventree.Set(before.Discount)
		}
	}
	return result
}

func buildPurchaseOrderExtraLineBulkPlan(ctx context.Context, client PurchaseOrderExtraLineClient, items []BulkUpdatePurchaseOrderExtraLineItem) purchaseOrderExtraLineBulkPlan {
	plan := purchaseOrderExtraLineBulkPlan{Items: make([]purchaseOrderExtraLineBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildPurchaseOrderExtraLineBulkPlanItem(ctx, client, item)
	}
	dup := duplicateBulkIDs(plan.ids())
	refKeys := make([]string, len(plan.Items))
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
		if plan.Items[i].FailReason == "" {
			refKeys[i] = fmt.Sprintf("%d\x00%s", plan.Items[i].TargetOrder, plan.Items[i].TargetReference)
		}
	}
	dupRefs := duplicateBulkStrings(refKeys)
	for i := range plan.Items {
		if plan.Items[i].FailReason == "" && refKeys[i] != "" && dupRefs[refKeys[i]] {
			plan.Items[i].FailReason = bulkReasonDuplicateExtraLineReference
		}
	}
	return plan
}

func buildPurchaseOrderExtraLineBulkPlanItem(ctx context.Context, client PurchaseOrderExtraLineClient, item BulkUpdatePurchaseOrderExtraLineItem) purchaseOrderExtraLineBulkPlanItem {
	if item.ID <= 0 {
		return purchaseOrderExtraLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetPurchaseOrderExtraLine(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return purchaseOrderExtraLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable purchase-order extra line"}}
	}
	mapped := UpdatePurchaseOrderExtraLineInput{
		ID: item.ID, Reference: item.Reference, Description: item.Description, Line: item.Line, Link: item.Link,
		Notes: item.Notes, Quantity: item.Quantity, UnitPrice: item.UnitPrice, Currency: item.Currency,
		ClearUnitPrice: item.ClearUnitPrice, TargetDate: item.TargetDate, ClearTargetDate: item.ClearTargetDate,
		Discount: item.Discount,
	}
	fields, targetOrderID, targetReference, err := prepareExtraLinePatch(mapped, before)
	if err != nil {
		return purchaseOrderExtraLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	matches, err := findExtraLinesByReference(ctx, client, targetOrderID, targetReference, item.ID)
	if err != nil {
		return purchaseOrderExtraLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "extra-line duplicate preflight failed"}, Before: before}
	}
	if len(matches) > 0 {
		return purchaseOrderExtraLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "the requested reference collides with an existing extra line in this purchase order"}, Before: before, TargetOrder: targetOrderID, TargetReference: targetReference}
	}
	return purchaseOrderExtraLineBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields, TargetOrder: targetOrderID, TargetReference: targetReference}
}

type purchaseOrderExtraLineBulkAdapter struct {
	client PurchaseOrderExtraLineClient
	bulkRecordStore[inventree.PurchaseOrderExtraLine]
}

func (a *purchaseOrderExtraLineBulkAdapter) Preflight(ctx context.Context, item purchaseOrderExtraLineBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetPurchaseOrderExtraLine(ctx, item.ID)
	if err != nil {
		return false, bulkReasonReadFailed, err
	}
	if current.PK != item.ID {
		return false, bulkReasonIdentityMismatch, errors.New("purchase-order extra-line identity verification failed")
	}
	if extraLineMatchesPatch(current, item.Fields) {
		return true, "already at target state", nil
	}
	if !extraLineMatchesPatch(current, extraLineBeforeFields(item.Before, item.Fields)) {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *purchaseOrderExtraLineBulkAdapter) Mutate(ctx context.Context, item purchaseOrderExtraLineBulkPlanItem) error {
	if _, err := a.client.UpdatePurchaseOrderExtraLine(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousCategoryMutation(err) {
			return err
		}
		current, getErr := a.client.GetPurchaseOrderExtraLine(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !extraLineMatchesPatch(current, item.Fields) {
			return err
		}
	}
	return nil
}

func (a *purchaseOrderExtraLineBulkAdapter) Verify(ctx context.Context, item purchaseOrderExtraLineBulkPlanItem) error {
	current, err := a.client.GetPurchaseOrderExtraLine(ctx, item.ID)
	if err != nil || current.PK != item.ID || !extraLineMatchesPatch(current, item.Fields) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, sanitizePurchaseOrderExtraLine(current))
	return nil
}

func bulkUpdatePurchaseOrderExtraLines(deps Dependencies) mcp.ToolHandlerFor[BulkUpdatePurchaseOrderExtraLinesInput, BulkUpdateOutput[inventree.PurchaseOrderExtraLine]] {
	return LookupHandler[PurchaseOrderExtraLineClient, BulkUpdatePurchaseOrderExtraLinesInput, BulkUpdateOutput[inventree.PurchaseOrderExtraLine]](deps, BulkUpdatePurchaseOrderExtraLinesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PurchaseOrderExtraLineClient, input BulkUpdatePurchaseOrderExtraLinesInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.PurchaseOrderExtraLine], error) {
			if len(input.Items) == 0 || len(input.Items) > bulkUpdateMaxItems {
				return bulkItemCountClarification[inventree.PurchaseOrderExtraLine](BulkUpdatePurchaseOrderExtraLinesToolName, len(input.Items))
			}
			plan := buildPurchaseOrderExtraLineBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[inventree.PurchaseOrderExtraLine]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[purchaseOrderExtraLineBulkPlanItem, inventree.PurchaseOrderExtraLine](plan.Items)
				token, err := deps.purchaseOrderExtraLineBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[inventree.PurchaseOrderExtraLine]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[inventree.PurchaseOrderExtraLine]()
			}
			if input.PlanHash == "" || !deps.purchaseOrderExtraLineBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.PurchaseOrderExtraLine]()
			}
			adapter := &purchaseOrderExtraLineBulkAdapter{client: client}
			results := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{Concurrency: bulkUpdateConcurrency})
			out.Items = bulkResults[purchaseOrderExtraLineBulkPlanItem, inventree.PurchaseOrderExtraLine](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			return TextResult(out.Status), out, nil
		})
}
