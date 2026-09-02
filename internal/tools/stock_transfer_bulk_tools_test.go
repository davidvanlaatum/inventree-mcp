package tools

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/batch"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fake
// ---------------------------------------------------------------------------

// bulkFakeStockTransfer implements StockTransferClient with per-ID stock
// items and locations, unlike stock_transfer_tools_test.go's
// fakeStockTransferClient (which tracks exactly one item), because a batch
// needs independent state per item.
type bulkFakeStockTransfer struct {
	mu                     sync.Mutex
	items                  map[int]inventree.StockItem
	locations              map[int]inventree.StockLocation
	transferErr            map[int]error
	transferApplyBeforeErr map[int]bool
	transferCalls          int
	lastTransferNotes      string
	lastTransferLocation   int
}

func newBulkFakeStockTransfer() *bulkFakeStockTransfer {
	return &bulkFakeStockTransfer{
		items:                  map[int]inventree.StockItem{},
		locations:              map[int]inventree.StockLocation{},
		transferErr:            map[int]error{},
		transferApplyBeforeErr: map[int]bool{},
	}
}

func (f *bulkFakeStockTransfer) GetStockItem(_ context.Context, id int) (inventree.StockItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	if !ok {
		return inventree.StockItem{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return item, nil
}

func (f *bulkFakeStockTransfer) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	location, ok := f.locations[id]
	if !ok {
		return inventree.StockLocation{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return location, nil
}

func (f *bulkFakeStockTransfer) SearchStockItemsPage(context.Context, inventree.StockItemQuery) (inventree.StockItemPage, error) {
	return inventree.StockItemPage{}, nil
}

func (f *bulkFakeStockTransfer) TransferStock(_ context.Context, input inventree.StockTransfer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transferCalls++
	f.lastTransferNotes = input.Notes
	f.lastTransferLocation = input.Location
	for _, entry := range input.Items {
		if err := f.transferErr[entry.PK]; err != nil {
			if f.transferApplyBeforeErr[entry.PK] {
				item := f.items[entry.PK]
				location := input.Location
				item.Location = &location
				f.items[entry.PK] = item
			}
			return err
		}
	}
	for _, entry := range input.Items {
		item := f.items[entry.PK]
		location := input.Location
		item.Location = &location
		f.items[entry.PK] = item
	}
	return nil
}

func testStockTransferBulkDeps(client any) Dependencies {
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.stockTransferBulkPlanStore = mustBulkStore(batch.Options[stockTransferBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p stockTransferBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	return deps
}

func availableStockItem(id, location int, quantity float64) inventree.StockItem {
	zeroFloat := 0.0
	zeroInt := 0
	return inventree.StockItem{
		PK: id, Part: 1, Location: &location, Quantity: quantity, Status: stockStatusOK, InStock: true,
		Allocated: &zeroFloat, InstalledItems: &zeroInt, ChildItems: &zeroInt,
	}
}

// ---------------------------------------------------------------------------
// bulk_transfer_stock_items
// ---------------------------------------------------------------------------

func TestBuildStockTransferBulkPlanRejectsUnknownID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()

	plan := buildStockTransferBulkPlan(ctx, client, []BulkTransferStockItem{{ID: 1}}, 20)
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
}

func TestBuildStockTransferBulkPlanRejectsDuplicateIDWithinBatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)

	plan := buildStockTransferBulkPlan(ctx, client, []BulkTransferStockItem{{ID: 1}, {ID: 1}}, 20)
	require.Len(t, plan.Items, 2)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[0].FailReason)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[1].FailReason)
}

func TestBuildStockTransferBulkPlanRejectsUnsafeItem(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	allocated := 2.0
	item := availableStockItem(1, 10, 5)
	item.Allocated = &allocated
	client.items[1] = item

	plan := buildStockTransferBulkPlan(ctx, client, []BulkTransferStockItem{{ID: 1}}, 20)
	require.Len(t, plan.Items, 1)
	assert.Contains(t, plan.Items[0].FailReason, "allocated stock cannot be transferred")
}

func TestBulkTransferStockItemsRequiresDestinationAndReason(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: []BulkTransferStockItem{{ID: 1}}, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: []BulkTransferStockItem{{ID: 1}}, DestinationLocationID: 20, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Equal(0, client.transferCalls)
}

func TestBulkTransferStockItemsRejectsUnknownDestination(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: []BulkTransferStockItem{{ID: 1}}, DestinationLocationID: 999, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestBulkTransferStockItemsRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{DestinationLocationID: 20, Reason: "consolidate"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	tooMany := make([]BulkTransferStockItem, bulkUpdateMaxItems+1)
	for i := range tooMany {
		tooMany[i] = BulkTransferStockItem{ID: i + 1}
	}
	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: tooMany, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestBulkTransferStockItemsDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	client.items[2] = availableStockItem(2, 11, 3)
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	items := []BulkTransferStockItem{{ID: 1}, {ID: 2}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 2)
	for _, item := range dryOut.Items {
		a.Equal(bulkOutcomePlanned, item.Outcome)
	}
	a.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 2)
	byID := bulkResultsByID(confirmOut.Items)
	a.Equal(string(batch.OutcomeApplied), byID[1].Outcome)
	a.Equal(string(batch.OutcomeApplied), byID[2].Outcome)
	r.NotNil(byID[1].Record)
	r.NotNil(byID[1].Record.Location)
	a.Equal(20, *byID[1].Record.Location)
	r.NotNil(byID[2].Record)
	r.NotNil(byID[2].Record.Location)
	a.Equal(20, *byID[2].Record.Location)
	a.Equal(2, client.transferCalls)
	a.Equal("consolidate", client.lastTransferNotes)
}

func TestBulkTransferStockItemsRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	items := []BulkTransferStockItem{{ID: 1}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)

	// State drifts after the dry run but before confirm: the digest embedded
	// in the freshly rebuilt plan at confirm time no longer matches, so the
	// stored token must be rejected rather than silently applied.
	client.items[1] = availableStockItem(1, 30, 5)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.transferCalls)
}

func TestBulkTransferStockItemsMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5) // applies
	client.items[2] = availableStockItem(2, 20, 3) // already at destination: skipped
	client.items[3] = availableStockItem(3, 10, 1) // ambiguous mutation
	client.transferErr[3] = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
	// no client.items[4]: this item fails plan-build entirely (unknown ID).
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	items := []BulkTransferStockItem{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 4)
	byID := bulkResultsByID(confirmOut.Items)
	a.Equal(string(batch.OutcomeApplied), byID[1].Outcome)
	a.Equal(string(batch.OutcomeSkipped), byID[2].Outcome)
	// Any error returned from Mutate is reported as ambiguous, never failed:
	// the write was attempted, so whether it partially applied is unknown.
	a.Equal(string(batch.OutcomeAmbiguous), byID[3].Outcome)
	// A plan-build failure (item 4, unknown ID) is caught by Preflight before
	// any write is attempted, so it is reported as failed, not ambiguous.
	a.Equal(string(batch.OutcomeFailed), byID[4].Outcome)
	a.Equal(2, client.transferCalls) // items 1 and 3 only; 2 is skipped, 4 never attempted
}

func TestBulkTransferStockItemsRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	// A 5xx response is ambiguous: the fake still applies the location change
	// before returning the error, simulating a write that landed upstream
	// before the response was lost. Mutate must recover by reading back
	// current state rather than reporting a bare ambiguous failure.
	client.transferErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.transferApplyBeforeErr[1] = true
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	items := []BulkTransferStockItem{{ID: 1}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	r.NotNil(confirmOut.Items[0].Record.Location)
	a.Equal(20, *confirmOut.Items[0].Record.Location)
}

func TestBulkTransferStockItemsRejectsWhenOutstandingPlanCapacityIsExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	client.items[2] = availableStockItem(2, 10, 5)
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	deps.stockTransferBulkPlanStore = mustBulkStore(batch.Options[stockTransferBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey:           func(p stockTransferBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		MaxEntriesPerPrincipal: 1,
	})
	handler := bulkTransferStockItems(deps)

	_, first, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: []BulkTransferStockItem{{ID: 1}}, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.NotEmpty(first.PlanHash)

	_, second, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: []BulkTransferStockItem{{ID: 2}}, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, second.Status)
	a.Empty(second.PlanHash)
}

// TestStockTransferBulkAdapterPreflightDetectsDrift calls the Adapter's
// Preflight directly with a hand-crafted item whose Before no longer matches
// current state. Reaching this branch through the full dry_run/confirm
// handler flow is impractical: confirm rebuilds the plan from fresh state
// immediately before Store.Consume, so any drift wide enough to observe from
// outside the handler already fails at the digest check (see
// TestBulkTransferStockItemsRejectsStalePlanHash) before Preflight ever runs.
func TestStockTransferBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 30, 5)
	adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}
	item := stockTransferBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           availableStockItem(1, 10, 5),
		Quantity:         "5",
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

func TestStockTransferBulkAdapterPreflightRejectsUnreadableItem(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}
	item := stockTransferBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           availableStockItem(1, 10, 5),
		Quantity:         "5",
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonReadFailed, reason)
}

func TestStockTransferBulkAdapterVerifyDetectsMismatches(t *testing.T) {
	t.Parallel()

	baseline := func() (stockTransferBulkPlanItem, *bulkFakeStockTransfer) {
		client := newBulkFakeStockTransfer()
		before := availableStockItem(1, 10, 5)
		client.items[1] = availableStockItem(1, 20, 5) // already moved to destination 20
		return stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: 1}, Before: before, Quantity: "5"}, client
	}

	t.Run("read_error", func(t *testing.T) {
		t.Parallel()
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		item, client := baseline()
		delete(client.items, 1)
		adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}

		a.Error(adapter.Verify(ctx, item))
	})

	t.Run("wrong_location", func(t *testing.T) {
		t.Parallel()
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		item, client := baseline()
		client.items[1] = availableStockItem(1, 10, 5) // never actually moved
		adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}

		a.Error(adapter.Verify(ctx, item))
	})

	t.Run("quantity_drift", func(t *testing.T) {
		t.Parallel()
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		item, client := baseline()
		client.items[1] = availableStockItem(1, 20, 3) // moved, but quantity does not match the reviewed plan
		adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}

		a.Error(adapter.Verify(ctx, item))
	})

	t.Run("provenance_mismatch", func(t *testing.T) {
		t.Parallel()
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		item, client := baseline()
		moved := availableStockItem(1, 20, 5)
		moved.Batch = stringPointer("someone-else-changed-it") // provenance drifted concurrently
		client.items[1] = moved
		adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}

		a.Error(adapter.Verify(ctx, item))
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		a := assert.New(t)
		ctx, _, _ := testhandler.SetupTestHandler(t)
		item, client := baseline()
		adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}

		a.NoError(adapter.Verify(ctx, item))
		view, ok := adapter.get(1)
		a.True(ok)
		a.Equal(1, view.PK)
	})
}

func TestStockTransferBulkAdapterMutateReturnsOriginalErrorWhenRecoveryReadDoesNotConfirm(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	// A 5xx response is ambiguous, so Mutate attempts a self-heal read-back;
	// here the item genuinely never moved (transferApplyBeforeErr unset), so
	// the read-back cannot confirm the write and the original error must be
	// returned rather than silently swallowed.
	client.transferErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	adapter := &stockTransferBulkAdapter{client: client, destinationLocationID: 20, reason: "consolidate"}
	item := stockTransferBulkPlanItem{bulkPlanItemBase: bulkPlanItemBase{ID: 1}, Before: availableStockItem(1, 10, 5), Quantity: "5"}

	err := adapter.Mutate(ctx, item)
	r.Error(err)
	var apiErr *inventree.APIError
	a.ErrorAs(err, &apiErr)
	if apiErr != nil {
		a.Equal(http.StatusInternalServerError, apiErr.StatusCode)
	}
}

func TestBuildStockTransferBulkPlanItemRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()

	item := buildStockTransferBulkPlanItem(ctx, client, BulkTransferStockItem{ID: 0}, 20)
	assert.Equal(t, "id must be positive", item.FailReason)
}

func TestBuildStockTransferBulkPlanItemRejectsInvalidQuantity(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, -3) // negative quantity is never schema-valid

	item := buildStockTransferBulkPlanItem(ctx, client, BulkTransferStockItem{ID: 1}, 20)
	assert.Equal(t, "current stock quantity is not schema-valid", item.FailReason)
}

// TestBulkTransferStockItemsRejectsDuplicateIDEndToEnd drives the full
// dry_run/confirm handler with a duplicate id, rather than calling
// buildStockTransferBulkPlan directly, to prove the duplicate-rejection
// acceptance criterion holds at the tool boundary: the dry-run preview
// reports the duplicate as failed and confirm never calls TransferStock for
// it.
func TestBulkTransferStockItemsRejectsDuplicateIDEndToEnd(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStockTransfer()
	client.items[1] = availableStockItem(1, 10, 5)
	client.locations[20] = inventree.StockLocation{PK: 20, Name: "Destination"}
	deps := testStockTransferBulkDeps(client)
	handler := bulkTransferStockItems(deps)

	items := []BulkTransferStockItem{{ID: 1}, {ID: 1}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 2)
	a.Equal(bulkOutcomeFailed, dryOut.Items[0].Outcome)
	a.Equal(bulkReasonDuplicateID, dryOut.Items[0].Message)
	a.Equal(bulkOutcomeFailed, dryOut.Items[1].Outcome)
	a.Equal(bulkReasonDuplicateID, dryOut.Items[1].Message)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkTransferStockItemsInput{Items: items, DestinationLocationID: 20, Reason: "consolidate", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusPartialFailure, confirmOut.Status)
	r.Len(confirmOut.Items, 2)
	for _, result := range confirmOut.Items {
		a.Equal(string(batch.OutcomeFailed), result.Outcome)
		a.Equal(bulkReasonDuplicateID, result.Message)
	}
	a.Equal(0, client.transferCalls, "a duplicate id must never reach TransferStock")
}
