package tools

import (
	"context"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePartStocktakeClient struct {
	stocktakes map[int]inventree.PartStocktake
	pageErr    error
	stkQueries []inventree.PartStocktakeQuery
}

func newFakePartStocktakeClient() *fakePartStocktakeClient {
	return &fakePartStocktakeClient{stocktakes: map[int]inventree.PartStocktake{}}
}

func (f *fakePartStocktakeClient) SearchPartStocktakesPage(_ context.Context, query inventree.PartStocktakeQuery) (inventree.Page[inventree.PartStocktake], error) {
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

func (f *fakePartStocktakeClient) GetPartStocktake(_ context.Context, id int) (inventree.PartStocktake, error) {
	record, ok := f.stocktakes[id]
	if !ok {
		return inventree.PartStocktake{}, &inventree.APIError{StatusCode: 404, Kind: inventree.ErrorKindNotFound}
	}
	return record, nil
}

func partStocktakeDeps(fake *fakePartStocktakeClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestListPartStocktakesRequiresPartID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartStocktakeClient()

	_, out, err := listPartStocktakes(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartStocktakesInput{})
	r.NoError(err)
	a.Equal(StatusClarificationRequired, out.Status)
}

func TestListPartStocktakesSortsAndPaginates(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartStocktakeClient()
	fake.stocktakes[2] = inventree.PartStocktake{PK: 2, Part: 7, ItemCount: 3, Quantity: 30}
	fake.stocktakes[1] = inventree.PartStocktake{PK: 1, Part: 7, ItemCount: 2, Quantity: 20}
	fake.stocktakes[3] = inventree.PartStocktake{PK: 3, Part: 9, ItemCount: 1, Quantity: 5}

	_, out, err := listPartStocktakes(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartStocktakesInput{PartID: 7})
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
	fake := newFakePartStocktakeClient()
	fake.stocktakes[1] = inventree.PartStocktake{PK: 1, Part: 7, ItemCount: 2, Quantity: 20}

	_, out, err := getPartStocktake(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 1})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(7, out.Record.Part)

	_, missing, err := getPartStocktake(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 999})
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)
}

func TestGetPartStocktakeRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartStocktakeClient()

	for _, id := range []int{0, -1} {
		_, out, err := getPartStocktake(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: id})
		r.NoError(err)
		a.Equal(StatusNotFound, out.Status)
	}
}

func TestGetPartStocktakeRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartStocktakeClient()
	fake.stocktakes[5] = inventree.PartStocktake{PK: 5, Part: 7}
	fake.stocktakes[9] = fake.stocktakes[5] // simulate InvenTree returning a mismatched record for ID 9

	_, _, err := getPartStocktake(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, IDInput{ID: 9})
	r.Error(err)
	a.Contains(err.Error(), "mismatched")
}

func TestListPartStocktakesOffsetBeyondCountReturnsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := newFakePartStocktakeClient()
	fake.stocktakes[1] = inventree.PartStocktake{PK: 1, Part: 7}

	_, out, err := listPartStocktakes(partStocktakeDeps(fake))(ctx, &mcp.CallToolRequest{}, ListPartStocktakesInput{PartID: 7, Offset: 1000})
	r.NoError(err)
	a.Equal(StatusOK, out.Status)
	a.Equal(1, out.Count)
	a.Empty(out.Results)
}
