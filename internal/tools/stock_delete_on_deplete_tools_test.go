package tools

import (
	"errors"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetStockDeleteOnDepletePlansAndConfirmsEnablingChange(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	allocated := 0.0
	zero := 0
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{
		PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK, InStock: true, DeleteOnDeplete: false,
		Allocated: &allocated, InstalledItems: &zero, ChildItems: &zero,
	}}
	input := SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: true, Reason: "authorize automatic cleanup of a receiving placeholder"}

	_, planned, err := setStockDeleteOnDeplete(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	a.False(planned.Plan.Before.DeleteOnDeplete)
	a.True(planned.Plan.After.DeleteOnDeplete)
	a.Equal(3.0, planned.Plan.After.Quantity)
	a.True(planned.Plan.HighRisk)
	a.NotEmpty(planned.Plan.RiskReason)
	r.NotNil(planned.Plan.Depletion)
	a.Equal(&allocated, planned.Plan.Depletion.Allocated)
	a.NotEmpty(planned.PlanHash)
	a.Zero(fake.updateCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := setStockDeleteOnDeplete(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	r.NotNil(executed.Record)
	a.True(executed.Record.DeleteOnDeplete)
	a.Equal(1, fake.updateCalls)
	a.Equal(true, fake.lastPatch["delete_on_deplete"].Value())
}

func TestSetStockDeleteOnDepleteAllowsZeroQuantityAndDisabling(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 0, Status: stockStatusOK, DeleteOnDeplete: true}}
	input := SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: false, Reason: "retain zero-quantity placeholder"}

	_, planned, err := setStockDeleteOnDeplete(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	a.False(planned.Plan.HighRisk)
	a.Empty(planned.Plan.RiskReason)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := setStockDeleteOnDeplete(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.False(executed.Record.DeleteOnDeplete)
}

func TestSetStockDeleteOnDepleteRejectsNoOpAndMissingIdentityOrReason(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, DeleteOnDeplete: true}}
	deps := stockAdjustmentDeps(fake)

	_, missingID, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, SetStockDeleteOnDepleteInput{DryRun: true, DeleteOnDeplete: false, Reason: "cleanup"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingID.Status)
	a.Equal("stock_item", missingID.Clarification.Field)

	_, missingReason, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: false})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingReason.Status)
	a.Equal("reason", missingReason.Clarification.Field)

	_, noOp, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: true, Reason: "already enabled"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, noOp.Status)
	a.Equal("delete_on_deplete", noOp.Clarification.Field)
	a.Equal("delete_on_deplete", noOp.Clarification.Retry)
	a.Zero(fake.updateCalls)
}

func TestSetStockDeleteOnDepleteRejectsStaleAndReusedPlans(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, DeleteOnDeplete: false}}
	deps := stockAdjustmentDeps(fake)

	input := SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: true, Reason: "enable cleanup"}
	_, planned, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	fake.item.Quantity = 5
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, stale, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Zero(fake.updateCalls)

	fake.item.Quantity = 3
	_, refreshed, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: true, Reason: "enable cleanup"})
	r.NoError(err)
	fake.mutateErr = &inventree.APIError{StatusCode: 400, Kind: inventree.ErrorKindValidation}
	input.PlanHash = refreshed.PlanHash
	_, _, err = setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.Error(err)
	fake.mutateErr = nil
	_, reused, err := setStockDeleteOnDeplete(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, reused.Status)
	a.Equal("confirmation", reused.Clarification.Field)
}

func TestSetStockDeleteOnDepleteRecoversUnknownResultOnMutationFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, DeleteOnDeplete: false}, mutateErr: errors.New("timeout")}
	input := SetStockDeleteOnDepleteInput{DryRun: true, StockItemID: 50, DeleteOnDeplete: true, Reason: "enable cleanup"}
	_, planned, err := setStockDeleteOnDeplete(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, output, err := setStockDeleteOnDeplete(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Failure)
}

func TestSetStockDeleteOnDepleteAuthorizationIsDestructiveAndOperational(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Equal("destructive", ToolAuthorizations[SetStockDeleteOnDepleteToolName].MutationClass)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive}, ToolAuthorizations[SetStockDeleteOnDepleteToolName].Scopes)
	expected := WriteAnnotations
	expected.Destructive = true
	a.Equal(expected, ToolAuthorizations[SetStockDeleteOnDepleteToolName].Annotations)
	a.NotEqual(WriteAnnotations, ToolAuthorizations[SetStockDeleteOnDepleteToolName].Annotations)
}

func TestSetStockDeleteOnDepleteMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK, DeleteOnDeplete: false}}
	session, closeSession := plannedChangesSession(t, ctx, fake)
	defer closeSession()

	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	found := false
	for _, tool := range listed.Tools {
		if tool.Name != SetStockDeleteOnDepleteToolName {
			continue
		}
		found = true
		inputProperties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		a.Contains(inputProperties, "stock_item_id")
		a.Contains(inputProperties, "delete_on_deplete")
		a.Contains(inputProperties, "reason")
	}
	a.True(found)

	plannedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: SetStockDeleteOnDepleteToolName, Arguments: map[string]any{
		"dry_run": true, "stock_item_id": 50, "delete_on_deplete": true, "reason": "authorize cleanup",
	}})
	r.NoError(err)
	a.False(plannedResult.IsError)
	planned := plannedResult.StructuredContent.(map[string]any)
	plan := planned["plan"].(map[string]any)
	a.Equal(true, plan["high_risk"])
	before := plan["before"].(map[string]any)
	a.Equal(false, before["delete_on_deplete"])
	a.Equal(true, plan["after"].(map[string]any)["delete_on_deplete"])

	executedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: SetStockDeleteOnDepleteToolName, Arguments: map[string]any{
		"stock_item_id": 50, "delete_on_deplete": true, "reason": "authorize cleanup", "confirm": true, "plan_hash": planned["plan_hash"],
	}})
	r.NoError(err)
	a.False(executedResult.IsError)
	executed := executedResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, executed["status"])
	a.Equal(true, executed["record"].(map[string]any)["delete_on_deplete"])
}
