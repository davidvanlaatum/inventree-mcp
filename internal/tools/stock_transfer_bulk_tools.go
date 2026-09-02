package tools

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stock_transfer_bulk_tools.go adds F-S29's bulk_transfer_stock_items on top
// of internal/batch (F-S76) and stock_bulk_tools.go's shared scaffolding.
// Unlike InvenTree's own native /api/stock/transfer/ endpoint (which accepts
// several items in one call but, per a pinned Testcontainers spike recorded
// in docs/TASKS.md, never validates a per-item quantity against on-hand
// stock), this tool issues one native TransferStock call per item so every
// item keeps transfer_stock_item's own relationship-safety gate
// (unsafeStockTransferReason) and independent read-back verification. Scope
// is deliberately narrower than transfer_stock_item: every item in a batch
// moves its complete current quantity to one shared destination location;
// partial (split) quantities and per-item distinct destinations are not
// supported here. Use transfer_stock_item for those. See
// docs/tool-reference.md.

func registerStockTransferBulkWriteTools(server *mcp.Server, deps Dependencies) {
	if deps.stockTransferBulkPlanStore == nil {
		deps.stockTransferBulkPlanStore = mustBulkStore(batch.Options[stockTransferBulkPlan]{
			IDGenerator:  platform.RandomIDGenerator{},
			Principal:    stockPlanPrincipal,
			SupersedeKey: func(p stockTransferBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		})
	}
	addWriteTool(server, deps, BulkTransferStockItemsToolName, "Bulk transfer stock items", "Plans or confirms a bounded independent-item stock transfer batch moving each item's complete current quantity to one shared destination location. Partial quantities and per-item distinct destinations are not supported here; use transfer_stock_item.", bulkTransferStockItems(deps))
}

type BulkTransferStockItem struct {
	ID int `json:"id" jsonschema:"Stable stock-item primary key whose complete current quantity will be transferred."`
}

type BulkTransferStockItemsInput struct {
	Items                 []BulkTransferStockItem `json:"items" jsonschema:"Ordered batch of independent stock items to transfer, 1 to 25 items. Duplicate ids are rejected."`
	DestinationLocationID int                     `json:"destination_location_id" jsonschema:"Explicit existing destination stock-location primary key shared by every item in this batch."`
	Reason                string                  `json:"reason" jsonschema:"Nonblank operator audit reason recorded for every item in this batch."`
	DryRun                bool                    `json:"dry_run,omitempty" jsonschema:"Build and return the reviewed plan without writing."`
	Confirm               bool                    `json:"confirm,omitempty" jsonschema:"Required together with plan_hash to execute a reviewed plan."`
	PlanHash              string                  `json:"plan_hash,omitempty" jsonschema:"Single-use token returned by the matching dry_run:true call."`
}

type stockTransferBulkPlanItem struct {
	bulkPlanItemBase
	Before   inventree.StockItem
	Quantity string
}

type stockTransferBulkPlan struct {
	Items                 []stockTransferBulkPlanItem
	DestinationLocationID int
	Reason                string
}

func (p stockTransferBulkPlan) Digest() (string, error) { return batch.JSONDigest(p) }
func (p stockTransferBulkPlan) ids() []int {
	ids := make([]int, len(p.Items))
	for i, item := range p.Items {
		ids[i] = item.ID
	}
	return ids
}

func buildStockTransferBulkPlan(ctx context.Context, client StockTransferClient, items []BulkTransferStockItem, destinationID int) stockTransferBulkPlan {
	plan := stockTransferBulkPlan{Items: make([]stockTransferBulkPlanItem, len(items))}
	for i, item := range items {
		plan.Items[i] = buildStockTransferBulkPlanItem(ctx, client, item, destinationID)
	}
	dup := duplicateBulkIDs(plan.ids())
	for i := range plan.Items {
		if dup[plan.Items[i].ID] && plan.Items[i].FailReason == "" {
			plan.Items[i].FailReason = bulkReasonDuplicateID
		}
	}
	return plan
}

func buildStockTransferBulkPlanItem(ctx context.Context, client StockTransferClient, item BulkTransferStockItem, destinationID int) stockTransferBulkPlanItem {
	if item.ID <= 0 {
		return stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id must be positive"}}
	}
	before, err := client.GetStockItem(ctx, item.ID)
	if err != nil || before.PK != item.ID {
		return stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "id does not identify a readable stock item"}}
	}
	quantityString, normalizedQuantity, ok := normalizedStockDecimal(before.Quantity, false)
	if !ok {
		return stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: "current stock quantity is not schema-valid"}, Before: before}
	}
	before.Quantity = normalizedQuantity
	if reason := unsafeStockTransferReason(before); reason != "" {
		return stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID, FailReason: reason}, Before: before}
	}
	// An item already at destinationID is left FailReason-free so Preflight's
	// dynamic re-check against fresh current state reports it as skipped,
	// matching stockStatusBulkPlanItem's equivalent no-op handling.
	return stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: item.ID}, Before: before, Quantity: quantityString}
}

type stockTransferBulkAdapter struct {
	client                StockTransferClient
	destinationLocationID int
	reason                string
	bulkRecordStore[inventree.StockItem]
}

func (a *stockTransferBulkAdapter) Preflight(ctx context.Context, item stockTransferBulkPlanItem) (bool, string, error) {
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
	if current.Location != nil && *current.Location == a.destinationLocationID {
		return true, "already at target location", nil
	}
	_, normalizedQuantity, ok := normalizedStockDecimal(current.Quantity, false)
	if !ok {
		const reason = "current stock quantity is not schema-valid"
		return false, reason, errors.New(reason)
	}
	current.Quantity = normalizedQuantity
	if reason := unsafeStockTransferReason(current); reason != "" {
		return false, reason, errors.New(reason)
	}
	if math.Abs(current.Quantity-item.Before.Quantity) > 1e-9 || !equalIntPtr(current.Location, item.Before.Location) {
		return false, bulkReasonDrifted, errors.New("current state drifted since the plan was reviewed")
	}
	return false, "", nil
}

func (a *stockTransferBulkAdapter) Mutate(ctx context.Context, item stockTransferBulkPlanItem) error {
	err := a.client.TransferStock(ctx, inventree.StockTransfer{
		Items:    []inventree.StockAdjustmentItem{{PK: item.ID, Quantity: item.Quantity}},
		Notes:    a.reason,
		Location: a.destinationLocationID,
	})
	if err == nil {
		return nil
	}
	if !ambiguousAdminMutation(err) {
		return err
	}
	current, getErr := a.client.GetStockItem(ctx, item.ID)
	if getErr != nil || current.PK != item.ID || current.Location == nil || *current.Location != a.destinationLocationID {
		return err
	}
	return nil
}

func (a *stockTransferBulkAdapter) Verify(ctx context.Context, item stockTransferBulkPlanItem) error {
	current, err := a.client.GetStockItem(ctx, item.ID)
	if err != nil || current.PK != item.ID {
		return errors.New("read-back does not match the reviewed plan")
	}
	if current.Location == nil || *current.Location != a.destinationLocationID {
		return errors.New("read-back does not match the reviewed plan")
	}
	if math.Abs(current.Quantity-item.Before.Quantity) > 1e-9 {
		return errors.New("read-back does not match the reviewed plan")
	}
	expected := &StockTransferContext{Provenance: stockTransferProvenance(item.Before), Safety: stockTransferSafety(item.Before)}
	if !stockTransferProjectionEqual(current, expected) {
		return errors.New("read-back does not match the reviewed plan")
	}
	a.store(item.ID, sanitizedStockItem(current))
	return nil
}

func bulkTransferStockItems(deps Dependencies) mcp.ToolHandlerFor[BulkTransferStockItemsInput, BulkUpdateOutput[inventree.StockItem]] {
	return LookupHandler[StockTransferClient, BulkTransferStockItemsInput, BulkUpdateOutput[inventree.StockItem]](deps, BulkTransferStockItemsToolName,
		func(ctx context.Context, req *mcp.CallToolRequest, client StockTransferClient, input BulkTransferStockItemsInput) (*mcp.CallToolResult, BulkUpdateOutput[inventree.StockItem], error) {
			if len(input.Items) == 0 || len(input.Items) > effectiveBulkMaxItems(deps) {
				return bulkItemCountClarification[inventree.StockItem](BulkTransferStockItemsToolName, len(input.Items), effectiveBulkMaxItems(deps))
			}
			if input.DestinationLocationID <= 0 {
				return bulkClarification[inventree.StockItem]("Which explicit destination should receive every item in this batch?", "destination_location", "destination_location_id must be positive", "destination_location_id")
			}
			reason := strings.TrimSpace(input.Reason)
			if reason == "" {
				return bulkClarification[inventree.StockItem]("What audit reason should be recorded for this batch?", "reason", "a nonblank operator reason is required for every bulk stock transfer", "reason")
			}
			out := BulkUpdateOutput[inventree.StockItem]{Status: StatusOK, DryRun: input.DryRun}
			destination, err := client.GetStockLocation(ctx, input.DestinationLocationID)
			if err != nil {
				if isNotFound(err) {
					return bulkClarification[inventree.StockItem]("Which existing stock location should receive this batch?", "destination_location", "destination_location_id was not found", "destination_location_id")
				}
				return nil, out, safeToolError(ctx, err)
			}
			if destination.PK != input.DestinationLocationID {
				return nil, out, errors.New("InvenTree returned a mismatched destination-location identity")
			}
			plan := buildStockTransferBulkPlan(ctx, client, input.Items, destination.PK)
			plan.DestinationLocationID = destination.PK
			plan.Reason = reason
			if input.DryRun {
				out.Items = bulkPreview[stockTransferBulkPlanItem, inventree.StockItem](plan.Items)
				token, err := deps.stockTransferBulkPlanStore.Issue(ctx, plan)
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
			if input.PlanHash == "" || !deps.stockTransferBulkPlanStore.Consume(ctx, input.PlanHash, plan) {
				return bulkPlanClarification[inventree.StockItem]()
			}
			adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: destination.PK, reason: reason}
			progress := newBulkProgressReporter(req, BulkTransferStockItemsToolName)
			results, timing := batch.Execute(ctx, plan.Items, adapter, batch.ExecuteOptions{
				Concurrency: effectiveBulkConcurrency(deps),
				OnProgress:  func(done, total int) { progress.report(ctx, done, total) },
			})
			out.Items = bulkResults[stockTransferBulkPlanItem, inventree.StockItem](results, adapter.get)
			out.Status = bulkOutputStatus(out.Items)
			out.Timing = bulkTimingEvidence(timing, len(plan.Items), effectiveBulkConcurrency(deps))
			return TextResult(out.Status), out, nil
		})
}
