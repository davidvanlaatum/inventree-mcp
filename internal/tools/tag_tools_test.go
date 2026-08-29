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

type fakeTagSearchClient struct {
	page      inventree.TagPage
	err       error
	lastQuery inventree.TagQuery
	called    bool
}

func (f *fakeTagSearchClient) SearchTagsPage(_ context.Context, query inventree.TagQuery) (inventree.TagPage, error) {
	f.lastQuery = query
	f.called = true
	return f.page, f.err
}

func depsForFakeTagSearch(fake *fakeTagSearchClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return fake, nil }}
}

func TestSearchTagsForwardsQueryAndNormalizesLimit(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeTagSearchClient{page: inventree.TagPage{Count: 1, Results: []inventree.Tag{{PK: 5, Name: "resistor", Slug: "resistor"}}}}
	_, out, err := searchTags(depsForFakeTagSearch(fake))(ctx, &mcp.CallToolRequest{}, SearchTagsInput{Search: "resistor", ModelType: "part.part"})
	r.NoError(err)
	r.True(fake.called)
	a.Equal("resistor", fake.lastQuery.Search)
	a.Equal("part.part", fake.lastQuery.ModelType)
	a.Equal(DefaultLookupLimit, fake.lastQuery.Limit)
	a.Equal(StatusOK, out.Status)
	r.Len(out.Results, 1)
	a.Equal(TagResult{ID: 5, Name: "resistor", Slug: "resistor"}, out.Results[0])

	fake.called = false
	_, _, err = searchTags(depsForFakeTagSearch(fake))(ctx, &mcp.CallToolRequest{}, SearchTagsInput{Search: "resistor", Limit: 500, Offset: 10})
	r.NoError(err)
	a.Equal(MaxLookupLimit, fake.lastQuery.Limit)
	a.Equal(10, fake.lastQuery.Offset)
}

func TestSearchTagsReturnsNotFoundForEmptyResults(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeTagSearchClient{page: inventree.TagPage{}}
	_, out, err := searchTags(depsForFakeTagSearch(fake))(ctx, &mcp.CallToolRequest{}, SearchTagsInput{Search: "nonexistent"})
	r.NoError(err)
	a.Equal(StatusNotFound, out.Status)
	a.Empty(out.Results)
}

func TestSearchTagsRejectsNegativeOffsetWithoutCallingClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeTagSearchClient{}
	_, out, err := searchTags(depsForFakeTagSearch(fake))(ctx, &mcp.CallToolRequest{}, SearchTagsInput{Offset: -1})
	r.NoError(err)
	a.False(fake.called, "a negative offset must be rejected before the client is called")
	a.Equal(StatusClarificationRequired, out.Status)
	require.NotNil(t, out.Clarification)
	a.Equal("offset", out.Clarification.Field)
}

func TestSearchTagsPropagatesClientError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	fake := &fakeTagSearchClient{err: errors.New("upstream unavailable")}
	_, _, err := searchTags(depsForFakeTagSearch(fake))(ctx, &mcp.CallToolRequest{}, SearchTagsInput{Search: "resistor"})
	r.Error(err)
}
