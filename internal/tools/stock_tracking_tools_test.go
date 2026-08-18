package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStockTrackingClient struct {
	entries    map[int]inventree.StockTracking
	stocktakes map[int]inventree.PartStocktake
	pageErr    error
	queries    []inventree.StockTrackingQuery
	stkQueries []inventree.PartStocktakeQuery
}

func newFakeStockTrackingClient(entries ...inventree.StockTracking) *fakeStockTrackingClient {
	f := &fakeStockTrackingClient{entries: map[int]inventree.StockTracking{}, stocktakes: map[int]inventree.PartStocktake{}}
	for _, entry := range entries {
		f.entries[entry.PK] = entry
	}
	return f
}

func (f *fakeStockTrackingClient) SearchStockTrackingPage(_ context.Context, query inventree.StockTrackingQuery) (inventree.Page[inventree.StockTracking], error) {
	f.queries = append(f.queries, query)
	if f.pageErr != nil {
		return inventree.Page[inventree.StockTracking]{}, f.pageErr
	}
	all := make([]inventree.StockTracking, 0, len(f.entries))
	for _, entry := range f.entries {
		if query.ItemID > 0 && (entry.Item == nil || *entry.Item != query.ItemID) {
			continue
		}
		if query.PartID > 0 && (entry.Part == nil || *entry.Part != query.PartID) {
			continue
		}
		all = append(all, entry)
	}
	return pageSlice(all, query.Offset, query.Limit), nil
}

func (f *fakeStockTrackingClient) GetStockTrackingEntry(_ context.Context, id int) (inventree.StockTracking, error) {
	entry, ok := f.entries[id]
	if !ok {
		return inventree.StockTracking{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return entry, nil
}

func (f *fakeStockTrackingClient) SearchPartStocktakesPage(_ context.Context, query inventree.PartStocktakeQuery) (inventree.Page[inventree.PartStocktake], error) {
	f.stkQueries = append(f.stkQueries, query)
	if f.pageErr != nil {
		return inventree.Page[inventree.PartStocktake]{}, f.pageErr
	}
	all := make([]inventree.PartStocktake, 0, len(f.stocktakes))
	for _, record := range f.stocktakes {
		if query.PartID > 0 && record.Part != query.PartID {
			continue
		}
		all = append(all, record)
	}
	return pageSlice(all, query.Offset, query.Limit), nil
}

func (f *fakeStockTrackingClient) GetPartStocktake(_ context.Context, id int) (inventree.PartStocktake, error) {
	record, ok := f.stocktakes[id]
	if !ok {
		return inventree.PartStocktake{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func stockTrackingDeps(fake *fakeStockTrackingClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestListStockTrackingEntriesRequiresStockItemOrPart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()

	_, out, err := listStockTrackingEntries(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListStockTrackingEntriesInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
	r.NotNil(out.Clarification)
	a.Equal("stock_item_id", out.Clarification.Field)
}

func TestListStockTrackingEntriesFiltersSortsAndPaginates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient(
		inventree.StockTracking{PK: 3, Item: dvgoutils.Ptr(1), Label: "c", Deltas: map[string]any{}},
		inventree.StockTracking{PK: 1, Item: dvgoutils.Ptr(1), Label: "a", Notes: "first", Deltas: map[string]any{"quantity": 10.0}},
		inventree.StockTracking{PK: 2, Item: dvgoutils.Ptr(1), Label: "b", Deltas: map[string]any{}},
		inventree.StockTracking{PK: 4, Item: dvgoutils.Ptr(2), Label: "other item", Deltas: map[string]any{}},
	)

	_, out, err := listStockTrackingEntries(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListStockTrackingEntriesInput{StockItemID: 1})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(3, out.Count)
	r.Len(out.Results, 3)
	a.Equal([]int{1, 2, 3}, []int{out.Results[0].PK, out.Results[1].PK, out.Results[2].PK})
	// Concise list results omit notes; the field simply is not present in this struct.
	a.Equal(1, fake.queries[len(fake.queries)-1].ItemID)

	_, page, err := listStockTrackingEntries(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListStockTrackingEntriesInput{StockItemID: 1, Limit: 1, Offset: 1})
	r.NoError(err)
	a.Equal(3, page.Count)
	r.Len(page.Results, 1)
	a.Equal(2, page.Results[0].PK)
}

func TestGetStockTrackingEntryIncludesNotesAndSanitizedDeltas(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient(inventree.StockTracking{
		PK:     5,
		Item:   dvgoutils.Ptr(1),
		Label:  "Location changed",
		Notes:  "moved to overflow",
		Deltas: map[string]any{"location": 2.0, "quantity": 20.0},
	})

	_, out, err := getStockTrackingEntry(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 5})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.NotNil(out.Record)
	a.Equal("moved to overflow", out.Record.Notes)
	a.Equal(map[string]any{"location": 2.0, "quantity": 20.0}, out.Record.Deltas)

	_, missing, err := getStockTrackingEntry(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)
	a.Nil(missing.Record)
}

func TestGetStockTrackingEntryRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()

	for _, id := range []int{0, -1} {
		_, out, err := getStockTrackingEntry(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: id})
		r.NoError(err)
		a.Equal(StatusNotFound, out.Status)
		a.Nil(out.Record)
	}
}

func TestGetStockTrackingEntryRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient(inventree.StockTracking{PK: 5, Deltas: map[string]any{}})
	fake.entries[7] = fake.entries[5] // simulate InvenTree returning a mismatched record for ID 7

	_, _, err := getStockTrackingEntry(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 7})
	r.Error(err)
	a.Contains(err.Error(), "mismatched")
}

func TestListStockTrackingEntriesOffsetBeyondCountReturnsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient(inventree.StockTracking{PK: 1, Item: dvgoutils.Ptr(1), Deltas: map[string]any{}})

	_, out, err := listStockTrackingEntries(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListStockTrackingEntriesInput{StockItemID: 1, Offset: 1000})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(1, out.Count)
	a.Empty(out.Results)
}

func TestListStockTrackingEntriesAndsBothFilters(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient(
		inventree.StockTracking{PK: 1, Item: dvgoutils.Ptr(1), Part: dvgoutils.Ptr(9), Deltas: map[string]any{}},
		inventree.StockTracking{PK: 2, Item: dvgoutils.Ptr(1), Part: dvgoutils.Ptr(8), Deltas: map[string]any{}},
	)

	_, out, err := listStockTrackingEntries(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListStockTrackingEntriesInput{StockItemID: 1, PartID: 9})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	r.Len(out.Results, 1, "both filters must be ANDed server-side, not ORed")
	a.Equal(1, out.Results[0].PK)
	last := fake.queries[len(fake.queries)-1]
	a.Equal(1, last.ItemID)
	a.Equal(9, last.PartID)
}

func TestListStockTrackingEntriesFailsClosedOnScanBudget(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()
	fake.pageErr = errors.New("boom")

	_, _, err := listStockTrackingEntries(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListStockTrackingEntriesInput{StockItemID: 1})
	r.Error(err)
	a.Contains(err.Error(), "boom")
}

func TestListPartStocktakesRequiresPartID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()

	_, out, err := listPartStocktakes(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartStocktakesInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestListPartStocktakesSortsAndPaginates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()
	fake.stocktakes[2] = inventree.PartStocktake{PK: 2, Part: 7, ItemCount: 3, Quantity: 30}
	fake.stocktakes[1] = inventree.PartStocktake{PK: 1, Part: 7, ItemCount: 2, Quantity: 20}
	fake.stocktakes[3] = inventree.PartStocktake{PK: 3, Part: 9, ItemCount: 1, Quantity: 5}

	_, out, err := listPartStocktakes(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartStocktakesInput{PartID: 7})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(2, out.Count)
	r.Len(out.Results, 2)
	a.Equal([]int{1, 2}, []int{out.Results[0].PK, out.Results[1].PK})
}

func TestGetPartStocktakeReturnsExactSnapshot(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()
	fake.stocktakes[1] = inventree.PartStocktake{PK: 1, Part: 7, ItemCount: 2, Quantity: 20}

	_, out, err := getPartStocktake(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 1})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(7, out.Record.Part)

	_, missing, err := getPartStocktake(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)
}

func TestGetPartStocktakeRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()

	for _, id := range []int{0, -1} {
		_, out, err := getPartStocktake(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: id})
		r.NoError(err)
		a.Equal(StatusNotFound, out.Status)
	}
}

func TestGetPartStocktakeRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()
	fake.stocktakes[5] = inventree.PartStocktake{PK: 5, Part: 7}
	fake.stocktakes[9] = fake.stocktakes[5] // simulate InvenTree returning a mismatched record for ID 9

	_, _, err := getPartStocktake(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 9})
	r.Error(err)
	a.Contains(err.Error(), "mismatched")
}

func TestListPartStocktakesOffsetBeyondCountReturnsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakeStockTrackingClient()
	fake.stocktakes[1] = inventree.PartStocktake{PK: 1, Part: 7}

	_, out, err := listPartStocktakes(stockTrackingDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartStocktakesInput{PartID: 7, Offset: 1000})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(1, out.Count)
	a.Empty(out.Results)
}
