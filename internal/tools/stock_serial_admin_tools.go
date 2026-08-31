package tools

import (
	"context"
	"errors"
	"strings"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serialGloballyUniqueSetting = "SERIAL_NUMBER_GLOBALLY_UNIQUE"

const maxStockSerialLength = 100

type AssignStockSerialInput struct {
	DryRun      bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound serial-assignment plan without writing."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash    string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID int    `json:"stock_item_id" jsonschema:"Existing unserialized stock-item primary key with quantity 1."`
	Serial      string `json:"serial" jsonschema:"Nonblank serial number to assign; up to 100 characters."`
	Reason      string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the serial assignment."`
}

type SetStockSerialInput struct {
	DryRun      bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound serial change plan without writing."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash    string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID int    `json:"stock_item_id" jsonschema:"Existing already-serialized stock-item primary key."`
	Serial      string `json:"serial,omitempty" jsonschema:"Replacement serial number; omit when clear_serial is true."`
	ClearSerial bool   `json:"clear_serial,omitempty" jsonschema:"true clears the current serial number instead of replacing it; mutually exclusive with serial."`
	Reason      string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the serial change."`
}

func assignStockSerial(deps Dependencies) mcp.ToolHandlerFor[AssignStockSerialInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, AssignStockSerialInput, StockAdjustmentOutput](deps, AssignStockSerialToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input AssignStockSerialInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			before, out, result, err := prepareStockAdjustment(ctx, client, input.DryRun, input.StockItemID, input.Reason)
			if result != nil || err != nil {
				return result, out, err
			}
			serial := strings.TrimSpace(input.Serial)
			if serial == "" || len(serial) > maxStockSerialLength {
				return stockClarification(out, "What nonblank serial number (up to 100 characters) should be assigned?", "serial", "serial must be nonblank and fit the InvenTree 100-character limit", "serial", map[string]any{"stock_item_id": input.StockItemID})
			}
			if stockItemHasSerial(before) {
				return stockClarification(out, "How should the existing serial number be changed instead?", "serial", "the selected stock item already has a serial number; use set_stock_serial to replace or clear it", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "serial": *before.Serial})
			}
			if before.Quantity != 1 {
				return stockClarification(out, "How should this stock item be split into single-quantity items before assigning a serial?", "quantity", "InvenTree requires quantity 1 to hold a serial number; splitting bulk stock into serialized items is outside this workflow", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "quantity": before.Quantity})
			}
			part, err := client.GetPart(ctx, before.Part)
			if err != nil {
				return nil, out, safeToolError(ctx, err)
			}
			if part.PK != before.Part {
				return nil, out, errors.New("InvenTree returned a mismatched part identity")
			}
			if !part.Trackable {
				return stockClarification(out, "Which trackable part's stock item should receive a serial number?", "part_id", "the base part is not trackable and cannot carry a serial number", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID, "part_id": before.Part})
			}
			if result, clarified, unsafe := unsafeStockSerialChange(out, before); unsafe {
				return result, clarified, nil
			}
			if result, clarified, collisionErr := stockSerialCollision(ctx, client, out, before.Part, serial, 0); result != nil || collisionErr != nil {
				return result, clarified, collisionErr
			}
			after := snapshotStockItem(before)
			after.Serial = &serial
			plan := StockAdjustmentPlan{Action: AssignStockSerialToolName, Before: snapshotStockItem(before), After: after, Reason: strings.TrimSpace(input.Reason)}
			return executeStockPlan(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan, func() error {
				_, updateErr := client.UpdateStockItem(ctx, input.StockItemID, inventree.PatchFields{"serial": inventree.Set(serial)})
				return updateErr
			})
		})
}

func setStockSerial(deps Dependencies) mcp.ToolHandlerFor[SetStockSerialInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, SetStockSerialInput, StockAdjustmentOutput](deps, SetStockSerialToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input SetStockSerialInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			before, out, result, err := prepareStockAdjustment(ctx, client, input.DryRun, input.StockItemID, input.Reason)
			if result != nil || err != nil {
				return result, out, err
			}
			serial := strings.TrimSpace(input.Serial)
			if input.ClearSerial == (serial != "") {
				return stockClarification(out, "Should the serial number be replaced with a new value or cleared?", "serial", "provide exactly one of a nonblank serial or clear_serial:true", "serial", map[string]any{"stock_item_id": input.StockItemID})
			}
			if !input.ClearSerial && len(serial) > maxStockSerialLength {
				return stockClarification(out, "What replacement serial number (up to 100 characters) should be assigned?", "serial", "serial must fit the InvenTree 100-character limit", "serial", map[string]any{"stock_item_id": input.StockItemID})
			}
			if !stockItemHasSerial(before) {
				return stockClarification(out, "How should this unserialized stock item receive its first serial number instead?", "serial", "the selected stock item has no serial number yet; use assign_stock_serial instead", "stock_item_id", map[string]any{"stock_item_id": input.StockItemID})
			}
			if !input.ClearSerial && serial == *before.Serial {
				return stockClarification(out, "What different serial number should replace the current one?", "serial", "the selected stock item already has this serial number; no mutation is needed", "serial", map[string]any{"stock_item_id": input.StockItemID, "serial": serial})
			}
			if result, clarified, unsafe := unsafeStockSerialChange(out, before); unsafe {
				return result, clarified, nil
			}
			if !input.ClearSerial {
				if result, clarified, collisionErr := stockSerialCollision(ctx, client, out, before.Part, serial, before.PK); result != nil || collisionErr != nil {
					return result, clarified, collisionErr
				}
			}
			after := snapshotStockItem(before)
			if input.ClearSerial {
				after.Serial = nil
			} else {
				after.Serial = &serial
			}
			plan := StockAdjustmentPlan{
				Action: SetStockSerialToolName, Before: snapshotStockItem(before), After: after, Reason: strings.TrimSpace(input.Reason),
				HighRisk: true, RiskReason: "changing an assigned serial number rewrites stock identity and downstream audit context",
			}
			return executeStockPlan(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan, func() error {
				value := inventree.Null()
				if !input.ClearSerial {
					value = inventree.Set(serial)
				}
				_, updateErr := client.UpdateStockItem(ctx, input.StockItemID, inventree.PatchFields{"serial": value})
				return updateErr
			})
		})
}

// stockSerialCollision reports whether serial is already in use by another
// stock item for the same part, and additionally across every part when
// SERIAL_NUMBER_GLOBALLY_UNIQUE is enabled. InvenTree's own write-time
// validation remains authoritative for the global scope either way.
func stockSerialCollision(ctx context.Context, client StockAdjustmentClient, out StockAdjustmentOutput, partID int, serial string, excludeStockItemID int) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
	partMatches, err := client.SearchStockItems(ctx, inventree.StockItemQuery{PartID: partID, Serial: serial, Limit: DefaultLookupLimit})
	if err != nil {
		return nil, out, safeToolError(ctx, err)
	}
	if conflict := firstSerialConflict(partMatches, excludeStockItemID); conflict != 0 {
		result, clarified, _ := stockClarification(out, "Which different serial number should be assigned?", "serial", "an existing stock item for this part already uses this serial number", "serial", map[string]any{"part_id": partID, "serial": serial, "conflicting_stock_item_id": conflict})
		return result, clarified, nil
	}
	globalUnique, err := stockSerialGloballyUnique(ctx, client)
	if err != nil {
		return nil, out, safeToolError(ctx, err)
	}
	if !globalUnique {
		return nil, out, nil
	}
	globalMatches, err := client.SearchStockItems(ctx, inventree.StockItemQuery{Serial: serial, Limit: DefaultLookupLimit})
	if err != nil {
		return nil, out, safeToolError(ctx, err)
	}
	if conflict := firstSerialConflict(globalMatches, excludeStockItemID); conflict != 0 {
		result, clarified, _ := stockClarification(out, "Which different serial number should be assigned?", "serial", "an existing stock item for a different part already uses this serial number and SERIAL_NUMBER_GLOBALLY_UNIQUE is enabled", "serial", map[string]any{"serial": serial, "conflicting_stock_item_id": conflict})
		return result, clarified, nil
	}
	return nil, out, nil
}

// stockSerialGloballyUnique reads SERIAL_NUMBER_GLOBALLY_UNIQUE. Pinned
// InvenTree treats /api/settings/global/{key}/ as staff-only, so an
// omittable permission/not-found error (per inventree.IsOmittableFetchError,
// mirroring get_inventree_instance_info's convention) is logged and treated
// as "unknown, skip the cross-part scan" rather than a hard failure; any
// other error is propagated so a real outage does not silently disable the
// check.
func stockSerialGloballyUnique(ctx context.Context, client StockAdjustmentClient) (bool, error) {
	setting, err := client.GetGlobalSetting(ctx, serialGloballyUniqueSetting)
	if err != nil {
		if !inventree.IsOmittableFetchError(err) {
			return false, err
		}
		logging.FromContext(ctx).WarnContext(ctx, "omitting global serial-uniqueness check; SERIAL_NUMBER_GLOBALLY_UNIQUE is unreadable", logging.Err(err))
		return false, nil
	}
	return strings.EqualFold(strings.TrimSpace(setting.Value), "true"), nil
}

func firstSerialConflict(items []inventree.StockItem, excludeStockItemID int) int {
	for _, item := range items {
		if item.PK == excludeStockItemID {
			continue
		}
		if stockItemHasSerial(item) {
			return item.PK
		}
	}
	return 0
}

func unsafeStockSerialChange(out StockAdjustmentOutput, item inventree.StockItem) (*mcp.CallToolResult, StockAdjustmentOutput, bool) {
	retry := map[string]any{"stock_item_id": item.PK, "dry_run": true}
	switch {
	case item.Allocated == nil:
		result, clarified, _ := stockClarification(out, "What is the current stock allocation state?", "allocated", "allocation context is unavailable, so a safe serial change cannot be proven", "stock_item_id", retry)
		return result, clarified, true
	case *item.Allocated != 0:
		retry["allocated"] = *item.Allocated
		result, clarified, _ := stockClarification(out, "How should the allocated stock be released first?", "allocated", "allocated stock cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.IsBuilding:
		result, clarified, _ := stockClarification(out, "How should the in-production stock item be completed or cancelled first?", "is_building", "stock currently being built cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.ConsumedBy != nil:
		retry["consumed_by_id"] = *item.ConsumedBy
		result, clarified, _ := stockClarification(out, "How should the consumed stock history be handled?", "consumed_by", "stock consumed by a build cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.BelongsTo != nil:
		retry["belongs_to_id"] = *item.BelongsTo
		result, clarified, _ := stockClarification(out, "How should the installed stock item be uninstalled first?", "belongs_to", "stock installed in another item cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.Parent != nil:
		retry["parent_id"] = *item.Parent
		result, clarified, _ := stockClarification(out, "How should the child stock relationship be removed first?", "parent", "a child stock item cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.InstalledItems == nil:
		result, clarified, _ := stockClarification(out, "What is the current installed-item state?", "installed_items", "installed-item context is unavailable, so a safe serial change cannot be proven", "stock_item_id", retry)
		return result, clarified, true
	case *item.InstalledItems != 0:
		retry["installed_items"] = *item.InstalledItems
		result, clarified, _ := stockClarification(out, "How should installed child items be removed first?", "installed_items", "stock containing installed items cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.ChildItems == nil:
		result, clarified, _ := stockClarification(out, "What is the current child-item state?", "child_items", "child-item context is unavailable, so a safe serial change cannot be proven", "stock_item_id", retry)
		return result, clarified, true
	case *item.ChildItems != 0:
		retry["child_items"] = *item.ChildItems
		result, clarified, _ := stockClarification(out, "How should child stock items be separated first?", "child_items", "stock with child items cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.Customer != nil:
		retry["customer_id"] = *item.Customer
		result, clarified, _ := stockClarification(out, "How should the customer-assigned stock be handled first?", "customer", "stock assigned to a customer cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	case item.SalesOrder != nil:
		retry["sales_order_id"] = *item.SalesOrder
		result, clarified, _ := stockClarification(out, "How should the sales-order-linked stock be handled first?", "sales_order", "stock linked to a sales order cannot have its serial number changed", "stock_item_id", retry)
		return result, clarified, true
	default:
		return nil, out, false
	}
}
