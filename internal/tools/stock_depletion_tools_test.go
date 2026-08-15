package tools

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepleteStockItemPlansAndDeletesCompletePositiveQuantity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	buildID := 70
	supplierPartID := 80
	purchaseOrderID := 90
	allocated := 0.0
	zero := 0
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{
		PK: 50, Part: 10, Location: &locationID, Quantity: 3.5, Status: stockStatusOK, InStock: true,
		DeleteOnDeplete: true, Allocated: &allocated, Build: &buildID, SupplierPart: &supplierPartID,
		PurchaseOrder: &purchaseOrderID, InstalledItems: &zero, ChildItems: &zero,
	}}
	input := DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "remove historical receiving placeholder"}

	_, planned, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	a.Equal(3.5, planned.Plan.Before.Quantity)
	a.Equal(0.0, planned.Plan.After.Quantity)
	a.True(planned.Plan.HighRisk)
	a.True(planned.Plan.WillDelete)
	r.NotNil(planned.Plan.Depletion)
	a.Equal(&buildID, planned.Plan.Depletion.BuildID)
	a.Equal(&supplierPartID, planned.Plan.Depletion.SupplierPartID)
	a.Equal(&purchaseOrderID, planned.Plan.Depletion.PurchaseOrderID)
	a.NotEmpty(planned.PlanHash)
	a.Zero(fake.removeCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.False(executed.Recovered)
	a.Nil(executed.Record)
	a.True(fake.deleted)
	a.Equal("3.5", fake.lastAdjustment.Items[0].Quantity)
	a.Equal("remove historical receiving placeholder", fake.lastAdjustment.Notes)
}

func TestDepleteStockItemRejectsUnsafeStates(t *testing.T) {
	t.Parallel()
	serial := "S-1"
	allocated := 1.0
	linkedID := 70
	one := 1
	negativeOne := -1

	for _, tc := range []struct {
		name      string
		configure func(*inventree.StockItem)
		field     string
	}{
		{name: "not delete on deplete", configure: func(item *inventree.StockItem) { item.DeleteOnDeplete = false }, field: "delete_on_deplete"},
		{name: "not in stock", configure: func(item *inventree.StockItem) { item.InStock = false }, field: "in_stock"},
		{name: "unknown allocation", configure: func(item *inventree.StockItem) { item.Allocated = nil }, field: "allocated"},
		{name: "allocated", configure: func(item *inventree.StockItem) { item.Allocated = &allocated }, field: "allocated"},
		{name: "serialized", configure: func(item *inventree.StockItem) { item.Serial = &serial }, field: "serial"},
		{name: "building", configure: func(item *inventree.StockItem) { item.IsBuilding = true }, field: "is_building"},
		{name: "consumed", configure: func(item *inventree.StockItem) { item.ConsumedBy = &linkedID }, field: "consumed_by"},
		{name: "installed", configure: func(item *inventree.StockItem) { item.BelongsTo = &linkedID }, field: "belongs_to"},
		{name: "child", configure: func(item *inventree.StockItem) { item.Parent = &linkedID }, field: "parent"},
		{name: "unknown installed children", configure: func(item *inventree.StockItem) { item.InstalledItems = nil }, field: "installed_items"},
		{name: "installed children", configure: func(item *inventree.StockItem) { item.InstalledItems = &one }, field: "installed_items"},
		{name: "invalid negative installed children", configure: func(item *inventree.StockItem) { item.InstalledItems = &negativeOne }, field: "installed_items"},
		{name: "unknown child items", configure: func(item *inventree.StockItem) { item.ChildItems = nil }, field: "child_items"},
		{name: "child items", configure: func(item *inventree.StockItem) { item.ChildItems = &one }, field: "child_items"},
		{name: "invalid negative child items", configure: func(item *inventree.StockItem) { item.ChildItems = &negativeOne }, field: "child_items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			item := safeDepletionStockItem(2)
			tc.configure(&item)
			fake := &fakeStockAdjustmentClient{item: item}
			_, output, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, output.Status)
			r.NotNil(output.Clarification)
			a.Equal(tc.field, output.Clarification.Field)
			a.Zero(fake.removeCalls)
		})
	}
}

func TestDepleteStockItemRejectsNonPositiveOrUnrepresentableCurrentQuantity(t *testing.T) {
	t.Parallel()
	for _, quantity := range []float64{0, -1, 0.0000000001} {
		t.Run(strconv.FormatFloat(quantity, 'g', -1, 64), func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakeStockAdjustmentClient{item: safeDepletionStockItem(quantity)}
			_, output, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, output.Status)
			r.NotNil(output.Clarification)
			a.Equal("quantity", output.Clarification.Field)
			a.Zero(fake.removeCalls)
		})
	}
}

func TestDepleteStockItemRejectsInvalidStaleAndReusedPlans(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: safeDepletionStockItem(2)}
	deps := stockAdjustmentDeps(fake)

	_, missingReason, err := depleteStockItem(deps)(ctx, &mcp.CallToolRequest{}, DepleteStockItemInput{DryRun: true, StockItemID: 50})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingReason.Status)

	input := DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"}
	_, planned, err := depleteStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	fake.item.Quantity = 3
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, stale, err := depleteStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Zero(fake.removeCalls)

	fake.item.Quantity = 2
	_, refreshed, err := depleteStockItem(deps)(ctx, &mcp.CallToolRequest{}, DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"})
	r.NoError(err)
	fake.mutateErr = &inventree.APIError{StatusCode: 400, Kind: inventree.ErrorKindValidation}
	input.PlanHash = refreshed.PlanHash
	_, _, err = depleteStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.Error(err)
	fake.mutateErr = nil
	_, reused, err := depleteStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, reused.Status)
	a.Equal("confirmation", reused.Clarification.Field)
}

func TestDepleteStockItemRecoversLostSuccessAndReturnsPartialForUnknownResult(t *testing.T) {
	t.Parallel()

	t.Run("lost success", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := &fakeStockAdjustmentClient{item: safeDepletionStockItem(5), responseLoss: true}
		input := DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"}
		_, planned, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = planned.PlanHash
		_, output, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusOK, output.Status)
		a.True(output.Verified)
		a.True(output.Recovered)
		a.True(fake.deleted)
	})

	t.Run("mutation not applied", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		item := safeDepletionStockItem(5)
		item.Link = "https://supplier.test/stock?token=secret#details"
		fake := &fakeStockAdjustmentClient{item: item, mutateErr: errors.New("timeout")}
		input := DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"}
		_, planned, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = planned.PlanHash
		_, output, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusPartialFailure, output.Status)
		r.NotNil(output.Record)
		a.Empty(output.Record.Link)
		a.Contains(output.Failure.RecoveryPlan, "do not retry blindly")
	})
}

func TestDepleteStockItemPreservesContextErrors(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakeStockAdjustmentClient{item: safeDepletionStockItem(1), mutateErr: sentinel}
			input := DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"}
			_, planned, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = planned.PlanHash
			_, _, err = depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.ErrorIs(err, sentinel)
		})
	}
}

func TestDepleteStockItemPreservesRecoveryReadbackContextErrors(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakeStockAdjustmentClient{
				item:      safeDepletionStockItem(1),
				mutateErr: errors.New("ambiguous removal response"),
				getErrAt:  map[int]error{3: sentinel},
			}
			input := DepleteStockItemInput{DryRun: true, StockItemID: 50, Reason: "cleanup"}
			_, planned, err := depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = planned.PlanHash
			_, _, err = depleteStockItem(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.ErrorIs(err, sentinel)
		})
	}
}

func TestDepleteStockItemAuthorizationIsDestructiveAndOperational(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Equal("destructive", ToolAuthorizations[DepleteStockItemToolName].MutationClass)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive}, ToolAuthorizations[DepleteStockItemToolName].Scopes)
	expected := WriteAnnotations
	expected.Destructive = true
	a.Equal(expected, ToolAuthorizations[DepleteStockItemToolName].Annotations)
	a.NotEqual(WriteAnnotations, ToolAuthorizations[DepleteStockItemToolName].Annotations)
}

func TestDepleteStockItemMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	allocated := 0.0
	zero := 0
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{
		PK: 50, Part: 10, Quantity: 2, Status: stockStatusOK, InStock: true, DeleteOnDeplete: true,
		Allocated: &allocated, InstalledItems: &zero, ChildItems: &zero,
	}}
	session, closeSession := plannedChangesSession(t, ctx, fake)
	defer closeSession()

	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	found := false
	for _, tool := range listed.Tools {
		if tool.Name != DepleteStockItemToolName {
			continue
		}
		found = true
		inputProperties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		a.NotContains(inputProperties, "quantity")
		a.Contains(inputProperties, "stock_item_id")
		outputProperties := tool.OutputSchema.(map[string]any)["properties"].(map[string]any)
		a.Contains(outputProperties, "verified")
		a.Contains(outputProperties, "recovered")
	}
	a.True(found)

	plannedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: DepleteStockItemToolName, Arguments: map[string]any{
		"dry_run": true, "stock_item_id": 50, "reason": "remove placeholder",
	}})
	r.NoError(err)
	a.False(plannedResult.IsError)
	planned := plannedResult.StructuredContent.(map[string]any)
	plan := planned["plan"].(map[string]any)
	a.Equal(true, plan["will_delete"])
	a.Equal(true, plan["high_risk"])
	before := plan["before"].(map[string]any)
	a.Equal(float64(2), before["quantity"])
	a.Contains(before, "delete_on_deplete")
	depletion := plan["depletion"].(map[string]any)
	a.Equal(float64(0), depletion["allocated"])
	a.Contains(depletion, "is_building")
	a.Equal(float64(0), plan["after"].(map[string]any)["quantity"])

	executedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: DepleteStockItemToolName, Arguments: map[string]any{
		"stock_item_id": 50, "reason": "remove placeholder", "confirm": true, "plan_hash": planned["plan_hash"],
	}})
	r.NoError(err)
	a.False(executedResult.IsError)
	executed := executedResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, executed["status"])
	a.Equal(true, executed["verified"])
	a.NotContains(executed, "record")
}

func safeDepletionStockItem(quantity float64) inventree.StockItem {
	zeroFloat := 0.0
	zeroInt := 0
	return inventree.StockItem{
		PK: 50, Part: 10, Quantity: quantity, Status: stockStatusOK, InStock: true, DeleteOnDeplete: true,
		Allocated: &zeroFloat, InstalledItems: &zeroInt, ChildItems: &zeroInt,
	}
}
