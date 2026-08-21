package tools

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdjustStockQuantityRequiresCurrentPlanAndRecordsBeforeAfter(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Location: &locationID, Quantity: 7, Status: stockStatusOK}}
	input := AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: -2, Reason: "cycle count correction"}

	_, plan, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	r.NotNil(plan.Plan)
	a.Equal(7.0, plan.Plan.Before.Quantity)
	a.Equal(5.0, plan.Plan.After.Quantity)
	a.True(plan.Plan.HighRisk)
	a.Contains(plan.Plan.RiskReason, "removes stock")
	a.NotEmpty(plan.PlanHash)
	a.Zero(fake.removeCalls)

	input.DryRun = false
	_, unconfirmed, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unconfirmed.Status)
	a.Equal("confirmation", unconfirmed.Clarification.Field)
	a.Zero(fake.removeCalls)

	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, executed, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	r.NotNil(executed.Record)
	a.Equal(5.0, executed.Record.Quantity)
	a.Equal("2", fake.lastAdjustment.Items[0].Quantity)
	a.Equal("cycle count correction", fake.lastAdjustment.Notes)
	a.Equal(1, fake.removeCalls)
}

func TestStockAdjustmentRejectsStalePlanAndInvalidAuditInputs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 5, Status: stockStatusOK}}

	_, missingReason, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, StocktakeAdjustmentInput{DryRun: true, StockItemID: 50, ObservedQuantity: 4})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, missingReason.Status)
	a.Equal("reason", missingReason.Clarification.Field)

	_, invalidDelta, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: -6, Reason: "invalid count"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalidDelta.Status)
	a.Equal("delta", invalidDelta.Clarification.Field)

	_, roundedToZero, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: 0.0000000001, Reason: "below schema precision"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, roundedToZero.Status)
	a.Equal("delta", roundedToZero.Clarification.Field)

	input := StocktakeAdjustmentInput{DryRun: true, StockItemID: 50, ObservedQuantity: 4, Reason: "shelf count"}
	_, plan, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	fake.item.Quantity = 6
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, stale, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Equal("confirmation", stale.Clarification.Field)
	a.Zero(fake.countCalls)
}

func TestAdjustStockQuantityNormalizesSchemaDecimalArithmetic(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 0.1, Status: stockStatusOK}}
	input := AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: 0.2, Reason: "fractional reel correction"}

	_, plan, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(0.3, plan.Plan.After.Quantity)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, output, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.InDelta(0.3, output.Record.Quantity, 1e-9)
	a.Equal("0.2", fake.lastAdjustment.Items[0].Quantity)
}

func TestStocktakeAdjustmentChangesOnlyAbsoluteQuantity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	locationID := 40
	batch := "B-1"
	packaging := "reel"
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Location: &locationID, Quantity: 8, Status: stockStatusDamaged, Batch: &batch, Packaging: &packaging}}
	input := StocktakeAdjustmentInput{DryRun: true, StockItemID: 50, ObservedQuantity: 6.5, Reason: "physical shelf count"}

	_, plan, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.True(plan.Plan.HighRisk)
	a.Equal(plan.Plan.Before.LocationID, plan.Plan.After.LocationID)
	a.Equal(plan.Plan.Before.Status, plan.Plan.After.Status)
	a.Equal(plan.Plan.Before.Batch, plan.Plan.After.Batch)
	a.Equal(plan.Plan.Before.Packaging, plan.Plan.After.Packaging)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, executed, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Equal(6.5, executed.Record.Quantity)
	a.Equal(locationID, *executed.Record.Location)
	a.Equal(stockStatusDamaged, executed.Record.Status)
	a.Equal(batch, *executed.Record.Batch)
	a.Equal(packaging, *executed.Record.Packaging)
	a.Equal(1, fake.countCalls)
}

func TestStockAdjustmentRefusesImplicitDeleteOnDeplete(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 2, Status: stockStatusOK, DeleteOnDeplete: true}}

	_, relative, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: -2, Reason: "counted none"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, relative.Status)
	a.Contains(relative.Clarification.Reason, "would delete")

	_, absolute, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, StocktakeAdjustmentInput{DryRun: true, StockItemID: 50, ObservedQuantity: 0, Reason: "counted none"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, absolute.Status)
	a.Contains(absolute.Clarification.Reason, "would delete")
	a.Zero(fake.removeCalls)
	a.Zero(fake.countCalls)
}

func TestStockAdjustmentRefusesSerializedQuantityChanges(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	serial := "S-1"
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Status: stockStatusOK, Serial: &serial}}

	_, relative, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: -1, Reason: "serialized count"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, relative.Status)
	a.Contains(relative.Clarification.Reason, "serialized stock")
	_, absolute, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, StocktakeAdjustmentInput{DryRun: true, StockItemID: 50, ObservedQuantity: 0, Reason: "serialized count"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, absolute.Status)
	a.Contains(absolute.Clarification.Reason, "serialized stock")
	a.Zero(fake.removeCalls)
	a.Zero(fake.countCalls)
}

func TestSetStockStatusFlagsWriteOffAndRequiresSupportedStatus(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	serial := "S-1"
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Status: stockStatusOK, Serial: &serial}}

	_, invalid, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: 999, Reason: "inspection"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, invalid.Status)
	a.Equal("status", invalid.Clarification.Field)

	input := SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusDestroyed, Reason: "destroyed after inspection"}
	_, plan, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.True(plan.Plan.HighRisk)
	a.Contains(plan.Plan.RiskReason, "Destroyed")

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, executed, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Equal(stockStatusDestroyed, executed.Record.Status)
	r.NotNil(executed.Record.Serial)
	a.Equal(serial, *executed.Record.Serial)
	a.Equal("destroyed after inspection", fake.lastStatusChange.Note)
}

func TestSetStockStatusSupportsCustomAssignmentReplacementClearAndOmission(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	logicalOK := 10
	logicalAttention := 50
	statuses := inventree.StockStatusClass{StatusClass: "StockStatus", Values: map[string]inventree.StockStatusValue{
		"OK":        {Key: 10, Name: "OK", Label: "OK"},
		"ATTENTION": {Key: 50, Name: "ATTENTION", Label: "Attention needed"},
		"INSPECT":   {Key: 110, LogicalKey: &logicalOK, Name: "INSPECT", Label: "Inspect", Custom: true},
		"REWORK":    {Key: 111, LogicalKey: &logicalOK, Name: "REWORK", Label: "Rework", Custom: true},
		"HOLD":      {Key: 112, LogicalKey: &logicalAttention, Name: "HOLD", Label: "Hold", Custom: true},
	}}

	newFake := func() *fakeStockAdjustmentClient {
		return &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Status: stockStatusOK}, statuses: statuses}
	}
	run := func(fake *fakeStockAdjustmentClient, input SetStockStatusInput) StockAdjustmentOutput {
		_, planned, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		r.Equal(StatusOK, planned.Status)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = planned.PlanHash
		_, executed, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		r.Equal(StatusOK, executed.Status)
		return executed
	}

	assigned := run(newFake(), SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{110}[0], Reason: "inspection"})
	r.NotNil(assigned.Record.StatusCustomKey)
	r.Equal(110, *assigned.Record.StatusCustomKey)

	replacedFake := newFake()
	assigned = run(replacedFake, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{110}[0], Reason: "inspection"})
	replaced := run(replacedFake, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{111}[0], Reason: "reclassification"})
	r.Equal(111, *replaced.Record.StatusCustomKey)
	r.Equal(111, *replaced.Plan.After.StatusCustomKey)
	_ = assigned

	clearedFake := newFake()
	run(clearedFake, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{110}[0], Reason: "inspection"})
	_, cleared, err := setStockStatus(stockAdjustmentDeps(clearedFake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, ClearStatusCustomKey: true, Reason: "clear inspection"})
	r.NoError(err)
	r.Equal(StatusClarificationRequired, cleared.Status)
	r.Equal(1, clearedFake.statusCalls)

	omittedFake := newFake()
	omitted := run(omittedFake, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusAttention, Reason: "logical reclassification"})
	r.Equal(stockStatusAttention, omitted.Record.Status)
	r.Nil(omitted.Record.StatusCustomKey)

	preserveFake := newFake()
	preserveFake.item.StatusCustomKey = &[]int{110}[0]
	_, preserve, err := setStockStatus(stockAdjustmentDeps(preserveFake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, Reason: "retain inspection"})
	r.NoError(err)
	r.Equal(StatusClarificationRequired, preserve.Status)
	r.Zero(preserveFake.statusCalls)
}

func TestSetStockStatusRejectsIncompatibleAndRemappedCustomKeys(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	logicalOK := 10
	logicalAttention := 50
	statuses := inventree.StockStatusClass{StatusClass: "StockStatus", Values: map[string]inventree.StockStatusValue{
		"OK":        {Key: 10, Name: "OK", Label: "OK"},
		"ATTENTION": {Key: 50, Name: "ATTENTION", Label: "Attention needed"},
		"INSPECT":   {Key: 110, LogicalKey: &logicalOK, Name: "INSPECT", Label: "Inspect", Custom: true},
		"HOLD":      {Key: 112, LogicalKey: &logicalAttention, Name: "HOLD", Label: "Hold", Custom: true},
	}}
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Status: stockStatusOK}, statuses: statuses}
	_, incompatible, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{112}[0], Reason: "wrong logical state"})
	r.NoError(err)
	r.Equal(StatusClarificationRequired, incompatible.Status)
	r.Zero(fake.statusCalls)

	_, planned, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{110}[0], Reason: "inspection"})
	r.NoError(err)
	logicalAttention = 50
	fake.statuses.Values["INSPECT"] = inventree.StockStatusValue{Key: 110, LogicalKey: &logicalAttention, Name: "INSPECT", Label: "Inspect", Custom: true}
	_, remapped, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{StockItemID: 50, Status: stockStatusOK, StatusCustomKey: &[]int{110}[0], Confirm: true, PlanHash: planned.PlanHash, Reason: "inspection"})
	r.NoError(err)
	r.Equal(StatusClarificationRequired, remapped.Status)
	r.Zero(fake.statusCalls)
}

func TestStockAdjustmentReturnsRecoveryForAmbiguousMutation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK}, mutateErr: errors.New("timeout")}
	input := AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: 1, Reason: "found one unit"}
	_, plan, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = plan.PlanHash
	_, output, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Failure)
	a.Contains(output.Failure.RecoveryPlan, "Do not retry")
}

func TestStockAdjustmentClassifiesDefiniteAndAmbiguousHTTPFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		statusCode int
		wantError  bool
		wantStatus string
	}{
		{name: "definite validation rejection", statusCode: http.StatusBadRequest, wantError: true},
		{name: "ambiguous request timeout", statusCode: http.StatusRequestTimeout, wantStatus: StatusPartialFailure},
		{name: "ambiguous too early", statusCode: http.StatusTooEarly, wantStatus: StatusPartialFailure},
		{name: "ambiguous rate limit", statusCode: http.StatusTooManyRequests, wantStatus: StatusPartialFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakeStockAdjustmentClient{
				item:      inventree.StockItem{PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK},
				mutateErr: &inventree.APIError{StatusCode: tc.statusCode},
			}
			input := AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: 1, Reason: "found one unit"}
			_, plan, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			input.DryRun = false
			input.Confirm = true
			input.PlanHash = plan.PlanHash
			_, output, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
			if tc.wantError {
				r.Error(err)
				return
			}
			r.NoError(err)
			a.Equal(tc.wantStatus, output.Status)
		})
	}
}

func TestStockPlanTokenIsBoundExpiringAndSingleUse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	tokenNumber := 0
	store := newStockPlanStore(func() time.Time { return now }, func() (string, error) {
		tokenNumber++
		return "opaque-token-" + strconv.Itoa(tokenNumber), nil
	})
	principal := "operator-a"
	store.principal = func(context.Context) string { return principal }
	plan := StockAdjustmentPlan{Action: AdjustStockQuantityToolName, Before: StockStateSnapshot{StockItemID: 50}, After: StockStateSnapshot{StockItemID: 50, Quantity: 1}, Reason: "count"}

	token, err := store.issue(context.Background(), plan)
	r.NoError(err)
	a.Equal("opaque-token-1", token)
	principal = "operator-b"
	a.False(store.consume(context.Background(), token, plan), "another principal must not consume the token")
	principal = "operator-a"
	a.True(store.consume(context.Background(), token, plan))
	a.False(store.consume(context.Background(), token, plan), "token must be single use")

	expiringToken, err := store.issue(context.Background(), plan)
	r.NoError(err)
	now = now.Add(stockPlanLifetime)
	a.False(store.consume(context.Background(), expiringToken, plan), "token must be expired at its deadline")
}

func TestStockPlanStoreInvalidatesSupersededTokensAndBoundsCapacity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	tokenNumber := 0
	store := newStockPlanStore(func() time.Time { return now }, func() (string, error) {
		tokenNumber++
		return "opaque-token-" + strconv.Itoa(tokenNumber), nil
	})
	principal := "operator-a"
	store.principal = func(context.Context) string { return principal }
	store.maxEntries = 2
	store.maxEntriesPerPrincipal = 1
	plan := StockAdjustmentPlan{Action: AdjustStockQuantityToolName, Before: StockStateSnapshot{StockItemID: 50}, After: StockStateSnapshot{StockItemID: 50, Quantity: 1}, Reason: "count"}

	first, err := store.issue(context.Background(), plan)
	r.NoError(err)
	latest, err := store.issue(context.Background(), plan)
	r.NoError(err)
	a.False(store.consume(context.Background(), first, plan), "a newer dry run must invalidate the superseded token")
	a.True(store.consume(context.Background(), latest, plan))

	_, err = store.issue(context.Background(), plan)
	r.NoError(err)
	otherItem := plan
	otherItem.Before.StockItemID = 51
	otherItem.After.StockItemID = 51
	_, err = store.issue(context.Background(), otherItem)
	r.ErrorIs(err, errStockPlanCapacity)

	principal = "operator-b"
	_, err = store.issue(context.Background(), otherItem)
	r.NoError(err)
	principal = "operator-c"
	thirdItem := plan
	thirdItem.Before.StockItemID = 52
	thirdItem.After.StockItemID = 52
	_, err = store.issue(context.Background(), thirdItem)
	r.ErrorIs(err, errStockPlanCapacity)
}

func TestStockAdjustmentRejectsNoOpPlans(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK}}

	_, quantity, err := stocktakeAdjustment(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, StocktakeAdjustmentInput{DryRun: true, StockItemID: 50, ObservedQuantity: 3, Reason: "cycle count"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, quantity.Status)
	_, status, err := setStockStatus(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockStatusInput{DryRun: true, StockItemID: 50, Status: stockStatusOK, Reason: "inspection"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, status.Status)
	a.Zero(fake.countCalls)
	a.Zero(fake.statusCalls)
}

func TestStockAdjustmentReturnsRecoveryAfterRefreshFailureOrStateMismatch(t *testing.T) {
	t.Parallel()

	t.Run("refresh failure", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK}, getErrAt: map[int]error{3: errors.New("refresh timeout")}}
		input := AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: 1, Reason: "found one unit"}
		_, plan, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, output, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusPartialFailure, output.Status)
		a.Contains(output.Failure.Message, "refreshed stock state is unavailable")
		a.Contains(output.Failure.RecoveryPlan, "Do not retry")
		a.Equal(1, fake.addCalls)
	})

	t.Run("concurrent state mismatch", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 3, Status: stockStatusOK, Link: "https://supplier.test/stock?token=secret#details"}}
		input := AdjustStockQuantityInput{DryRun: true, StockItemID: 50, Delta: 1, Reason: "found one unit"}
		_, plan, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		fake.beforeMutation = func(client *fakeStockAdjustmentClient) { client.item.Quantity++ }
		input.DryRun = false
		input.Confirm = true
		input.PlanHash = plan.PlanHash
		_, output, err := adjustStockQuantity(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(err)
		a.Equal(StatusPartialFailure, output.Status)
		a.Contains(output.Failure.Message, "does not match")
		a.Contains(output.Failure.RecoveryPlan, "Do not retry")
		r.NotNil(output.Record)
		a.Empty(output.Record.Link)
		a.Equal(1, fake.addCalls)
	})
}

func TestStockAdjustmentToolAuthorizationsAreOperational(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	for _, name := range []string{AdjustStockQuantityToolName, SetStockStatusToolName, StocktakeAdjustmentToolName} {
		auth, ok := ToolAuthorizations[name]
		if !a.True(ok, name) {
			continue
		}
		a.Equal(ToolMilestone1, auth.MilestoneStatus, name)
		a.Equal("operational", auth.MutationClass, name)
		a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational}, auth.Scopes, name)
		a.Equal(WriteAnnotations, auth.Annotations, name)
	}
}

type fakeStockAdjustmentClient struct {
	item                  inventree.StockItem
	lastAdjustment        inventree.StockAdjustment
	lastStatusChange      inventree.StockStatusChange
	lastPatch             inventree.PatchFields
	addCalls              int
	removeCalls           int
	countCalls            int
	statusCalls           int
	updateCalls           int
	mutateErr             error
	getCalls              int
	getErrAt              map[int]error
	beforeMutation        func(*fakeStockAdjustmentClient)
	deleted               bool
	responseLoss          bool
	planStore             *stockPlanStore
	searchResults         []inventree.StockItem
	searchResultsSequence [][]inventree.StockItem
	searchCalls           int
	searchErr             error
	part                  inventree.Part
	getPartErr            error
	setting               inventree.SettingValue
	getSettingErr         error
	statuses              inventree.StockStatusClass
}

func (f *fakeStockAdjustmentClient) GetStockItem(context.Context, int) (inventree.StockItem, error) {
	f.getCalls++
	if err := f.getErrAt[f.getCalls]; err != nil {
		return inventree.StockItem{}, err
	}
	if f.deleted {
		return inventree.StockItem{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return f.item, nil
}

func (f *fakeStockAdjustmentClient) GetStockStatuses(context.Context) (inventree.StockStatusClass, error) {
	if f.statuses.StatusClass != "" {
		return f.statuses, nil
	}
	values := make(map[string]inventree.StockStatusValue, len(stockStatusNames))
	for key, label := range stockStatusNames {
		values[label] = inventree.StockStatusValue{Key: key, Name: label, Label: label}
	}
	return inventree.StockStatusClass{StatusClass: "StockStatus", Values: values}, nil
}

func (f *fakeStockAdjustmentClient) AddStock(_ context.Context, input inventree.StockAdjustment) error {
	f.addCalls++
	f.lastAdjustment = input
	if f.mutateErr != nil {
		return f.mutateErr
	}
	f.runBeforeMutation()
	f.item.Quantity += mustAdjustmentQuantity(input)
	return nil
}

func (f *fakeStockAdjustmentClient) RemoveStock(_ context.Context, input inventree.StockAdjustment) error {
	f.removeCalls++
	f.lastAdjustment = input
	if f.mutateErr != nil {
		return f.mutateErr
	}
	f.runBeforeMutation()
	f.item.Quantity -= mustAdjustmentQuantity(input)
	if f.item.DeleteOnDeplete && f.item.Quantity == 0 {
		f.deleted = true
	}
	if f.responseLoss {
		return errors.New("injected response loss after stock removal")
	}
	return nil
}

func (f *fakeStockAdjustmentClient) CountStock(_ context.Context, input inventree.StockAdjustment) error {
	f.countCalls++
	f.lastAdjustment = input
	if f.mutateErr != nil {
		return f.mutateErr
	}
	f.runBeforeMutation()
	f.item.Quantity = mustAdjustmentQuantity(input)
	return nil
}

func (f *fakeStockAdjustmentClient) ChangeStockStatus(_ context.Context, input inventree.StockStatusChange) error {
	f.statusCalls++
	f.lastStatusChange = input
	if f.mutateErr != nil {
		return f.mutateErr
	}
	f.runBeforeMutation()
	f.item.Status = input.Status
	f.item.StatusCustomKey = nil
	if f.statuses.StatusClass != "" {
		for _, value := range f.statuses.Values {
			if value.Key == input.Status && value.Custom && value.LogicalKey != nil {
				f.item.Status = *value.LogicalKey
				key := value.Key
				f.item.StatusCustomKey = &key
				break
			}
		}
	}
	return nil
}

func (f *fakeStockAdjustmentClient) ClearStockCustomStatus(_ context.Context, _ int, logicalStatus int) (inventree.StockItem, error) {
	f.statusCalls++
	if f.mutateErr != nil {
		return inventree.StockItem{}, f.mutateErr
	}
	f.runBeforeMutation()
	f.item.Status = logicalStatus
	f.item.StatusCustomKey = nil
	return f.item, nil
}

func (f *fakeStockAdjustmentClient) UpdateStockItem(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockItem, error) {
	f.updateCalls++
	f.lastPatch = fields
	if f.mutateErr != nil {
		return inventree.StockItem{}, f.mutateErr
	}
	f.runBeforeMutation()
	if value, ok := fields["delete_on_deplete"]; ok {
		f.item.DeleteOnDeplete = value.Value().(bool)
	}
	if value, ok := fields["serial"]; ok {
		if value.Value() == nil {
			f.item.Serial = nil
		} else {
			serial, _ := value.Value().(string)
			f.item.Serial = &serial
		}
	}
	if f.deleted || id != f.item.PK {
		return inventree.StockItem{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return f.item, nil
}

func (f *fakeStockAdjustmentClient) SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error) {
	f.searchCalls++
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	if len(f.searchResultsSequence) >= f.searchCalls {
		return f.searchResultsSequence[f.searchCalls-1], nil
	}
	return f.searchResults, nil
}

func (f *fakeStockAdjustmentClient) GetPart(context.Context, int) (inventree.Part, error) {
	if f.getPartErr != nil {
		return inventree.Part{}, f.getPartErr
	}
	return f.part, nil
}

func (f *fakeStockAdjustmentClient) GetGlobalSetting(context.Context, string) (inventree.SettingValue, error) {
	if f.getSettingErr != nil {
		return inventree.SettingValue{}, f.getSettingErr
	}
	return f.setting, nil
}

func (f *fakeStockAdjustmentClient) runBeforeMutation() {
	if f.beforeMutation == nil {
		return
	}
	hook := f.beforeMutation
	f.beforeMutation = nil
	hook(f)
}

func stockAdjustmentDeps(fake *fakeStockAdjustmentClient) Dependencies {
	if fake.planStore == nil {
		fake.planStore = newStockPlanStore(time.Now, randomStockPlanToken)
	}
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, stockPlanStore: fake.planStore}
}

func mustAdjustmentQuantity(input inventree.StockAdjustment) float64 {
	quantity, err := strconv.ParseFloat(input.Items[0].Quantity, 64)
	if err != nil {
		panic(err)
	}
	return quantity
}
