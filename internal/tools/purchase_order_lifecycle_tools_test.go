package tools

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldPurchaseOrderFromPlacedRequiresReviewedCurrentStatePlanAndCarriesNoWarning(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Status: inventree.PurchaseOrderStatusPlaced}}}
	handler := holdPurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	a.Equal(HoldPurchaseOrderToolName, plan.Action)
	a.Empty(plan.Warning)
	r.NotEmpty(plan.PlanHash)
	a.Equal([]PlannedChange{{Action: HoldPurchaseOrderToolName, RecordType: "purchase_order", ID: 120, Fields: map[string]any{"status": inventree.PurchaseOrderStatusOnHold}}}, plan.PlannedChanges)
	a.Zero(fake.holdCalls)

	_, unconfirmed, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: 120})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unconfirmed.Status)
	a.Equal("confirm", unconfirmed.Clarification.Retry)
	a.Zero(fake.holdCalls)

	_, held, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: 120, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, held.Status)
	r.NotNil(held.Order)
	a.Equal(inventree.PurchaseOrderStatusOnHold, held.Order.Status)
	a.False(held.Recovered)
	a.Equal(1, fake.holdCalls)

	// replaying the plan against the now-ON_HOLD order hits the already_on_hold
	// no-op rather than re-consuming the single-use token; single-use
	// enforcement itself is covered by
	// TestPurchaseOrderLifecyclePlanStoreBindsPrincipalAndSingleUse.
	_, replay, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: 120, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, replay.Status)
	a.Equal("already_on_hold", replay.Action)
	a.Equal(1, fake.holdCalls)
}

func TestHoldPurchaseOrderFromPendingWarnsThatResumeWillPlaceTheOrder(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 121, Status: inventree.PurchaseOrderStatusPending}}}
	handler := holdPurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 121})
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	a.NotEmpty(plan.Warning)
	a.Contains(plan.Warning, "place the order with its supplier")
}

func TestHoldPurchaseOrderAlreadyOnHoldIsANoOp(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 122, Status: inventree.PurchaseOrderStatusOnHold}}}
	_, out, err := holdPurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 122})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("already_on_hold", out.Action)
	a.Zero(fake.holdCalls)
}

func TestHoldPurchaseOrderRefusesFromCompleteAndCancelled(t *testing.T) {
	t.Parallel()
	for _, status := range []int{inventree.PurchaseOrderStatusComplete, inventree.PurchaseOrderStatusCancelled} {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 123, Status: status}}}
			_, out, err := holdPurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 123})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status)
			a.Zero(fake.holdCalls)
		})
	}
}

func TestResumePurchaseOrderRequiresReviewedCurrentStatePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 130, Status: inventree.PurchaseOrderStatusOnHold}}}
	handler := resumePurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{DryRun: true, OrderID: 130})
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	r.NotEmpty(plan.PlanHash)

	_, resumed, err := handler(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{OrderID: 130, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, resumed.Status)
	r.NotNil(resumed.Order)
	a.Equal(inventree.PurchaseOrderStatusPlaced, resumed.Order.Status)
	a.Equal(1, fake.issueCalls)
}

func TestResumePurchaseOrderAlreadyPlacedIsANoOp(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 131, Status: inventree.PurchaseOrderStatusPlaced}}}
	_, out, err := resumePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{DryRun: true, OrderID: 131})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("already_placed", out.Action)
	a.Zero(fake.issueCalls)
}

func TestResumePurchaseOrderRefusesNonOnHoldOrders(t *testing.T) {
	t.Parallel()
	for _, status := range []int{inventree.PurchaseOrderStatusPending, inventree.PurchaseOrderStatusComplete, inventree.PurchaseOrderStatusCancelled} {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 132, Status: status}}}
			_, out, err := resumePurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{DryRun: true, OrderID: 132})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status)
		})
	}
}

func TestCancelPurchaseOrderRequiresReviewedCurrentStatePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 140, Status: inventree.PurchaseOrderStatusPending}}}
	handler := cancelPurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: 140})
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	r.NotEmpty(plan.PlanHash)

	_, cancelled, err := handler(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{OrderID: 140, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, cancelled.Status)
	r.NotNil(cancelled.Order)
	a.Equal(inventree.PurchaseOrderStatusCancelled, cancelled.Order.Status)
	a.Equal(1, fake.cancelCalls)
}

func TestCancelPurchaseOrderAlreadyCancelledIsANoOp(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 141, Status: inventree.PurchaseOrderStatusCancelled}}}
	_, out, err := cancelPurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: 141})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal("already_cancelled", out.Action)
	a.Zero(fake.cancelCalls)
}

func TestCancelPurchaseOrderRefusesCompletedOrders(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 142, Status: inventree.PurchaseOrderStatusComplete}}}
	_, out, err := cancelPurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: 142})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.cancelCalls)
}

func TestCancelPurchaseOrderRefusesAnyReceivedLine(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 143, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 150, Order: 143, Part: 40, Quantity: 2, Received: 1}},
	}
	_, out, err := cancelPurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: 143})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Equal("line_item_id", out.Clarification.Retry)
	a.Zero(fake.cancelCalls)
}

func TestPurchaseOrderLifecycleStaleConfirmationIsRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{orders: []inventree.PurchaseOrder{{PK: 160, Status: inventree.PurchaseOrderStatusPlaced}}}
	handler := holdPurchaseOrder(purchasingDeps(fake))
	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 160})
	r.NoError(err)

	// order metadata changes after the dry run
	fake.orders[0].Description = "changed after dry run"
	_, out, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: 160, Confirm: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.holdCalls)
}

func TestPurchaseOrderLifecyclePlanStoreBindsPrincipalAndSingleUse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	store := newPurchaseOrderLifecyclePlanStore(time.Now, randomStockPlanToken)
	principal := "first"
	store.principal = func(context.Context) string { return principal }
	plan := PurchaseOrderLifecyclePlan{Action: HoldPurchaseOrderToolName, OrderID: 170, Order: inventree.PurchaseOrder{PK: 170}, TargetStatus: inventree.PurchaseOrderStatusOnHold}
	token, err := store.issue(context.Background(), plan)
	r.NoError(err)
	principal = "second"
	a.False(store.consume(context.Background(), token, plan))
	principal = "first"
	a.True(store.consume(context.Background(), token, plan))
	a.False(store.consume(context.Background(), token, plan))
}

func TestPurchaseOrderLifecyclePlanStoreEnforcesCapacityPerPrincipal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	store := newPurchaseOrderLifecyclePlanStore(time.Now, randomStockPlanToken)
	store.principal = func(context.Context) string { return "same" }
	store.maxEntriesPerPrincipal = 2
	for i := range 2 {
		plan := PurchaseOrderLifecyclePlan{Action: HoldPurchaseOrderToolName, OrderID: 300 + i, Order: inventree.PurchaseOrder{PK: 300 + i}, TargetStatus: inventree.PurchaseOrderStatusOnHold}
		_, err := store.issue(context.Background(), plan)
		r.NoError(err)
	}
	plan := PurchaseOrderLifecyclePlan{Action: HoldPurchaseOrderToolName, OrderID: 302, Order: inventree.PurchaseOrder{PK: 302}, TargetStatus: inventree.PurchaseOrderStatusOnHold}
	_, err := store.issue(context.Background(), plan)
	r.ErrorIs(err, errStockPlanCapacity)
}

func TestHoldPurchaseOrderMutationRecoveryAndDefiniteRejection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		mutationErr   error
		afterPersist  error
		keepStatus    bool
		refreshErr    error
		wantError     bool
		wantPartial   bool
		wantRecovered bool
	}{
		{name: "ambiguous response recovered", afterPersist: errors.New("response lost"), wantRecovered: true},
		{name: "mutation succeeds but status unchanged", keepStatus: true, wantPartial: true},
		{name: "refresh unavailable after mutation", refreshErr: errors.New("read unavailable"), wantPartial: true},
		{name: "definite validation rejection", mutationErr: &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{
				orders:               []inventree.PurchaseOrder{{PK: 220, Status: inventree.PurchaseOrderStatusPlaced}},
				holdErr:              test.mutationErr,
				holdErrAfterPersist:  test.afterPersist,
				holdKeepStatus:       test.keepStatus,
				getOrderErrAfterHold: test.refreshErr,
			}
			handler := holdPurchaseOrder(purchasingDeps(fake))
			_, plan, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 220})
			r.NoError(err)
			_, output, err := handler(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{OrderID: 220, Confirm: true, PlanHash: plan.PlanHash})
			if test.wantError {
				r.Error(err)
				a.Equal(1, fake.holdCalls)
				return
			}
			r.NoError(err)
			if test.wantPartial {
				a.Equal(StatusPartialFailure, output.Status)
				r.NotNil(output.Failure)
			} else {
				a.Equal(StatusOK, output.Status)
			}
			a.Equal(test.wantRecovered, output.Recovered)
			a.Equal(1, fake.holdCalls)
		})
	}
}

func TestResumePurchaseOrderMutationRecoveryAndDefiniteRejection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		mutationErr   error
		afterPersist  error
		keepStatus    bool
		refreshErr    error
		wantError     bool
		wantPartial   bool
		wantRecovered bool
	}{
		{name: "ambiguous response recovered", afterPersist: errors.New("response lost"), wantRecovered: true},
		{name: "mutation succeeds but status unchanged", keepStatus: true, wantPartial: true},
		{name: "refresh unavailable after mutation", refreshErr: errors.New("read unavailable"), wantPartial: true},
		{name: "definite validation rejection", mutationErr: &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{
				orders:                []inventree.PurchaseOrder{{PK: 221, Status: inventree.PurchaseOrderStatusOnHold}},
				issueErr:              test.mutationErr,
				issueErrAfterPersist:  test.afterPersist,
				issueKeepStatus:       test.keepStatus,
				getOrderErrAfterIssue: test.refreshErr,
			}
			handler := resumePurchaseOrder(purchasingDeps(fake))
			_, plan, err := handler(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{DryRun: true, OrderID: 221})
			r.NoError(err)
			_, output, err := handler(ctx, &mcp.CallToolRequest{}, ResumePurchaseOrderInput{OrderID: 221, Confirm: true, PlanHash: plan.PlanHash})
			if test.wantError {
				r.Error(err)
				a.Equal(1, fake.issueCalls)
				return
			}
			r.NoError(err)
			if test.wantPartial {
				a.Equal(StatusPartialFailure, output.Status)
				r.NotNil(output.Failure)
			} else {
				a.Equal(StatusOK, output.Status)
			}
			a.Equal(test.wantRecovered, output.Recovered)
			a.Equal(1, fake.issueCalls)
		})
	}
}

func TestCancelPurchaseOrderMutationRecoveryAndDefiniteRejection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		mutationErr   error
		afterPersist  error
		keepStatus    bool
		refreshErr    error
		wantError     bool
		wantPartial   bool
		wantRecovered bool
	}{
		{name: "ambiguous response recovered", afterPersist: errors.New("response lost"), wantRecovered: true},
		{name: "mutation succeeds but status unchanged", keepStatus: true, wantPartial: true},
		{name: "refresh unavailable after mutation", refreshErr: errors.New("read unavailable"), wantPartial: true},
		{name: "definite validation rejection", mutationErr: &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{
				orders:                 []inventree.PurchaseOrder{{PK: 222, Status: inventree.PurchaseOrderStatusPending}},
				cancelErr:              test.mutationErr,
				cancelErrAfterPersist:  test.afterPersist,
				cancelKeepStatus:       test.keepStatus,
				getOrderErrAfterCancel: test.refreshErr,
			}
			handler := cancelPurchaseOrder(purchasingDeps(fake))
			_, plan, err := handler(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{DryRun: true, OrderID: 222})
			r.NoError(err)
			_, output, err := handler(ctx, &mcp.CallToolRequest{}, CancelPurchaseOrderInput{OrderID: 222, Confirm: true, PlanHash: plan.PlanHash})
			if test.wantError {
				r.Error(err)
				a.Equal(1, fake.cancelCalls)
				return
			}
			r.NoError(err)
			if test.wantPartial {
				a.Equal(StatusPartialFailure, output.Status)
				r.NotNil(output.Failure)
			} else {
				a.Equal(StatusOK, output.Status)
			}
			a.Equal(test.wantRecovered, output.Recovered)
			a.Equal(1, fake.cancelCalls)
		})
	}
}

func TestPurchaseOrderLifecycleRedactsLineAndExtraLineNotes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders:     []inventree.PurchaseOrder{{PK: 230, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:      []inventree.PurchaseOrderLineItem{{PK: 240, Order: 230, Notes: "confidential vendor terms"}},
		extraLines: []inventree.PurchaseOrderExtraLine{{PK: 250, Order: 230, Notes: "confidential surcharge reason"}},
	}
	_, out, err := holdPurchaseOrder(purchasingDeps(fake))(ctx, &mcp.CallToolRequest{}, HoldPurchaseOrderInput{DryRun: true, OrderID: 230})
	r.NoError(err)
	r.Len(out.Lines, 1)
	r.Len(out.ExtraLines, 1)
	a.Empty(out.Lines[0].Notes)
	a.Empty(out.ExtraLines[0].Notes)
}
