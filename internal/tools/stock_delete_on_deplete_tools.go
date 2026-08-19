package tools

import (
	"context"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SetStockDeleteOnDepleteInput struct {
	DryRun          bool   `json:"dry_run,omitempty" jsonschema:"Return the current-state-bound delete-on-deplete policy plan without writing."`
	Confirm         bool   `json:"confirm,omitempty" jsonschema:"Required true for execution after reviewing a dry run."`
	PlanHash        string `json:"plan_hash,omitempty" jsonschema:"Opaque single-use confirmation token returned by the latest dry run."`
	StockItemID     int    `json:"stock_item_id" jsonschema:"Existing stock-item primary key."`
	DeleteOnDeplete bool   `json:"delete_on_deplete" jsonschema:"Target delete_on_deplete policy. true authorizes InvenTree to delete this stock item once a later depletion transaction reduces its quantity to zero; false removes that authorization."`
	Reason          string `json:"reason" jsonschema:"Nonblank operator audit reason recorded with the policy change."`
}

func setStockDeleteOnDeplete(deps Dependencies) mcp.ToolHandlerFor[SetStockDeleteOnDepleteInput, StockAdjustmentOutput] {
	return LookupHandler[StockAdjustmentClient, SetStockDeleteOnDepleteInput, StockAdjustmentOutput](deps, SetStockDeleteOnDepleteToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockAdjustmentClient, input SetStockDeleteOnDepleteInput) (*mcp.CallToolResult, StockAdjustmentOutput, error) {
			before, out, result, err := prepareStockAdjustment(ctx, client, input.DryRun, input.StockItemID, input.Reason)
			if result != nil || err != nil {
				return result, out, err
			}
			if before.DeleteOnDeplete == input.DeleteOnDeplete {
				return stockClarification(out, "Which different delete-on-deplete policy should be applied?", "delete_on_deplete", "the selected stock item already has this delete-on-deplete policy; no mutation is needed", "delete_on_deplete", map[string]any{"stock_item_id": input.StockItemID, "delete_on_deplete": input.DeleteOnDeplete})
			}
			after := snapshotStockItem(before)
			after.DeleteOnDeplete = input.DeleteOnDeplete
			plan := StockAdjustmentPlan{
				Action: SetStockDeleteOnDepleteToolName, Before: snapshotStockItem(before), After: after, Reason: strings.TrimSpace(input.Reason),
				Depletion: stockDepletionContext(before),
			}
			if input.DeleteOnDeplete {
				plan.HighRisk = true
				plan.RiskReason = "enabling delete_on_deplete authorizes InvenTree to delete this stock-item record at a future depletion"
			}
			return executeStockPlan(ctx, deps.stockPlanStore, client, input.DryRun, input.Confirm, input.PlanHash, plan, func() error {
				_, updateErr := client.UpdateStockItem(ctx, input.StockItemID, inventree.PatchFields{"delete_on_deplete": inventree.Set(input.DeleteOnDeplete)})
				return updateErr
			})
		})
}
