package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stock_bulk_tools.go adds F-S78's bulk stock tools on top of internal/batch
// (F-S76) and catalog_bulk_tools.go's shared bulk-tool scaffolding (F-S77):
// bounded, independent metadata and low-risk status updates for existing
// stock items. bulk_update_stock_item_metadata reuses
// update_stock_item_metadata's own stockMetadataPatch field builder
// unchanged, so its allowlist and clear-conflict semantics stay
// byte-for-byte identical. bulk_set_stock_status is a separate tool, mirroring
// set_stock_status's own tool split, because status changes use a dedicated
// upstream endpoint (ChangeStockStatus) and a mandatory audit reason rather
// than an ordinary PATCH.
//
// Scope is deliberately narrower than each single-item tool's risk surface:
// bulk_set_stock_status refuses the three high-risk write-off targets
// (Destroyed, Rejected, Lost) that set_stock_status itself flags HighRisk;
// use set_stock_status for those one at a time. Quantity, stocktake counts,
// transfers, serial identity, installation, depletion, delete-on-deplete
// policy, and deletion remain entirely outside both tools. See
// docs/tool-reference.md.

const (
	bulkReasonHighRiskStatus = "status is a high-risk write-off target excluded from bulk low-risk status updates; use set_stock_status"
	bulkReasonUnknownStatus  = "status must be one of the supported InvenTree stock status codes: 10 (OK), 50 (Attention needed), 55 (Damaged), 60 (Destroyed), 65 (Rejected), 70 (Lost), 75 (Quarantined), or 85 (Returned)"
)

func registerStockBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.stockMetadataBulkPlanStore == nil {
		deps.stockMetadataBulkPlanStore = mustBulkStore(batch.Options[stockMetadataBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p stockMetadataBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	if deps.stockStatusBulkPlanStore == nil {
		deps.stockStatusBulkPlanStore = mustBulkStore(batch.Options[stockStatusBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p stockStatusBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	addWriteTool(server, deps, BulkUpdateStockItemMetadataToolName, "Bulk update stock item metadata", "Plans or confirms a bounded independent-item stock metadata update batch, using update_stock_item_metadata's own field allowlist. Location, quantity, status, serial, ownership, source/pricing, deletion, installation, and order/build fields are not supported here.", bulkUpdateStockItemMetadata(deps))
	addWriteTool(server, deps, BulkSetStockStatusToolName, "Bulk set stock status", "Plans or confirms a bounded independent-item low-risk stock status change batch with one shared audit reason. Destroyed, Rejected, and Lost targets are excluded here; use set_stock_status for those high-risk transitions.", bulkSetStockStatus(deps))
}

// ---------------------------------------------------------------------------
// Stock item metadata
// ---------------------------------------------------------------------------

type BulkUpdateStockItemMetadataItem struct {
	ID              int     `json:"id" jsonschema:"Stable stock-item primary key."`
	Batch           *string `json:"batch,omitempty" jsonschema:"Optional replacement batch code; an explicit empty string is preserved."`
	ClearBatch      bool    `json:"clear_batch,omitempty" jsonschema:"Explicitly clear batch; mutually exclusive with batch."`
	ExpiryDate      *string `json:"expiry_date,omitempty" jsonschema:"Optional replacement expiry date in YYYY-MM-DD form."`
	ClearExpiryDate bool    `json:"clear_expiry_date,omitempty" jsonschema:"Explicitly clear expiry_date; mutually exclusive with expiry_date."`
	Packaging       *string `json:"packaging,omitempty" jsonschema:"Optional replacement packaging; an explicit empty string is preserved."`
	ClearPackaging  bool    `json:"clear_packaging,omitempty" jsonschema:"Explicitly clear packaging; mutually exclusive with packaging."`
	Notes           *string `json:"notes,omitempty" jsonschema:"Optional replacement notes; an explicit empty string is preserved."`
	ClearNotes      bool    `json:"clear_notes,omitempty" jsonschema:"Explicitly clear notes; mutually exclusive with notes."`
	Link            *string `json:"link,omitempty" jsonschema:"Optional complete HTTP(S) external link without userinfo; query parameters and fragments are preserved, and an explicit empty string clears it."`
}

type BulkUpdateStockItemMetadataInput struct {
	Items    []BulkUpdateStockItemMetadataItem `json:"items" jsonschema:"Ordered batch of independent stock-item metadata updates, 1 to 25 items."`
	DryRun   bool                              `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                              `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                            `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type stockMetadataBulkPlanItem struct {
	bulkPlanItemBase
	Before inventree.StockItem
	Fields inventree.PatchFields
}

type stockMetadataBulkPlan struct {
	Items []stockMetadataBulkPlanItem
}

func (p stockMetadataBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p stockMetadataBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

// stockMetadataBulkValues projects the touched-by-bulk_update_stock_item_metadata
// fields into the same map[string]any shape patchMatches/valuesMatchForKeys
// compare against, mirroring partValues/companyValues.
func stockMetadataBulkValues(item inventree.StockItem) map[string]any {
	return map[string]any{"batch": item.Batch, "expiry_date": item.ExpiryDate, "packaging": item.Packaging, "notes": item.Notes, "link": item.Link}
}

func buildStockMetadataBulkPlan(ctx context.Context, client StockAdminClient, items []BulkUpdateStockItemMetadataItem) stockMetadataBulkPlan {
	plan := stockMetadataBulkPlan{Items: make([]stockMetadataBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildStockMetadataBulkPlanItem(ctx, client, item)
	}
	dup := duplicateBulkIDs(plan.ids())
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
	}
	return plan
}

func buildStockMetadataBulkPlanItem(ctx context.Context, client StockAdminClient, item BulkUpdateStockItemMetadataItem) stockMetadataBulkPlanItem {
	if item.ID <= 0 {
		return stockMetadataBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetStockItem(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return stockMetadataBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable stock item"}}
	}
	mapped := UpdateStockItemMetadataInput{
		ID: item.ID, Batch: item.Batch, ClearBatch: item.ClearBatch,
		ExpiryDate: item.ExpiryDate, ClearExpiryDate: item.ClearExpiryDate,
		Packaging: item.Packaging, ClearPackaging: item.ClearPackaging,
		Notes: item.Notes, ClearNotes: item.ClearNotes, Link: item.Link,
	}
	// stockMetadataFields (not stockMetadataPatch) is used deliberately: an
	// item whose requested values already equal before must still carry real
	// target Fields so Preflight's dynamic re-check reports it as skipped
	// rather than this build step misreporting it as failed.
	fields, _, err := stockMetadataFields(mapped, before)
	if err != nil {
		return stockMetadataBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: err.Error()}, Before: before}
	}
	if len(fields) == 0 {
		return stockMetadataBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "at least one approved metadata field is required"}, Before: before}
	}
	return stockMetadataBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Fields: fields}
}

type stockMetadataBulkAdapter struct {
	client StockAdminClient
	bulkRecordStore[inventree.StockItem]
}

func (a *stockMetadataBulkAdapter) Preflight(ctx context.Context, item stockMetadataBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetStockItem(ctx, item.ID)
	if err != nil {
		return false, bulkReasonReadFailed, err
	}
	if current.PK != item.ID {
		return false, bulkReasonIdentityMismatch, errors.New("stock item identity verification failed")
	}
	if patchMatches(item.Fields, stockMetadataBulkValues(current)) {
		return true, "already at target state", nil
	}
	if !valuesMatchForKeys(item.Fields, stockMetadataBulkValues(current), stockMetadataBulkValues(item.Before)) {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *stockMetadataBulkAdapter) Mutate(ctx context.Context, item stockMetadataBulkPlanItem) error {
	if _, err := a.client.UpdateStockItem(ctx, item.ID, item.Fields); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetStockItem(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || !patchMatches(item.Fields, stockMetadataBulkValues(current)) {
			return err
		}
	}
	return nil
}

func (a *stockMetadataBulkAdapter) Verify(ctx context.Context, item stockMetadataBulkPlanItem) error {
	current, err := a.client.GetStockItem(ctx, item.ID)
	if err != nil || current.PK != item.ID || !patchMatches(item.Fields, stockMetadataBulkValues(current)) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, sanitizedStockItem(current))
	return nil
}

func bulkUpdateStockItemMetadata(deps Dependencies) mcp.ToolHandlerFor[BulkUpdateStockItemMetadataInput, BulkUpdateOutput[inventree.StockItem]] {
	return LookupHandler[StockAdminClient, BulkUpdateStockItemMetadataInput, BulkUpdateOutput[inventree.StockItem]](deps, BulkUpdateStockItemMetadataToolName,
		func(ctx context.Context, req *mcp.CallToolRequest, client StockAdminClient, input BulkUpdateStockItemMetadataInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.StockItem], error) {
			if len(input.Items) == 0 || len(input.Items) > effectiveBulkMaxItems(deps) {
				return bulkItemCountClarification[inventree.StockItem](BulkUpdateStockItemMetadataToolName, len(input.Items), effectiveBulkMaxItems(deps))
			}
			plan := buildStockMetadataBulkPlan(ctx, client, input.Items)
			out := BulkUpdateOutput[inventree.StockItem]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[stockMetadataBulkPlanItem, inventree.StockItem](plan.Items)
				token, err := deps.stockMetadataBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[inventree.StockItem]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[inventree.StockItem]()
			}
			if input.PlanHash == "" || !deps.stockMetadataBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.StockItem]()
			}
			adapter := &stockMetadataBulkAdapter{client: client}
			progress := newBulkProgressReporter(req, BulkUpdateStockItemMetadataToolName)
			results, timing := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{
				Concurrency: effectiveBulkConcurrency(deps),
				OnProgress:  func(done, total int) { progress.report(ctx, done, total) },
			})
			out.Items = bulkResults[stockMetadataBulkPlanItem, inventree.StockItem](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			out.Timing = bulkTimingEvidence(timing, len(plan.Items), effectiveBulkConcurrency(deps))
			return TextResult(out.Status), out, nil
		})
}

// ---------------------------------------------------------------------------
// Stock status
// ---------------------------------------------------------------------------

type BulkSetStockStatusItem struct {
	ID     int `json:"id" jsonschema:"Stable stock-item primary key."`
	Status int `json:"status" jsonschema:"Target low-risk InvenTree stock status code: 10 (OK), 50 (Attention needed), 55 (Damaged), 75 (Quarantined), or 85 (Returned). Destroyed (60), Rejected (65), and Lost (70) are refused; use set_stock_status."`
}

type BulkSetStockStatusInput struct {
	Items    []BulkSetStockStatusItem `json:"items" jsonschema:"Ordered batch of independent stock-status changes, 1 to 25 items."`
	Reason   string                   `json:"reason" jsonschema:"Nonblank operator audit reason recorded for every item in this batch."`
	DryRun   bool                     `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm  bool                     `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash string                   `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type stockStatusBulkPlanItem struct {
	bulkPlanItemBase
	Before       inventree.StockItem
	TargetStatus int
}

type stockStatusBulkPlan struct {
	Items  []stockStatusBulkPlanItem
	Reason string
}

func (p stockStatusBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p stockStatusBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

func buildStockStatusBulkPlan(ctx context.Context, client StockAdjustmentClient, items []BulkSetStockStatusItem) stockStatusBulkPlan {
	plan := stockStatusBulkPlan{Items: make([]stockStatusBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildStockStatusBulkPlanItem(ctx, client, item)
	}
	dup := duplicateBulkIDs(plan.ids())
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
	}
	return plan
}

func buildStockStatusBulkPlanItem(ctx context.Context, client StockAdjustmentClient, item BulkSetStockStatusItem) stockStatusBulkPlanItem {
	if item.ID <= 0 {
		return stockStatusBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	if _, ok := stockStatusNames[item.Status]; !ok {
		return stockStatusBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: bulkReasonUnknownStatus}}
	}
	if highRiskStockStatus(item.Status) {
		return stockStatusBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: bulkReasonHighRiskStatus}}
	}
	before, err := client.GetStockItem(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return stockStatusBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable stock item"}}
	}
	// An item whose target status already equals before.Status is left
	// FailReason-free so Preflight's dynamic re-check against fresh current
	// state reports it as skipped, matching stockMetadataBulkPlanItem's
	// equivalent no-op handling instead of statically failing it here.
	return stockStatusBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, TargetStatus: item.Status}
}

type stockStatusBulkAdapter struct {
	client StockAdjustmentClient
	reason string
	bulkRecordStore[inventree.StockItem]
}

func (a *stockStatusBulkAdapter) Preflight(ctx context.Context, item stockStatusBulkPlanItem) (bool, string, error) {
	if item.FailReason != "" {
		return false, item.FailReason, errors.New(item.FailReason)
	}
	current, err := a.client.GetStockItem(ctx, item.ID)
	if err != nil {
		return false, bulkReasonReadFailed, err
	}
	if current.PK != item.ID {
		return false, bulkReasonIdentityMismatch, errors.New("stock item identity verification failed")
	}
	if current.Status == item.TargetStatus {
		return true, "already at target state", nil
	}
	if current.Status != item.Before.Status {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *stockStatusBulkAdapter) Mutate(ctx context.Context, item stockStatusBulkPlanItem) error {
	if err := a.client.ChangeStockStatus(ctx, inventree.StockStatusChange{Items: []int{item.ID}, Status: item.TargetStatus, Note: a.reason}); err != nil {
		if !ambiguousAdminMutation(err) {
			return err
		}
		current, getErr := a.client.GetStockItem(ctx, item.ID)
		if getErr != nil || current.PK != item.ID || current.Status != item.TargetStatus {
			return err
		}
	}
	return nil
}

func (a *stockStatusBulkAdapter) Verify(ctx context.Context, item stockStatusBulkPlanItem) error {
	current, err := a.client.GetStockItem(ctx, item.ID)
	if err != nil || current.PK != item.ID || current.Status != item.TargetStatus {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, sanitizedStockItem(current))
	return nil
}

func bulkSetStockStatus(deps Dependencies) mcp.ToolHandlerFor[BulkSetStockStatusInput, BulkUpdateOutput[inventree.StockItem]] {
	return LookupHandler[StockAdjustmentClient, BulkSetStockStatusInput, BulkUpdateOutput[inventree.StockItem]](deps, BulkSetStockStatusToolName,
		func(ctx context.Context, req *mcp.CallToolRequest, client StockAdjustmentClient, input BulkSetStockStatusInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.StockItem], error) {
			if len(input.Items) == 0 || len(input.Items) > effectiveBulkMaxItems(deps) {
				return bulkItemCountClarification[inventree.StockItem](BulkSetStockStatusToolName, len(input.Items), effectiveBulkMaxItems(deps))
			}
			reason := strings.TrimSpace(input.Reason)
			if reason == "" {
				return bulkClarification[inventree.StockItem]("What audit reason should be recorded for this batch?", "reason", "a nonblank operator reason is required for every bulk status change", "reason")
			}
			plan := buildStockStatusBulkPlan(ctx, client, input.Items)
			plan.Reason = reason
			out := BulkUpdateOutput[inventree.StockItem]{Status: StatusOK, DryRun: input.DryRun}
			if input.DryRun {
				out.Items = bulkPreview[stockStatusBulkPlanItem, inventree.StockItem](plan.Items)
				token, err := deps.stockStatusBulkPlanStore.Issue(ctx, plan)
				if err != nil {
					if errors.Is(err, batch.ErrCapacity) {
						return bulkCapacityClarification[inventree.StockItem]()
					}
					return nil, out, err
				}
				out.PlanHash = token
				return TextResult(StatusOK), out, nil
			}
			if !input.Confirm {
				return bulkConfirmClarification[inventree.StockItem]()
			}
			if input.PlanHash == "" || !deps.stockStatusBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.StockItem]()
			}
			adapter := &stockStatusBulkAdapter{client: client, reason: reason}
			progress := newBulkProgressReporter(req, BulkSetStockStatusToolName)
			results, timing := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{
				Concurrency: effectiveBulkConcurrency(deps),
				OnProgress:  func(done, total int) { progress.report(ctx, done, total) },
			})
			out.Items = bulkResults[stockStatusBulkPlanItem, inventree.StockItem](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			out.Timing = bulkTimingEvidence(timing, len(plan.Items), effectiveBulkConcurrency(deps))
			return TextResult(out.Status), out, nil
		})
}
