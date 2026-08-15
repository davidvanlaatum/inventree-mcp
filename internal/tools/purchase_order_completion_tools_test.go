package tools

import (
	"errors"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletePurchaseOrderRequiresReviewedCurrentStatePlan(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Reference: "PO-1", Status: inventree.PurchaseOrderStatusPlaced}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Part: 40, Quantity: 2, Received: 2}},
	}
	handler := completePurchaseOrder(purchasingDeps(fake))

	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	a.Equal(StatusOK, plan.Status)
	a.Equal(CompletePurchaseOrderToolName, plan.Action)
	r.NotEmpty(plan.PlanHash)
	r.Len(plan.Lines, 1)
	a.Equal([]PlannedChange{{
		Action: CompletePurchaseOrderToolName, RecordType: "purchase_order", ID: 120,
		Fields: map[string]any{"status": inventree.PurchaseOrderStatusComplete, "accept_incomplete": false},
	}}, plan.PlannedChanges)
	a.Zero(fake.completeCalls)

	_, unconfirmed, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{OrderID: 120})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unconfirmed.Status)
	a.Equal("dry_run", unconfirmed.Clarification.Retry)
	a.Zero(fake.completeCalls)

	_, completed, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{OrderID: 120, ConfirmComplete: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, completed.Status)
	r.NotNil(completed.Order)
	a.Equal(inventree.PurchaseOrderStatusComplete, completed.Order.Status)
	a.False(completed.Recovered)
	a.Equal(1, fake.completeCalls)
}

func TestCompletePurchaseOrderRejectsOutstandingAndStaleLineState(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakePurchasingClient{
		orders: []inventree.PurchaseOrder{{PK: 120, Status: inventree.PurchaseOrderStatusPlaced}},
		lines:  []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Quantity: 2, Received: 1}},
	}
	handler := completePurchaseOrder(purchasingDeps(fake))

	_, incomplete, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, incomplete.Status)
	a.Contains(incomplete.Clarification.Reason, "incomplete completion is not supported")
	a.Equal(float64(1), incomplete.Clarification.RetryValues["outstanding_quantity"])
	a.Zero(fake.completeCalls)

	fake.lines[0].Received = 2
	_, plan, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{DryRun: true, OrderID: 120})
	r.NoError(err)
	fake.lines[0].Notes = "changed after review"
	_, stale, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{OrderID: 120, ConfirmComplete: true, PlanHash: plan.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Equal("dry_run", stale.Clarification.Retry)
	a.Zero(fake.completeCalls)
}

func TestCompletePurchaseOrderHandlesAlreadyCompleteAndMutationRecovery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		status        int
		mutationErr   error
		afterPersist  error
		keepPlaced    bool
		refreshErr    error
		wantError     bool
		wantPartial   bool
		wantRecovered bool
		wantCalls     int
	}{
		{name: "already complete", status: inventree.PurchaseOrderStatusComplete, wantCalls: 0},
		{name: "ambiguous response recovered", status: inventree.PurchaseOrderStatusPlaced, afterPersist: errors.New("response lost"), wantRecovered: true, wantCalls: 1},
		{name: "ambiguous response remains placed", status: inventree.PurchaseOrderStatusPlaced, mutationErr: errors.New("timeout"), wantPartial: true, wantCalls: 1},
		{name: "successful mutation refresh unavailable", status: inventree.PurchaseOrderStatusPlaced, refreshErr: errors.New("read unavailable"), wantPartial: true, wantCalls: 1},
		{name: "definite validation rejection", status: inventree.PurchaseOrderStatusPlaced, mutationErr: &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}, wantError: true, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakePurchasingClient{
				orders:      []inventree.PurchaseOrder{{PK: 120, Status: test.status}},
				lines:       []inventree.PurchaseOrderLineItem{{PK: 130, Order: 120, Quantity: 1, Received: 1}},
				completeErr: test.mutationErr, completeErrAfterPersist: test.afterPersist,
				completeKeepPlaced: test.keepPlaced, getOrderErrAfterComplete: test.refreshErr,
			}
			handler := completePurchaseOrder(purchasingDeps(fake))
			input := CompletePurchaseOrderInput{OrderID: 120}
			if test.status == inventree.PurchaseOrderStatusPlaced {
				_, plan, err := handler(ctx, &mcp.CallToolRequest{}, CompletePurchaseOrderInput{DryRun: true, OrderID: 120})
				r.NoError(err)
				input.ConfirmComplete = true
				input.PlanHash = plan.PlanHash
			}
			_, output, err := handler(ctx, &mcp.CallToolRequest{}, input)
			if test.wantError {
				r.Error(err)
			} else {
				r.NoError(err)
			}
			if test.wantPartial {
				a.Equal(StatusPartialFailure, output.Status)
				r.NotNil(output.Failure)
				a.Contains(output.Failure.RecoveryPlan, "Do not repeat any purchase-order receipt")
			}
			a.Equal(test.wantRecovered, output.Recovered)
			a.Equal(test.wantCalls, fake.completeCalls)
		})
	}
}
