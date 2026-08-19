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

type fakeStockInstallClient struct {
	items        map[int]inventree.StockItem
	itemErrs     map[int]error
	locations    map[int]inventree.StockLocation
	locationErrs map[int]error
	bomItems     []inventree.BomItem
	bomErr       error

	installCalls    int
	lastInstallInto int
	lastInstall     inventree.InstallStockItem
	installErr      error
	afterInstall    func(*fakeStockInstallClient)
	installResponse bool

	uninstallCalls    int
	lastUninstallOf   int
	lastUninstall     inventree.UninstallStockItem
	uninstallErr      error
	afterUninstall    func(*fakeStockInstallClient)
	uninstallResponse bool

	planStore *stockPlanStore
}

func newFakeStockInstallClient() *fakeStockInstallClient {
	zeroFloat := 0.0
	zeroInt := 0
	parentLocation := 10
	return &fakeStockInstallClient{
		items: map[int]inventree.StockItem{
			100: {PK: 100, Part: 1, Location: &parentLocation, Quantity: 1, InStock: true, Allocated: &zeroFloat, InstalledItems: &zeroInt, ChildItems: &zeroInt},
			200: {PK: 200, Part: 2, Location: &parentLocation, Quantity: 1, InStock: true, Allocated: &zeroFloat, InstalledItems: &zeroInt, ChildItems: &zeroInt},
		},
		itemErrs:     map[int]error{},
		locations:    map[int]inventree.StockLocation{20: {PK: 20, Name: "Bench"}},
		locationErrs: map[int]error{},
		bomItems:     []inventree.BomItem{{PK: 1, Part: 1, SubPart: 2, Quantity: 1}},
	}
}

func (f *fakeStockInstallClient) GetStockItem(_ context.Context, id int) (inventree.StockItem, error) {
	if err := f.itemErrs[id]; err != nil {
		return inventree.StockItem{}, err
	}
	item, ok := f.items[id]
	if !ok {
		return inventree.StockItem{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return item, nil
}

func (f *fakeStockInstallClient) GetStockLocation(_ context.Context, id int) (inventree.StockLocation, error) {
	if err := f.locationErrs[id]; err != nil {
		return inventree.StockLocation{}, err
	}
	location, ok := f.locations[id]
	if !ok {
		return inventree.StockLocation{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	}
	return location, nil
}

func (f *fakeStockInstallClient) SearchBomItems(context.Context, inventree.BomItemQuery) ([]inventree.BomItem, error) {
	return f.bomItems, f.bomErr
}

func (f *fakeStockInstallClient) InstallStockItem(_ context.Context, parentID int, input inventree.InstallStockItem) error {
	f.installCalls++
	f.lastInstallInto = parentID
	f.lastInstall = input
	if f.installErr != nil {
		return f.installErr
	}
	child := f.items[input.StockItem]
	parentIDCopy := parentID
	child.BelongsTo = &parentIDCopy
	child.Location = nil
	child.InStock = false
	f.items[input.StockItem] = child
	if parent, ok := f.items[parentID]; ok {
		installed := 0
		if parent.InstalledItems != nil {
			installed = *parent.InstalledItems
		}
		installed++
		parent.InstalledItems = &installed
		f.items[parentID] = parent
	}
	if f.afterInstall != nil {
		f.afterInstall(f)
	}
	if f.installResponse {
		return errors.New("injected response loss after install")
	}
	return nil
}

func (f *fakeStockInstallClient) UninstallStockItem(_ context.Context, childID int, input inventree.UninstallStockItem) error {
	f.uninstallCalls++
	f.lastUninstallOf = childID
	f.lastUninstall = input
	if f.uninstallErr != nil {
		return f.uninstallErr
	}
	child := f.items[childID]
	child.BelongsTo = nil
	location := input.Location
	child.Location = &location
	f.items[childID] = child
	if f.afterUninstall != nil {
		f.afterUninstall(f)
	}
	if f.uninstallResponse {
		return errors.New("injected response loss after uninstall")
	}
	return nil
}

func stockInstallDeps(fake *fakeStockInstallClient) Dependencies {
	if fake.planStore == nil {
		fake.planStore = newStockPlanStore(time.Now, randomStockPlanToken)
	}
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }, stockPlanStore: fake.planStore}
}

func TestInstallStockItemFullFlow(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	deps := stockInstallDeps(fake)
	input := InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "assembling unit"}

	_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	r.NotNil(planned.Plan.Install)
	a.Equal(100, planned.Plan.Install.ParentID)
	a.True(planned.Plan.Install.BOMConfirmed)
	a.Equal(200, planned.Plan.Before.StockItemID)
	a.Nil(planned.Plan.After.LocationID)
	a.NotEmpty(planned.PlanHash)
	a.Zero(fake.installCalls)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	a.False(executed.Recovered)
	r.NotNil(executed.Record)
	a.Equal(200, executed.Record.PK)
	r.NotNil(executed.Record.BelongsTo)
	a.Equal(100, *executed.Record.BelongsTo)
	a.Nil(executed.Record.Location)
	a.Equal(1, fake.installCalls)
	a.Equal(100, fake.lastInstallInto)
	a.Equal(200, fake.lastInstall.StockItem)
	a.Equal(1, fake.lastInstall.Quantity)
	a.Equal("assembling unit", fake.lastInstall.Note)

	// The dry-run token is single-use.
	_, replay, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, replay.Status)
}

func TestInstallStockItemValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input InstallStockItemInput
	}{
		{"missing parent", InstallStockItemInput{DryRun: true, ChildStockItemID: 200, Reason: "r"}},
		{"missing child", InstallStockItemInput{DryRun: true, ParentStockItemID: 100, Reason: "r"}},
		{"same id", InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 100, Reason: "r"}},
		{"missing reason", InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, tc.input)
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status)
		})
	}
}

func TestInstallStockItemChildQuantityMustBeOne(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	child.Quantity = 3
	fake.items[200] = child
	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestInstallStockItemNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	t.Run("child", func(t *testing.T) {
		fake := newFakeStockInstallClient()
		_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 999, Reason: "r"})
		r.NoError(err)
		a.Equal(StatusNotFound, out.Status)
	})
	t.Run("parent", func(t *testing.T) {
		fake := newFakeStockInstallClient()
		_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 999, ChildStockItemID: 200, Reason: "r"})
		r.NoError(err)
		a.Equal(StatusClarificationRequired, out.Status)
	})
}

func TestInstallStockItemUnsafeChildStates(t *testing.T) {
	t.Parallel()
	zero := 0.0
	one := 1.0
	someInt := 1
	cases := []struct {
		name   string
		mutate func(*inventree.StockItem)
	}{
		{"allocated unknown", func(item *inventree.StockItem) { item.Allocated = nil }},
		{"allocated", func(item *inventree.StockItem) { item.Allocated = &one }},
		{"is_building", func(item *inventree.StockItem) { item.IsBuilding = true }},
		{"consumed_by", func(item *inventree.StockItem) { item.ConsumedBy = &someInt }},
		{"parent", func(item *inventree.StockItem) { item.Parent = &someInt }},
		{"child_items unknown", func(item *inventree.StockItem) { item.ChildItems = nil }},
		{"child_items", func(item *inventree.StockItem) { item.ChildItems = &someInt }},
		{"customer", func(item *inventree.StockItem) { item.Customer = &someInt }},
		{"sales_order", func(item *inventree.StockItem) { item.SalesOrder = &someInt }},
		{"not in stock", func(item *inventree.StockItem) { item.InStock = false }},
		{"already installed", func(item *inventree.StockItem) { item.BelongsTo = &someInt }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			child := fake.items[200]
			child.Allocated = &zero
			tc.mutate(&child)
			fake.items[200] = child
			_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status, tc.name)
		})
	}
}

func TestInstallStockItemUnsafeParentStates(t *testing.T) {
	t.Parallel()
	one := 1.0
	someInt := 1
	cases := []struct {
		name   string
		mutate func(*inventree.StockItem)
	}{
		{"allocated unknown", func(item *inventree.StockItem) { item.Allocated = nil }},
		{"allocated", func(item *inventree.StockItem) { item.Allocated = &one }},
		{"is_building", func(item *inventree.StockItem) { item.IsBuilding = true }},
		{"consumed_by", func(item *inventree.StockItem) { item.ConsumedBy = &someInt }},
		{"parent", func(item *inventree.StockItem) { item.Parent = &someInt }},
		{"child_items unknown", func(item *inventree.StockItem) { item.ChildItems = nil }},
		{"child_items", func(item *inventree.StockItem) { item.ChildItems = &someInt }},
		{"customer", func(item *inventree.StockItem) { item.Customer = &someInt }},
		{"sales_order", func(item *inventree.StockItem) { item.SalesOrder = &someInt }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			parent := fake.items[100]
			tc.mutate(&parent)
			fake.items[100] = parent
			_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status, tc.name)
		})
	}
}

// Nested-assembly states are explicitly allowed, not blocked: the parent may
// already be installed somewhere else, may already contain other installed
// items, and the child may itself already contain further installed items.
func TestInstallStockItemAllowsNestedAssemblyStates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	grandparentID := 300
	fake.items[grandparentID] = inventree.StockItem{PK: grandparentID, Part: 9}
	parent := fake.items[100]
	parent.BelongsTo = &grandparentID
	installedAlready := 2
	parent.InstalledItems = &installedAlready
	fake.items[100] = parent
	child := fake.items[200]
	nestedInstalled := 1
	child.InstalledItems = &nestedInstalled
	fake.items[200] = child

	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
}

func TestInstallStockItemRejectsOutOfBOMPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	fake.bomItems = nil
	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestInstallStockItemDirectCycleRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	parent := fake.items[100]
	childID := 200
	parent.BelongsTo = &childID
	fake.items[100] = parent
	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.installCalls)
}

func TestInstallStockItemDeepCycleRejectedWithinBudget(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	// parent(100) -> 301 -> 302 -> child(200): installing child into parent
	// would create a cycle three hops up.
	fake.items[301] = inventree.StockItem{PK: 301, Part: 9}
	item301 := fake.items[301]
	link302 := 302
	item301.BelongsTo = &link302
	fake.items[301] = item301
	fake.items[302] = inventree.StockItem{PK: 302, Part: 9}
	item302 := fake.items[302]
	childID := 200
	item302.BelongsTo = &childID
	fake.items[302] = item302
	parent := fake.items[100]
	link301 := 301
	parent.BelongsTo = &link301
	fake.items[100] = parent

	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestInstallStockItemAncestorBudgetExceeded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	// Build a long chain of stock items each belonging to the next, longer
	// than the shared traversal budget, none of which ever reaches the
	// child -- so the walk must fail closed rather than loop forever.
	const chainLength = stockInstallTopologyMaxRecords + 5
	previousID := 100
	for i := 0; i < chainLength; i++ {
		nextID := 1000 + i
		item := fake.items[previousID]
		linked := nextID
		item.BelongsTo = &linked
		fake.items[previousID] = item
		fake.items[nextID] = inventree.StockItem{PK: nextID, Part: 9}
		previousID = nextID
	}

	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

// TestInstallStockItemPreexistingCycleInAncestorDataRejected covers a short
// loop already present in the parent's ancestor data (parent -> 301 -> 302
// -> 301) that revisits an already-seen node without ever reaching the
// child and without exceeding the traversal budget.
func TestInstallStockItemPreexistingCycleInAncestorDataRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	link301, link302 := 301, 302
	parent := fake.items[100]
	parent.BelongsTo = &link301
	fake.items[100] = parent
	fake.items[301] = inventree.StockItem{PK: 301, Part: 9, BelongsTo: &link302}
	fake.items[302] = inventree.StockItem{PK: 302, Part: 9, BelongsTo: &link301}

	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.installCalls)
}

func TestInstallStockItemAncestorNotFoundFailsClosed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	missingAncestor := 999
	parent := fake.items[100]
	parent.BelongsTo = &missingAncestor
	fake.items[100] = parent

	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.installCalls)
}

// TestInstallStockItemAncestorLookupErrorFailsClosed is a regression test
// for a bug caught in review: a non-NotFound error while walking the
// parent's belongs_to ancestor chain must fail closed (refuse the install)
// rather than silently treating the relationship as cycle-free.
func TestInstallStockItemAncestorLookupErrorFailsClosed(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	link301 := 301
	parent := fake.items[100]
	parent.BelongsTo = &link301
	fake.items[100] = parent
	fake.itemErrs[301] = errors.New("transient upstream failure")

	_, out, err := installStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.installCalls)
}

func TestInstallStockItemMutateContextErrorPropagates(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			deps := stockInstallDeps(fake)
			_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
			r.NoError(err)
			fake.installErr = sentinel
			_, _, err = installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
			r.ErrorIs(err, sentinel)
		})
	}
}

func TestInstallStockItemVerifyRefetchContextErrorPropagates(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			deps := stockInstallDeps(fake)
			_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
			r.NoError(err)
			fake.afterInstall = func(f *fakeStockInstallClient) { f.itemErrs[200] = sentinel }
			_, _, err = installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
			r.ErrorIs(err, sentinel)
		})
	}
}

func TestInstallStockItemDefiniteRejectionPropagatesError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	deps := stockInstallDeps(fake)
	_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)

	fake.installErr = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
	_, _, err = installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.Error(err)
}

func TestInstallStockItemUnknownResultRecovers(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	deps := stockInstallDeps(fake)
	_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)

	fake.installResponse = true
	_, out, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	a.True(out.Recovered)
}

func TestInstallStockItemVerifyMismatchIsPartialFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	deps := stockInstallDeps(fake)
	_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)

	fake.afterInstall = func(f *fakeStockInstallClient) {
		child := f.items[200]
		child.BelongsTo = nil
		f.items[200] = child
	}
	_, out, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	r.NotNil(out.Failure)
}

func TestInstallStockItemStaleTokenRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	deps := stockInstallDeps(fake)
	_, out, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{Confirm: true, PlanHash: "not-a-real-token", ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.installCalls)
}

func TestInstallStockItemMissingConfirmRequiresClarification(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	deps := stockInstallDeps(fake)
	_, planned, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{DryRun: true, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	_, out, err := installStockItem(deps)(ctx, &mcp.CallToolRequest{}, InstallStockItemInput{PlanHash: planned.PlanHash, ParentStockItemID: 100, ChildStockItemID: 200, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestUninstallStockItemFullFlow(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	child.Location = nil
	child.InStock = false
	fake.items[200] = child
	deps := stockInstallDeps(fake)
	input := UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "removing for repair"}

	_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, planned.Status)
	r.NotNil(planned.Plan)
	r.NotNil(planned.Plan.Install)
	a.Equal(100, planned.Plan.Install.ParentID)
	r.NotEmpty(planned.PlanHash)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.True(executed.Verified)
	r.NotNil(executed.Record)
	a.Nil(executed.Record.BelongsTo)
	r.NotNil(executed.Record.Location)
	a.Equal(20, *executed.Record.Location)
	a.Equal(1, fake.uninstallCalls)
	a.Equal(200, fake.lastUninstallOf)
	a.Equal(20, fake.lastUninstall.Location)
	a.Equal("removing for repair", fake.lastUninstall.Note)
}

func TestUninstallStockItemRequiresExistingBelongsTo(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	_, out, err := uninstallStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.uninstallCalls)
}

func TestUninstallStockItemValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input UninstallStockItemInput
	}{
		{"missing stock item", UninstallStockItemInput{DryRun: true, DestinationLocationID: 20, Reason: "r"}},
		{"missing destination", UninstallStockItemInput{DryRun: true, StockItemID: 200, Reason: "r"}},
		{"missing reason", UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			_, out, err := uninstallStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, tc.input)
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status)
		})
	}
}

func TestUninstallStockItemNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	_, out, err := uninstallStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 999, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusNotFound, out.Status)
}

func TestUninstallStockItemDestinationNotFound(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	_, out, err := uninstallStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 999, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestUninstallStockItemUnsafeStates(t *testing.T) {
	t.Parallel()
	one := 1.0
	someInt := 1
	cases := []struct {
		name   string
		mutate func(*inventree.StockItem)
	}{
		{"allocated", func(item *inventree.StockItem) { item.Allocated = &one }},
		{"is_building", func(item *inventree.StockItem) { item.IsBuilding = true }},
		{"consumed_by", func(item *inventree.StockItem) { item.ConsumedBy = &someInt }},
		{"customer", func(item *inventree.StockItem) { item.Customer = &someInt }},
		{"sales_order", func(item *inventree.StockItem) { item.SalesOrder = &someInt }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			child := fake.items[200]
			parentID := 100
			child.BelongsTo = &parentID
			tc.mutate(&child)
			fake.items[200] = child
			_, out, err := uninstallStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status, tc.name)
		})
	}
}

func TestUninstallStockItemStaleTokenRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	_, out, err := uninstallStockItem(stockInstallDeps(fake))(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{Confirm: true, PlanHash: "bogus", StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.uninstallCalls)
}

func TestUninstallStockItemDefiniteRejectionPropagatesError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	deps := stockInstallDeps(fake)
	_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)

	fake.uninstallErr = &inventree.APIError{StatusCode: http.StatusBadRequest, Kind: inventree.ErrorKindValidation}
	_, _, err = uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.Error(err)
}

func TestUninstallStockItemUnknownResultRecovers(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	deps := stockInstallDeps(fake)
	_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)

	fake.uninstallResponse = true
	_, out, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.True(out.Verified)
	a.True(out.Recovered)
}

func TestUninstallStockItemVerifyMismatchIsPartialFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	deps := stockInstallDeps(fake)
	_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)

	fake.afterUninstall = func(f *fakeStockInstallClient) {
		item := f.items[200]
		item.BelongsTo = &parentID
		f.items[200] = item
	}
	_, out, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusPartialFailure, out.Status)
	r.NotNil(out.Failure)
}

func TestUninstallStockItemMissingConfirmRequiresClarification(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	deps := stockInstallDeps(fake)
	_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	_, out, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{PlanHash: planned.PlanHash, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	a.Zero(fake.uninstallCalls)
}

func TestUninstallStockItemTokenIsSingleUse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockInstallClient()
	child := fake.items[200]
	parentID := 100
	child.BelongsTo = &parentID
	fake.items[200] = child
	deps := stockInstallDeps(fake)
	input := UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"}
	_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)

	input.DryRun = false
	input.Confirm = true
	input.PlanHash = planned.PlanHash
	_, executed, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusOK, executed.Status)
	a.Equal(1, fake.uninstallCalls)

	_, replay, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusClarificationRequired, replay.Status)
	a.Equal(1, fake.uninstallCalls)
}

func TestUninstallStockItemMutateContextErrorPropagates(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			child := fake.items[200]
			parentID := 100
			child.BelongsTo = &parentID
			fake.items[200] = child
			deps := stockInstallDeps(fake)
			_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
			r.NoError(err)
			fake.uninstallErr = sentinel
			_, _, err = uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
			r.ErrorIs(err, sentinel)
		})
	}
}

func TestUninstallStockItemVerifyRefetchContextErrorPropagates(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			r := require.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			fake := newFakeStockInstallClient()
			child := fake.items[200]
			parentID := 100
			child.BelongsTo = &parentID
			fake.items[200] = child
			deps := stockInstallDeps(fake)
			_, planned, err := uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{DryRun: true, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
			r.NoError(err)
			fake.afterUninstall = func(f *fakeStockInstallClient) { f.itemErrs[200] = sentinel }
			_, _, err = uninstallStockItem(deps)(ctx, &mcp.CallToolRequest{}, UninstallStockItemInput{Confirm: true, PlanHash: planned.PlanHash, StockItemID: 200, DestinationLocationID: 20, Reason: "r"})
			r.ErrorIs(err, sentinel)
		})
	}
}
