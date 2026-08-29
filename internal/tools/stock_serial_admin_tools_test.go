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

// safeStockItem builds a stock item with every unsafeStockSerialChange
// relationship guard already in its safe state (zero allocation, no
// installed/child items, no build/customer/sales-order linkage), so tests
// that exercise unrelated behavior do not trip the safety guard.
func safeStockItem(pk, part int, quantity float64, serial *string) inventree.StockItem {
	zero := 0.0
	zeroInt := 0
	return inventree.StockItem{PK: pk, Part: part, Quantity: quantity, Serial: serial, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt}
}

func TestAssignStockSerialPlansAndConfirmsAssignment(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true}}
	fake.item.Status = stockStatusOK
	input := AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: " 1001 ", Reason: "assign serial for new receipt"}

	_, planned, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	a.Nil(planned.Plan.Before.Serial)
	r.NotNil(planned.Plan.After.Serial)
	a.Equal("1001", *planned.Plan.After.Serial)
	a.False(planned.Plan.HighRisk)
	a.NotEmpty(planned.PlanHash)
	a.Zero(fake.updateCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	r.NotNil(executed.Record)
	r.NotNil(executed.Record.Serial)
	a.Equal("1001", *executed.Record.Serial)
	a.Equal(1, fake.updateCalls)
	a.Equal("1001", fake.lastPatch["serial"].Value())
}

func TestAssignStockSerialRejectsBlankSerialAlreadySerializedAndMultiQuantity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	deps := stockAdjustmentDeps(&fakeStockAdjustmentClient{})

	_, blank, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "  ", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, blank.Status)
	a.Equal("serial", blank.Clarification.Field)

	existing := "77"
	deps = stockAdjustmentDeps(&fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Serial: &existing}})
	_, already, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "78", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, already.Status)
	a.Equal("serial", already.Clarification.Field)
	a.Contains(already.Clarification.Reason, "already has a serial number")

	deps = stockAdjustmentDeps(&fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 4}})
	_, multi, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "78", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, multi.Status)
	a.Equal("quantity", multi.Clarification.Field)
}

func TestAssignStockSerialTreatsEmptyOrWhitespaceExistingSerialAsUnserialized(t *testing.T) {
	t.Parallel()
	empty := ""
	whitespace := "   "

	for _, tc := range []struct {
		name   string
		serial *string
	}{
		{name: "empty existing serial", serial: &empty},
		{name: "whitespace-only existing serial", serial: &whitespace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, tc.serial), part: inventree.Part{PK: 10, Trackable: true}}

			_, planned, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "1001", Reason: "assign serial for new receipt"})
			r.NoError(err)
			a.Equal(StatusOK, planned.Status)
			r.Nil(planned.Clarification)
		})
	}
}

func TestAssignStockSerialRejectsNonTrackablePartAndDuplicateSerial(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: false}}
	_, notTrackable, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, notTrackable.Status)
	a.Equal("part_id", notTrackable.Clarification.Field)

	conflicting := "5"
	fake = &fakeStockAdjustmentClient{
		item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true},
		searchResults: []inventree.StockItem{{PK: 51, Part: 10, Serial: &conflicting}},
	}
	_, duplicate, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, duplicate.Status)
	a.Equal("serial", duplicate.Clarification.Field)
	a.Equal(51, duplicate.Clarification.RetryValues["conflicting_stock_item_id"])
}

func TestAssignStockSerialRejectsMismatchedPartIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 999, Trackable: true}}

	_, _, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.ErrorContains(err, "mismatched part identity")
}

func TestAssignStockSerialRejectsUnsafeRelationshipStates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	allocated := 3.0
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true}}
	fake.item.Allocated = &allocated

	_, output, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("allocated", output.Clarification.Field)
	a.Zero(fake.updateCalls)
}

func TestAssignStockSerialEnforcesGlobalUniquenessWhenEnabled(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	conflicting := "5"
	fake := &fakeStockAdjustmentClient{
		item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true},
		setting: inventree.SettingValue{Key: serialGloballyUniqueSetting, Value: "true"},
		searchResultsSequence: [][]inventree.StockItem{
			{},
			{{PK: 61, Part: 20, Serial: &conflicting}},
		},
	}
	_, output, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("serial", output.Clarification.Field)
	a.Equal(61, output.Clarification.RetryValues["conflicting_stock_item_id"])
	a.Equal(2, fake.searchCalls)
}

func TestAssignStockSerialSkipsGlobalCheckOnOmittableSettingError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{
		item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true},
		getSettingErr: &inventree.APIError{StatusCode: 403, Kind: inventree.ErrorKindPermission},
	}
	_, output, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	a.Equal(StatusOK, output.Status)
	a.Equal(1, fake.searchCalls)
}

func TestAssignStockSerialFailsHardOnNonOmittableSettingError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{
		item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true},
		getSettingErr: errors.New("network timeout"),
	}
	_, _, err := assignStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.Error(err)
	r.Zero(fake.updateCalls)
}

// TestAssignStockSerialRejectsStaleAndSupersededPlans covers state-drift and
// newer-dry-run-supersedes-older-token staleness. Reuse of an already-
// consumed token after a successful assignment cannot be observed the same
// way sibling idempotent-ish tools test it: assign_stock_serial's own
// precondition ("item already has a serial number") fires first once the
// item is serialized, which is a clearer error than a generic stale-token
// message and is covered by TestAssignStockSerialRejectsBlankSerialAlreadySerializedAndMultiQuantity.
func TestAssignStockSerialRejectsStaleAndSupersededPlans(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, nil), part: inventree.Part{PK: 10, Trackable: true}}
	deps := stockAdjustmentDeps(fake)

	input := AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"}
	_, planned, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	// Status is part of the state-bound plan but not an assignment precondition,
	// so changing it (rather than quantity) reaches the token-staleness check
	// instead of re-tripping the quantity precondition.
	fake.item.Status = stockStatusAttention
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, stale, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Equal("confirmation", stale.Clarification.Field)
	a.Zero(fake.updateCalls)

	fake.item.Status = stockStatusOK
	_, superseded, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	_, refreshed, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, AssignStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "assign"})
	r.NoError(err)
	input.PlanHash = superseded.PlanHash
	_, supersededResult, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supersededResult.Status)
	a.Equal("confirmation", supersededResult.Clarification.Field)
	a.Zero(fake.updateCalls)

	input.PlanHash = refreshed.PlanHash
	_, executed, err := assignStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Equal(1, fake.updateCalls)
}

func TestSetStockSerialPlansAndConfirmsReplacement(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existing := "1001"
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, &existing)}
	input := SetStockSerialInput{DryRun: true, StockItemID: 50, Serial: "1002", Reason: "correct mislabeled unit"}

	_, planned, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	a.Equal("1001", *planned.Plan.Before.Serial)
	a.Equal("1002", *planned.Plan.After.Serial)
	a.True(planned.Plan.HighRisk)
	a.NotEmpty(planned.Plan.RiskReason)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	r.NotNil(executed.Record.Serial)
	a.Equal("1002", *executed.Record.Serial)
	a.Equal("1002", fake.lastPatch["serial"].Value())
}

func TestSetStockSerialPlansAndConfirmsClear(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existing := "1001"
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, &existing)}
	input := SetStockSerialInput{DryRun: true, StockItemID: 50, ClearSerial: true, Reason: "unlabel damaged unit"}

	_, planned, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	a.Nil(planned.Plan.After.Serial)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Nil(executed.Record.Serial)
	a.Nil(fake.lastPatch["serial"].Value())
}

func TestSetStockSerialRejectsAmbiguousInputUnserializedAndNoOp(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	deps := stockAdjustmentDeps(&fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1}})
	_, neither, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, Reason: "fix"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, neither.Status)
	a.Equal("serial", neither.Clarification.Field)

	_, both, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", ClearSerial: true, Reason: "fix"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, both.Status)
	a.Equal("serial", both.Clarification.Field)

	_, unserialized, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "fix"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, unserialized.Status)
	a.Contains(unserialized.Clarification.Reason, "no serial number yet")

	existing := "5"
	deps = stockAdjustmentDeps(&fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Serial: &existing}})
	_, noOp, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "fix"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, noOp.Status)
	a.Contains(noOp.Clarification.Reason, "already has this serial number")
}

func TestSetStockSerialTreatsEmptyOrWhitespaceExistingSerialAsUnserialized(t *testing.T) {
	t.Parallel()
	empty := ""
	whitespace := "   "

	for _, tc := range []struct {
		name   string
		serial *string
	}{
		{name: "empty existing serial", serial: &empty},
		{name: "whitespace-only existing serial", serial: &whitespace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			deps := stockAdjustmentDeps(&fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Serial: tc.serial}})

			_, unserialized, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, Serial: "5", Reason: "fix"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, unserialized.Status)
			a.Contains(unserialized.Clarification.Reason, "no serial number yet")
		})
	}
}

// TestUnsafeStockSerialChangeRejectsEveryUnsafeRelationshipState exercises
// every branch of unsafeStockSerialChange directly so each relationship
// guard (shared by assign_stock_serial and set_stock_serial) is proven
// independently, matching the acceptance criteria's "refuse ... allocation,
// parent/child, build, consumption, installation, or provenance states".
func TestUnsafeStockSerialChangeRejectsEveryUnsafeRelationshipState(t *testing.T) {
	t.Parallel()
	zero := 0.0
	zeroInt := 0
	one := 1.0
	oneInt := 1
	otherID := 7

	cases := []struct {
		name  string
		item  inventree.StockItem
		field string
	}{
		{"allocation context unavailable", inventree.StockItem{PK: 50}, "allocated"},
		{"allocated", inventree.StockItem{PK: 50, Allocated: &one, InstalledItems: &zeroInt, ChildItems: &zeroInt}, "allocated"},
		{"building", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt, IsBuilding: true}, "is_building"},
		{"consumed", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt, ConsumedBy: &otherID}, "consumed_by"},
		{"belongs to", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt, BelongsTo: &otherID}, "belongs_to"},
		{"parent", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt, Parent: &otherID}, "parent"},
		{"installed-item context unavailable", inventree.StockItem{PK: 50, Allocated: &zero}, "installed_items"},
		{"installed items", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &oneInt, ChildItems: &zeroInt}, "installed_items"},
		{"child-item context unavailable", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt}, "child_items"},
		{"child items", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &oneInt}, "child_items"},
		{"customer", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt, Customer: &otherID}, "customer"},
		{"sales order", inventree.StockItem{PK: 50, Allocated: &zero, InstalledItems: &zeroInt, ChildItems: &zeroInt, SalesOrder: &otherID}, "sales_order"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)
			result, out, unsafe := unsafeStockSerialChange(StockAdjustmentOutput{Status: StatusOK}, tc.item)
			a.True(unsafe)
			a.NotNil(result)
			a.Equal(StatusClarificationRequired, out.Status)
			a.Equal(tc.field, out.Clarification.Field)
		})
	}

	a := assert.New(t)
	safe := safeStockItem(50, 10, 1, nil)
	_, _, unsafe := unsafeStockSerialChange(StockAdjustmentOutput{Status: StatusOK}, safe)
	a.False(unsafe)
}

func TestSetStockSerialRejectsUnsafeRelationshipStates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existing := "5"
	allocated := 2.0
	fake := &fakeStockAdjustmentClient{item: inventree.StockItem{PK: 50, Part: 10, Quantity: 1, Serial: &existing, Allocated: &allocated}}

	_, output, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, ClearSerial: true, Reason: "fix"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("allocated", output.Clarification.Field)
	a.Zero(fake.updateCalls)
}

func TestSetStockSerialRejectsDuplicateReplacement(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existing := "5"
	conflicting := "6"
	fake := &fakeStockAdjustmentClient{
		item:          safeStockItem(50, 10, 1, &existing),
		searchResults: []inventree.StockItem{{PK: 52, Part: 10, Serial: &conflicting}},
	}
	_, output, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, Serial: "6", Reason: "fix"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, output.Status)
	a.Equal("serial", output.Clarification.Field)
	a.Equal(52, output.Clarification.RetryValues["conflicting_stock_item_id"])
}

func TestSetStockSerialRecoversUnknownResultOnMutationFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existing := "5"
	fake := &fakeStockAdjustmentClient{
		item:      safeStockItem(50, 10, 1, &existing),
		mutateErr: errors.New("timeout"),
	}
	input := SetStockSerialInput{DryRun: true, StockItemID: 50, ClearSerial: true, Reason: "fix"}
	_, planned, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, output, err := setStockSerial(stockAdjustmentDeps(fake))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusPartialFailure, output.Status)
	r.NotNil(output.Failure)
}

// TestSetStockSerialRejectsStaleAndSupersededPlans covers state-drift and
// newer-dry-run-supersedes-older-token staleness; see
// TestAssignStockSerialRejectsStaleAndSupersededPlans for why reuse after a
// successful clear cannot be observed the same way (the item's changed
// serial state is itself caught by an earlier, clearer precondition).
func TestSetStockSerialRejectsStaleAndSupersededPlans(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	existing := "5"
	fake := &fakeStockAdjustmentClient{item: safeStockItem(50, 10, 1, &existing)}
	deps := stockAdjustmentDeps(fake)

	input := SetStockSerialInput{DryRun: true, StockItemID: 50, ClearSerial: true, Reason: "fix"}
	_, planned, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	other := "6"
	fake.item.Serial = &other
	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, stale, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, stale.Status)
	a.Equal("confirmation", stale.Clarification.Field)
	a.Zero(fake.updateCalls)

	fake.item.Serial = &existing
	_, superseded, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, ClearSerial: true, Reason: "fix"})
	r.NoError(err)
	_, refreshed, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, SetStockSerialInput{DryRun: true, StockItemID: 50, ClearSerial: true, Reason: "fix"})
	r.NoError(err)
	input.PlanHash = superseded.PlanHash
	_, supersededResult, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, supersededResult.Status)
	a.Equal("confirmation", supersededResult.Clarification.Field)
	a.Zero(fake.updateCalls)

	input.PlanHash = refreshed.PlanHash
	_, executed, err := setStockSerial(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Equal(1, fake.updateCalls)
}

func TestAssignStockSerialAuthorizationIsOperationalOnly(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Equal("operational", ToolAuthorizations[AssignStockSerialToolName].MutationClass)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational}, ToolAuthorizations[AssignStockSerialToolName].Scopes)
	a.Equal(WriteAnnotations, ToolAuthorizations[AssignStockSerialToolName].Annotations)
}

func TestSetStockSerialAuthorizationIsDestructive(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	a.Equal("destructive", ToolAuthorizations[SetStockSerialToolName].MutationClass)
	a.Equal([]string{ScopeInventreeRead, ScopeInventreeWrite, ScopeInventreeOperational, ScopeInventreeDestructive}, ToolAuthorizations[SetStockSerialToolName].Scopes)
	expected := WriteAnnotations
	expected.Destructive = true
	a.Equal(expected, ToolAuthorizations[SetStockSerialToolName].Annotations)
}

func TestStockSerialToolsMCPWireContract(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeStockAdjustmentClient{
		item: safeStockItem(50, 10, 1, nil),
		part: inventree.Part{PK: 10, Trackable: true},
	}
	fake.item.Status = stockStatusOK
	session, closeSession := plannedChangesSession(t, ctx, fake)
	defer closeSession()

	plannedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: AssignStockSerialToolName, Arguments: map[string]any{
		"dry_run": true, "stock_item_id": 50, "serial": "2001", "reason": "assign new receipt",
	}})
	r.NoError(err)
	a.False(plannedResult.IsError)
	planned := plannedResult.StructuredContent.(map[string]any)
	planHash := planned["plan_hash"]

	executedResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: AssignStockSerialToolName, Arguments: map[string]any{
		"stock_item_id": 50, "serial": "2001", "reason": "assign new receipt", "confirm": true, "plan_hash": planHash,
	}})
	r.NoError(err)
	a.False(executedResult.IsError)
	executed := executedResult.StructuredContent.(map[string]any)
	a.Equal(StatusOK, executed["status"])
	a.Equal("2001", executed["record"].(map[string]any)["serial"])

	clearPlan, err := session.CallTool(ctx, &mcp.CallToolParams{Name: SetStockSerialToolName, Arguments: map[string]any{
		"dry_run": true, "stock_item_id": 50, "clear_serial": true, "reason": "remove mislabeled serial",
	}})
	r.NoError(err)
	a.False(clearPlan.IsError)
	clearPlanned := clearPlan.StructuredContent.(map[string]any)
	a.Equal(true, clearPlanned["plan"].(map[string]any)["high_risk"])
}
