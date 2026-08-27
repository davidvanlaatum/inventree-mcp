package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGlobalSearchClient struct {
	result      inventree.GlobalSearchResult
	err         error
	lastQuery   inventree.GlobalSearchQuery
	queryCalled bool
}

func (f *fakeGlobalSearchClient) GlobalSearch(_ context.Context, query inventree.GlobalSearchQuery) (inventree.GlobalSearchResult, error) {
	f.lastQuery = query
	f.queryCalled = true
	return f.result, f.err
}

func depsForFakeGlobalSearch(fake *fakeGlobalSearchClient) Dependencies {
	return Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return fake, nil
		},
	}
}

func TestGlobalSearchDefaultsToEverySupportedObjectType(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeGlobalSearchClient{}
	handler := globalSearch(depsForFakeGlobalSearch(fake))

	_, output, err := handler(ctx, &mcp.CallToolRequest{}, GlobalSearchInput{Search: "resistor"})
	r.NoError(err)
	a.Equal(StatusNotFound, output.Status)
	r.True(fake.queryCalled)
	a.Equal(inventree.SupportedGlobalSearchObjectTypes, fake.lastQuery.ObjectTypes)
	a.Equal(DefaultLookupLimit, fake.lastQuery.Limit)
}

func TestGlobalSearchNormalizesLimitAndForwardsFlags(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeGlobalSearchClient{}
	handler := globalSearch(depsForFakeGlobalSearch(fake))

	_, _, err := handler(ctx, &mcp.CallToolRequest{}, GlobalSearchInput{
		Search:      "resistor",
		ObjectTypes: []string{"company"},
		SearchRegex: true,
		SearchWhole: true,
		SearchNotes: true,
		Limit:       500,
	})
	r.NoError(err)
	a.Equal([]inventree.GlobalSearchObjectType{inventree.GlobalSearchCompany}, fake.lastQuery.ObjectTypes)
	a.True(fake.lastQuery.SearchRegex)
	a.True(fake.lastQuery.SearchWhole)
	a.True(fake.lastQuery.SearchNotes)
	a.Equal(MaxLookupLimit, fake.lastQuery.Limit)
}

func TestGlobalSearchMapsBucketsToDetailToolsAndOmitsUnrequestedTypes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeGlobalSearchClient{
		result: inventree.GlobalSearchResult{
			Parts:     &inventree.GlobalSearchBucket[inventree.Part]{Count: 1, Results: []inventree.Part{{PK: 10, Name: "10k resistor"}}},
			Companies: &inventree.GlobalSearchBucket[inventree.Company]{Count: 0, Results: nil},
		},
	}
	handler := globalSearch(depsForFakeGlobalSearch(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, GlobalSearchInput{Search: "resistor"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal(StatusOK, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusOK, output.Status)

	r.NotNil(output.Parts)
	a.Equal(1, output.Parts.Count)
	a.Equal(GetPartToolName, output.Parts.DetailTool)
	a.Equal(10, output.Parts.Results[0].PK)

	r.NotNil(output.Companies)
	a.Equal(0, output.Companies.Count)
	a.Equal(GetCompanyToolName, output.Companies.DetailTool)

	a.Nil(output.PartCategories)
	a.Nil(output.StockItems)
	a.Nil(output.StockLocations)
	a.Nil(output.SupplierParts)
	a.Nil(output.ManufacturerParts)
	a.Nil(output.PurchaseOrders)
}

func TestGlobalSearchReturnsNotFoundWhenEveryRequestedBucketIsEmpty(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeGlobalSearchClient{
		result: inventree.GlobalSearchResult{
			Parts:     &inventree.GlobalSearchBucket[inventree.Part]{Count: 0},
			Companies: &inventree.GlobalSearchBucket[inventree.Company]{Count: 0},
		},
	}
	handler := globalSearch(depsForFakeGlobalSearch(fake))

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, GlobalSearchInput{Search: "nomatch"})
	r.NoError(err)
	a.Equal(StatusNotFound, result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusNotFound, output.Status)
}

func TestGlobalSearchPropagatesClientError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeGlobalSearchClient{err: errors.New("global search does not support object type \"salesorder\"")}
	handler := globalSearch(depsForFakeGlobalSearch(fake))

	_, _, err := handler(ctx, &mcp.CallToolRequest{}, GlobalSearchInput{Search: "x", ObjectTypes: []string{"salesorder"}})
	r.Error(err)
}

func TestGlobalSearchToolAuthorizationUsesReadOnlyScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	auth, ok := ToolAuthorizations[GlobalSearchToolName]
	r.True(ok)
	a.Equal("read_only", auth.MutationClass)
	a.Equal([]string{ScopeInventreeRead}, auth.Scopes)
	a.Equal(ReadOnlyAnnotations, auth.Annotations)
}
