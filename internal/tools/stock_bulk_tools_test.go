package tools

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
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

// bulkFakeStock backs both bulk stock tools with one map-keyed fake, mirroring
// bulkFakeCompanyAdmin's single-fake-covers-related-resources shape. It
// implements the complete StockAdminClient and StockAdjustmentClient method
// sets so it satisfies whichever interface LookupHandler resolves; methods
// unused by either bulk tool are unreachable stubs.
type bulkFakeStock struct {
	mu                   sync.Mutex
	items                map[int]inventree.StockItem
	updateErr            map[int]error
	applyBeforeErr       map[int]bool
	statusErr            map[int]error
	statusApplyBeforeErr map[int]bool
	updateCalls          int
	statusCalls          int
	lastStatusNote       string
}

func newBulkFakeStock() *bulkFakeStock {
	return &bulkFakeStock{
		items:                map[int]inventree.StockItem{},
		updateErr:            map[int]error{},
		applyBeforeErr:       map[int]bool{},
		statusErr:            map[int]error{},
		statusApplyBeforeErr: map[int]bool{},
	}
}

func (f *bulkFakeStock) GetStockItem(_ context.Context, id int) (inventree.StockItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	if !ok {
		return inventree.StockItem{}, &inventree.APIError{Kind: inventree.ErrorKindNotFound, StatusCode: http.StatusNotFound}
	}
	return item, nil
}

func (f *bulkFakeStock) UpdateStockItem(_ context.Context, id int, fields inventree.PatchFields) (inventree.StockItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls++
	item := f.items[id]
	applyPatchToStockItem(&item, fields)
	if err := f.updateErr[id]; err != nil {
		if f.applyBeforeErr[id] {
			f.items[id] = item
		}
		return inventree.StockItem{}, err
	}
	f.items[id] = item
	return item, nil
}

func (f *bulkFakeStock) ChangeStockStatus(_ context.Context, input inventree.StockStatusChange) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls++
	f.lastStatusNote = input.Note
	id := input.Items[0]
	item := f.items[id]
	if err := f.statusErr[id]; err != nil {
		if f.statusApplyBeforeErr[id] {
			item.Status = input.Status
			f.items[id] = item
		}
		return err
	}
	item.Status = input.Status
	f.items[id] = item
	return nil
}

func applyPatchToStockItem(item *inventree.StockItem, fields inventree.PatchFields) {
	if field, ok := fields["batch"]; ok {
		item.Batch = nullableStringPatchValue(field)
	}
	if field, ok := fields["expiry_date"]; ok {
		item.ExpiryDate = nullableStringPatchValue(field)
	}
	if field, ok := fields["packaging"]; ok {
		item.Packaging = nullableStringPatchValue(field)
	}
	if field, ok := fields["notes"]; ok {
		item.Notes = nullableStringPatchValue(field)
	}
	if field, ok := fields["link"]; ok {
		if value, ok := field.Value().(string); ok {
			item.Link = value
		}
	}
}

// StockAdminClient stub methods, unused by bulk_update_stock_item_metadata.
func (f *bulkFakeStock) GetOwner(context.Context, int) (inventree.Owner, error) {
	return inventree.Owner{}, errors.New("not implemented")
}
func (f *bulkFakeStock) GetStockLocation(context.Context, int) (inventree.StockLocation, error) {
	return inventree.StockLocation{}, errors.New("not implemented")
}
func (f *bulkFakeStock) SearchStockLocationsPage(context.Context, inventree.StockLocationQuery) (inventree.StockLocationPage, error) {
	return inventree.StockLocationPage{}, nil
}
func (f *bulkFakeStock) GetStockLocationType(context.Context, int) (inventree.StockLocationType, error) {
	return inventree.StockLocationType{}, errors.New("not implemented")
}
func (f *bulkFakeStock) CreateStockLocation(context.Context, inventree.StockLocationCreate) (inventree.StockLocation, error) {
	return inventree.StockLocation{}, errors.New("not implemented")
}
func (f *bulkFakeStock) UpdateStockLocation(context.Context, int, inventree.PatchFields) (inventree.StockLocation, error) {
	return inventree.StockLocation{}, errors.New("not implemented")
}

// StockAdjustmentClient stub methods, unused by bulk_set_stock_status.
func (f *bulkFakeStock) AddStock(context.Context, inventree.StockAdjustment) error {
	return errors.New("not implemented")
}
func (f *bulkFakeStock) RemoveStock(context.Context, inventree.StockAdjustment) error {
	return errors.New("not implemented")
}
func (f *bulkFakeStock) CountStock(context.Context, inventree.StockAdjustment) error {
	return errors.New("not implemented")
}
func (f *bulkFakeStock) SearchStockItems(context.Context, inventree.StockItemQuery) ([]inventree.StockItem, error) {
	return nil, nil
}
func (f *bulkFakeStock) GetPart(context.Context, int) (inventree.Part, error) {
	return inventree.Part{}, errors.New("not implemented")
}
func (f *bulkFakeStock) GetGlobalSetting(context.Context, string) (inventree.SettingValue, error) {
	return inventree.SettingValue{}, errors.New("not implemented")
}

func testStockBulkDeps(client any) Dependencies {
	deps := Dependencies{ClientFromContext: func(context.Context) (any, error) { return client, nil }, BulkMaxItems: 25, BulkConcurrency: 4}
	deps.stockMetadataBulkPlanStore = mustBulkStore(batch.Options[stockMetadataBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p stockMetadataBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	deps.stockStatusBulkPlanStore = mustBulkStore(batch.Options[stockStatusBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey: func(p stockStatusBulkPlan) string { return bulkSupersedeKey(p.ids()) },
	})
	return deps
}

// ---------------------------------------------------------------------------
// bulk_update_stock_item_metadata
// ---------------------------------------------------------------------------

func TestBuildStockMetadataBulkPlanRejectsUnknownID(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()

	plan := buildStockMetadataBulkPlan(ctx, client, []BulkUpdateStockItemMetadataItem{{ID: 1, Batch: dvgoutils.Ptr("B1")}})
	require.Len(t, plan.Items, 1)
	assert.NotEmpty(t, plan.Items[0].FailReason)
}

func TestBuildStockMetadataBulkPlanRejectsDuplicateIDWithinBatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1}

	plan := buildStockMetadataBulkPlan(ctx, client, []BulkUpdateStockItemMetadataItem{
		{ID: 1, Batch: dvgoutils.Ptr("B1")},
		{ID: 1, Batch: dvgoutils.Ptr("B2")},
	})
	require.Len(t, plan.Items, 2)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[0].FailReason)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[1].FailReason)
}

func TestBuildStockMetadataBulkPlanRejectsClearConflict(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1}

	plan := buildStockMetadataBulkPlan(ctx, client, []BulkUpdateStockItemMetadataItem{{ID: 1, Batch: dvgoutils.Ptr("B1"), ClearBatch: true}})
	require.Len(t, plan.Items, 1)
	assert.Contains(t, plan.Items[0].FailReason, "conflict")
}

func TestBulkUpdateStockItemMetadataDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("Old")}
	deps := testStockBulkDeps(client)
	handler := bulkUpdateStockItemMetadata(deps)

	items := []BulkUpdateStockItemMetadataItem{{ID: 1, Batch: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 1)
	a.Equal(bulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	require.NotNil(t, confirmOut.Items[0].Record.Batch)
	a.Equal("New", *confirmOut.Items[0].Record.Batch)
	a.Equal(1, client.updateCalls)
}

func TestBulkUpdateStockItemMetadataRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("Old")}
	deps := testStockBulkDeps(client)
	handler := bulkUpdateStockItemMetadata(deps)

	items := []BulkUpdateStockItemMetadataItem{{ID: 1, Batch: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, DryRun: true})
	r.NoError(err)

	// State drifts after the dry run but before confirm: the digest embedded
	// in the freshly rebuilt plan at confirm time no longer matches, so the
	// stored token must be rejected rather than silently applied.
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("SomeoneElseChangedIt")}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.updateCalls)
}

func TestBulkUpdateStockItemMetadataMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("Applies")}
	client.items[2] = inventree.StockItem{PK: 2, Batch: dvgoutils.Ptr("AlreadyThere")}
	client.items[3] = inventree.StockItem{PK: 3, Batch: dvgoutils.Ptr("Ambiguous")}
	client.updateErr[3] = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
	// no client.items[4]: this item fails plan-build entirely (unknown ID).
	deps := testStockBulkDeps(client)
	handler := bulkUpdateStockItemMetadata(deps)

	items := []BulkUpdateStockItemMetadataItem{
		{ID: 1, Batch: dvgoutils.Ptr("Applied")},
		{ID: 2, Batch: dvgoutils.Ptr("AlreadyThere")}, // no-op: already at target state
		{ID: 3, Batch: dvgoutils.Ptr("WouldNotApplyCleanly")},
		{ID: 4, Batch: dvgoutils.Ptr("Unknown")},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
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
	a.Equal(2, client.updateCalls)
}

func TestBulkUpdateStockItemMetadataRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	deps := testStockBulkDeps(client)
	handler := bulkUpdateStockItemMetadata(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	tooMany := make([]BulkUpdateStockItemMetadataItem, bulkUpdateMaxItems+1)
	for i := range tooMany {
		tooMany[i] = BulkUpdateStockItemMetadataItem{ID: i + 1}
	}
	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: tooMany, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestBulkUpdateStockItemMetadataAcceptsExactlyMaxItems(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	maxItems := make([]BulkUpdateStockItemMetadataItem, bulkUpdateMaxItems)
	for i := range maxItems {
		id := i + 1
		client.items[id] = inventree.StockItem{PK: id, Batch: dvgoutils.Ptr("Old")}
		maxItems[i] = BulkUpdateStockItemMetadataItem{ID: id, Notes: dvgoutils.Ptr("bulk note")}
	}
	deps := testStockBulkDeps(client)
	handler := bulkUpdateStockItemMetadata(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: maxItems, DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.Len(out.Items, bulkUpdateMaxItems)
	for _, item := range out.Items {
		a.Equal(bulkOutcomePlanned, item.Outcome)
	}
	a.NotEmpty(out.PlanHash)
}

func TestBulkUpdateStockItemMetadataRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("Old")}
	// A 5xx response is ambiguous: the fake still applies the field change
	// before returning the error, simulating a write that landed upstream
	// before the response was lost. Mutate must recover by reading back
	// current state rather than reporting a bare ambiguous failure.
	client.updateErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.applyBeforeErr[1] = true
	deps := testStockBulkDeps(client)
	handler := bulkUpdateStockItemMetadata(deps)

	items := []BulkUpdateStockItemMetadataItem{{ID: 1, Batch: dvgoutils.Ptr("New")}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: items, Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	require.NotNil(t, confirmOut.Items[0].Record.Batch)
	a.Equal("New", *confirmOut.Items[0].Record.Batch)
}

func TestBulkUpdateStockItemMetadataRejectsWhenOutstandingPlanCapacityIsExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("A")}
	client.items[2] = inventree.StockItem{PK: 2, Batch: dvgoutils.Ptr("B")}
	deps := testStockBulkDeps(client)
	deps.stockMetadataBulkPlanStore = mustBulkStore(batch.Options[stockMetadataBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey:           func(p stockMetadataBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		MaxEntriesPerPrincipal: 1,
	})
	handler := bulkUpdateStockItemMetadata(deps)

	_, first, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: []BulkUpdateStockItemMetadataItem{{ID: 1, Batch: dvgoutils.Ptr("A2")}}, DryRun: true})
	r.NoError(err)
	a.NotEmpty(first.PlanHash)

	_, second, err := handler(ctx, &mcp.CallToolRequest{}, BulkUpdateStockItemMetadataInput{Items: []BulkUpdateStockItemMetadataItem{{ID: 2, Batch: dvgoutils.Ptr("B2")}}, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, second.Status)
	a.Empty(second.PlanHash)
}

// TestStockMetadataBulkAdapterPreflightDetectsDrift calls the Adapter's
// Preflight directly with a hand-crafted item whose Before no longer matches
// current state. Reaching this branch through the full dry_run/confirm
// handler flow is impractical: confirm rebuilds the plan from fresh state
// immediately before Store.Consume, so any drift wide enough to observe from
// outside the handler already fails at the digest check (see
// TestBulkUpdateStockItemMetadataRejectsStalePlanHash) before Preflight ever
// runs.
func TestStockMetadataBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("Drifted")}
	adapter := &stockMetadataBulkAdapter{client: client}
	item := stockMetadataBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.StockItem{PK: 1, Batch: dvgoutils.Ptr("Original")},
		Fields:           inventree.PatchFields{"batch": inventree.Set("Target")},
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}

// ---------------------------------------------------------------------------
// bulk_set_stock_status
// ---------------------------------------------------------------------------

func TestBuildStockStatusBulkPlanRejectsHighRiskTarget(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}

	plan := buildStockStatusBulkPlan(ctx, client, []BulkSetStockStatusItem{{ID: 1, Status: stockStatusDestroyed}})
	require.Len(t, plan.Items, 1)
	assert.Equal(t, bulkReasonHighRiskStatus, plan.Items[0].FailReason)
}

func TestBuildStockStatusBulkPlanRejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}

	plan := buildStockStatusBulkPlan(ctx, client, []BulkSetStockStatusItem{{ID: 1, Status: 9999}})
	require.Len(t, plan.Items, 1)
	assert.Equal(t, bulkReasonUnknownStatus, plan.Items[0].FailReason)
}

func TestBuildStockStatusBulkPlanRejectsDuplicateIDWithinBatch(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}

	plan := buildStockStatusBulkPlan(ctx, client, []BulkSetStockStatusItem{
		{ID: 1, Status: stockStatusAttention},
		{ID: 1, Status: stockStatusDamaged},
	})
	require.Len(t, plan.Items, 2)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[0].FailReason)
	assert.Equal(t, bulkReasonDuplicateID, plan.Items[1].FailReason)
}

func TestBulkSetStockStatusRequiresNonblankReason(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: []BulkSetStockStatusItem{{ID: 1, Status: stockStatusAttention}}, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Equal(0, client.statusCalls)
}

func TestBulkSetStockStatusRejectsEmptyAndOversizedBatches(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	_, out, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Reason: "review"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)

	tooMany := make([]BulkSetStockStatusItem, bulkUpdateMaxItems+1)
	for i := range tooMany {
		tooMany[i] = BulkSetStockStatusItem{ID: i + 1, Status: stockStatusAttention}
	}
	// The item-count check must short-circuit before the reason check, so an
	// oversized batch with a blank reason still reports the count problem.
	_, out, err = handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: tooMany, DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("items", out.Clarification.Field)
}

func TestBulkSetStockStatusRejectsChangedReasonBetweenDryRunAndConfirm(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	items := []BulkSetStockStatusItem{{ID: 1, Status: stockStatusAttention}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "original reason", DryRun: true})
	r.NoError(err)
	r.NotEmpty(dryOut.PlanHash)

	// reason is part of the plan digest, so confirming with a different
	// reason than the one reviewed at dry_run must be rejected as stale,
	// exactly like a drifted stock item would be.
	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "different reason", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	a.Equal(0, client.statusCalls)
}

func TestBulkSetStockStatusDryRunThenConfirmApplies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	items := []BulkSetStockStatusItem{{ID: 1, Status: stockStatusAttention}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "quarterly review", DryRun: true})
	r.NoError(err)
	a.Equal(StatusOK, dryOut.Status)
	r.Len(dryOut.Items, 1)
	a.Equal(bulkOutcomePlanned, dryOut.Items[0].Outcome)
	a.NotEmpty(dryOut.PlanHash)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "quarterly review", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusOK, confirmOut.Status)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal(stockStatusAttention, confirmOut.Items[0].Record.Status)
	a.Equal(1, client.statusCalls)
	a.Equal("quarterly review", client.lastStatusNote)
}

func TestBulkSetStockStatusRejectsStalePlanHash(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	items := []BulkSetStockStatusItem{{ID: 1, Status: stockStatusAttention}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "review", DryRun: true})
	r.NoError(err)

	// State drifts after the dry run but before confirm: the digest embedded
	// in the freshly rebuilt plan at confirm time no longer matches, so the
	// stored token must be rejected rather than silently applied.
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusQuarantine}

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "review", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, confirmOut.Status)
	r.NotNil(confirmOut.Clarification)
	a.Equal(0, client.statusCalls)
}

func TestBulkSetStockStatusMixedOutcomesAreIndependent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	client.items[2] = inventree.StockItem{PK: 2, Status: stockStatusAttention}
	client.items[3] = inventree.StockItem{PK: 3, Status: stockStatusOK}
	client.statusErr[3] = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
	// no client.items[4]: this item fails plan-build entirely (unknown ID).
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	items := []BulkSetStockStatusItem{
		{ID: 1, Status: stockStatusAttention},
		{ID: 2, Status: stockStatusAttention}, // no-op: already at target status
		{ID: 3, Status: stockStatusAttention},
		{ID: 4, Status: stockStatusAttention},
	}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "review", DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "review", Confirm: true, PlanHash: dryOut.PlanHash})
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
	a.Equal(2, client.statusCalls)
}

func TestBulkSetStockStatusRecoversWhenMutationResponseIsLost(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	// A 5xx response is ambiguous: the fake still applies the status change
	// before returning the error, simulating a write that landed upstream
	// before the response was lost. Mutate must recover by reading back
	// current state rather than reporting a bare ambiguous failure.
	client.statusErr[1] = &inventree.APIError{StatusCode: http.StatusInternalServerError}
	client.statusApplyBeforeErr[1] = true
	deps := testStockBulkDeps(client)
	handler := bulkSetStockStatus(deps)

	items := []BulkSetStockStatusItem{{ID: 1, Status: stockStatusAttention}}
	_, dryOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "review", DryRun: true})
	r.NoError(err)

	_, confirmOut, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: items, Reason: "review", Confirm: true, PlanHash: dryOut.PlanHash})
	r.NoError(err)
	r.Len(confirmOut.Items, 1)
	a.Equal(string(batch.OutcomeApplied), confirmOut.Items[0].Outcome)
	r.NotNil(confirmOut.Items[0].Record)
	a.Equal(stockStatusAttention, confirmOut.Items[0].Record.Status)
}

func TestBulkSetStockStatusRejectsWhenOutstandingPlanCapacityIsExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusOK}
	client.items[2] = inventree.StockItem{PK: 2, Status: stockStatusOK}
	deps := testStockBulkDeps(client)
	deps.stockStatusBulkPlanStore = mustBulkStore(batch.Options[stockStatusBulkPlan]{
		IDGenerator: platform.RandomIDGenerator{}, Principal: stockPlanPrincipal,
		SupersedeKey:           func(p stockStatusBulkPlan) string { return bulkSupersedeKey(p.ids()) },
		MaxEntriesPerPrincipal: 1,
	})
	handler := bulkSetStockStatus(deps)

	_, first, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: []BulkSetStockStatusItem{{ID: 1, Status: stockStatusAttention}}, Reason: "review", DryRun: true})
	r.NoError(err)
	a.NotEmpty(first.PlanHash)

	_, second, err := handler(ctx, &mcp.CallToolRequest{}, BulkSetStockStatusInput{Items: []BulkSetStockStatusItem{{ID: 2, Status: stockStatusAttention}}, Reason: "review", DryRun: true})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, second.Status)
	a.Empty(second.PlanHash)
}

// TestStockStatusBulkAdapterPreflightDetectsDrift calls the Adapter's
// Preflight directly; see TestStockMetadataBulkAdapterPreflightDetectsDrift
// for why the full handler flow cannot reach this branch.
func TestStockStatusBulkAdapterPreflightDetectsDrift(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	client := newBulkFakeStock()
	client.items[1] = inventree.StockItem{PK: 1, Status: stockStatusDamaged}
	adapter := &stockStatusBulkAdapter{client: client, reason: "review"}
	item := stockStatusBulkPlanItem{
		bulkPlanItemBase: bulkPlanItemBase{ID: 1},
		Before:           inventree.StockItem{PK: 1, Status: stockStatusOK},
		TargetStatus:     stockStatusAttention,
	}

	skip, reason, err := adapter.Preflight(ctx, item)
	a.False(skip)
	a.Error(err)
	a.Equal(bulkReasonDrifted, reason)
}
