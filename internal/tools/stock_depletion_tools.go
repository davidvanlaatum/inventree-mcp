package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func depleteStockItem(deps Dependencies) mcp.ToolHandlerFor[DepleteStockItemInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, DepleteStockItemInput, StockAdjustmentOutput](deps, DepleteStockItemToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input DepleteStockItemInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			out := StockAdjustmentOutput{Status: StatusOK, DryRun: input.DryRun}
			if input.StockItemID <= 0 {
				return stockClarification(out, "Which stock item should be depleted?", "stock_item", "stock_item_id must be positive", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID})
			}
			reason := strings.TrimSpace(input.Reason)
			if reason == "" {
				return stockClarification(out, "What audit reason should be recorded for this stock depletion?", "reason", "a nonblank operator reason is required", "reason", map[string]any{"stock_item_id": input.StockItemID})
			}
			before, err := client.GetStockItem(ctx, input.StockItemID)
			if err != nil {
				if isNotFound(err) {
					out.Status = StatusNotFound
					return TextResult(StatusNotFound), out, nil
				}
				return nil, out, safeToolError(err)
			}
			if before.PK != input.StockItemID {
				return nil, out, errors.New("InvenTree returned a mismatched stock-item identity")
			}
			quantity, normalizedQuantity, ok := normalizedStockDecimal(before.Quantity, false)
			if !ok {
				return stockClarification(out, "Which positive stock item should be depleted?", "quantity", "the current stock quantity must be positive and fit the InvenTree decimal bounds", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "current_quantity": before.Quantity})
			}
			before.Quantity = normalizedQuantity
			if result, clarified, unsafe := unsafeStockDepletion(out, before); unsafe {
				return result, clarified, nil
			}
			after := snapshotStockItem(before)
			after.Quantity = 0
			plan := StockAdjustmentPlan{
				Action: DepleteStockItemToolName, Before: snapshotStockItem(before), After: after, Reason: reason,
				HighRisk: true, RiskReason: "complete depletion will delete the stock-item record", WillDelete: true,
				Depletion: stockDepletionContext(before),
			}
			return executeStockDepletion(ctx, deps.stockPlanStore, client, input, plan, quantity)
		})
}

func stockDepletionContext(item inventree.StockItem) *StockDepletionContext {
	return &StockDepletionContext{
		InStock: item.InStock, Allocated: item.Allocated, IsBuilding: item.IsBuilding, BuildID: item.Build,
		ConsumedByID: item.ConsumedBy, BelongsToID: item.BelongsTo, ParentID: item.Parent,
		InstalledItems: item.InstalledItems, ChildItems: item.ChildItems, TrackingItems: item.TrackingItems,
		OwnerID: item.Owner, SupplierPartID: item.SupplierPart, PurchaseOrderID: item.PurchaseOrder, CustomerID: item.Customer, SalesOrderID: item.SalesOrder,
	}
}

func unsafeStockDepletion(out StockAdjustmentOutput, item inventree.StockItem) (*mcp.CallToolResult, StockAdjustmentOutput, bool) {
	retry := map[string]any{"stock_item_id": item.PK, "dry_run": true}
	switch {
	case item.Serial != nil:
		retry["serial"] = item.Serial
		result, clarified, _ := stockClarification(out, "How should the serialized stock item be handled?", "serial", "serialized stock cannot be depleted through this deletion workflow", "stock_item_id", retry)
		return result, clarified, true
	case !item.DeleteOnDeplete:
		result, clarified, _ := stockClarification(out, "Which delete-on-deplete stock item should be removed?", "delete_on_deplete", "the selected stock item does not have delete_on_deplete enabled", "stock_item_id", retry)
		return result, clarified, true
	case !item.InStock:
		result, clarified, _ := stockClarification(out, "Which available stock item should be removed?", "in_stock", "the selected stock item is not currently in stock", "stock_item_id", retry)
		return result, clarified, true
	case item.Allocated == nil:
		result, clarified, _ := stockClarification(out, "What is the current stock allocation state?", "allocated", "allocation context is unavailable, so safe depletion cannot be proven", "stock_item_id", retry)
		return result, clarified, true
	case *item.Allocated != 0:
		retry["allocated"] = *item.Allocated
		result, clarified, _ := stockClarification(out, "How should the allocated stock be released first?", "allocated", "allocated stock cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	case item.IsBuilding:
		result, clarified, _ := stockClarification(out, "How should the in-production stock item be completed or cancelled first?", "is_building", "stock currently being built cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	case item.ConsumedBy != nil:
		retry["consumed_by_id"] = *item.ConsumedBy
		result, clarified, _ := stockClarification(out, "How should the consumed stock history be handled?", "consumed_by", "stock consumed by a build cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	case item.BelongsTo != nil:
		retry["belongs_to_id"] = *item.BelongsTo
		result, clarified, _ := stockClarification(out, "How should the installed stock item be uninstalled first?", "belongs_to", "stock installed in another item cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	case item.Parent != nil:
		retry["parent_id"] = *item.Parent
		result, clarified, _ := stockClarification(out, "How should the child stock relationship be removed first?", "parent", "a child stock item cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	case item.InstalledItems == nil:
		result, clarified, _ := stockClarification(out, "What is the current installed-item state?", "installed_items", "installed-item context is unavailable, so safe depletion cannot be proven", "stock_item_id", retry)
		return result, clarified, true
	case *item.InstalledItems != 0:
		retry["installed_items"] = *item.InstalledItems
		result, clarified, _ := stockClarification(out, "How should installed child items be removed first?", "installed_items", "stock containing installed items cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	case item.ChildItems == nil:
		result, clarified, _ := stockClarification(out, "What is the current child-item state?", "child_items", "child-item context is unavailable, so safe depletion cannot be proven", "stock_item_id", retry)
		return result, clarified, true
	case *item.ChildItems != 0:
		retry["child_items"] = *item.ChildItems
		result, clarified, _ := stockClarification(out, "How should child stock items be separated first?", "child_items", "stock with child items cannot be depleted and deleted", "stock_item_id", retry)
		return result, clarified, true
	default:
		return nil, out, false
	}
}

func executeStockDepletion(ctx context.Context, store *stockPlanStore, client StockAdjustmentClient, input DepleteStockItemInput, plan StockAdjustmentPlan, quantity string) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	out := StockAdjustmentOutput{Status: StatusOK, DryRun: input.DryRun, Plan: &plan}
	if store == nil {
		return nil, out, errors.New("stock plan store is unavailable")
	}
	if input.DryRun {
		token, err := store.issue(ctx, plan)
		if err != nil {
			if errors.Is(err, errStockPlanCapacity) {
				return stockClarification(out, "When should a new stock-depletion plan be prepared?", "confirmation", "too many confirmation plans are outstanding; wait for a token to expire or execute a reviewed plan", "dry_run", map[string]any{"stock_item_id": input.StockItemID})
			}
			return nil, out, err
		}
		out.PlanHash = token
		return TextResult(StatusOK), out, nil
	}
	if !input.Confirm {
		return stockClarification(out, "Should this reviewed stock item now be depleted and deleted?", "confirmation", "confirm must be true after reviewing the latest high-risk dry-run plan", "confirm", map[string]any{"stock_item_id": input.StockItemID, "dry_run": true, "high_risk": true, "will_delete": true})
	}
	if input.PlanHash == "" || !store.consume(ctx, input.PlanHash, plan) {
		return stockClarification(out, "Which current dry-run plan should authorize this stock deletion?", "confirmation", "plan_hash must be the unexpired, single-use token from a matching dry run by the same principal", "plan_hash", map[string]any{"stock_item_id": input.StockItemID, "dry_run": true})
	}
	mutationErr := client.RemoveStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: input.StockItemID, Quantity: quantity}}, Notes: plan.Reason})
	if errors.Is(mutationErr, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(mutationErr, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if mutationErr != nil {
		var apiErr *inventree.APIError
		if errors.As(mutationErr, &apiErr) && definiteStockMutationRejection(apiErr.StatusCode) {
			return nil, out, safeToolError(mutationErr)
		}
		return verifyStockDepletion(ctx, client, out, true)
	}
	return verifyStockDepletion(ctx, client, out, false)
}

func verifyStockDepletion(ctx context.Context, client StockAdjustmentClient, out StockAdjustmentOutput, recovered bool) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	current, err := client.GetStockItem(ctx, out.Plan.Before.StockItemID)
	if isNotFound(err) {
		out.Verified = true
		out.Recovered = recovered
		return TextResult(StatusOK), out, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, out, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, out, context.DeadlineExceeded
	}
	if err != nil {
		return stockDepletionPartial(out, nil, "stock removal may have completed, but exact-ID absence verification failed; use get_stock_item before preparing any new plan")
	}
	safe := sanitizedStockItem(current)
	return stockDepletionPartial(out, &safe, "the stock item still exists after the removal attempt; inspect its current state with get_stock_item and do not retry blindly")
}

func stockDepletionPartial(out StockAdjustmentOutput, record *inventree.StockItem, recovery string) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	out.Status = StatusPartialFailure
	out.Record = record
	out.Failure = &StockAdjustmentFailure{Action: DepleteStockItemToolName, Message: "stock deletion result could not be verified", RecoveryPlan: recovery}
	return TextResult(StatusPartialFailure), out, nil
}
