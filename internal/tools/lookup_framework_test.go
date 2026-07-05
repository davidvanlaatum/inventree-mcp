package tools

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type partSearchClient interface {
	SearchParts(context.Context, url.Values) ([]inventree.Part, error)
}

func TestDependenciesReturnLookupClientFromContext(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeLookupClient{}
	deps := Dependencies{
		ClientFromContext: func(got context.Context) (any, error) {
			r.Same(ctx, got)
			return fake, nil
		},
	}

	client, err := deps.Client(ctx)
	r.NoError(err)
	r.Same(fake, client)
}

func TestDependenciesRejectMissingLookupClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)

	_, err := (Dependencies{}).Client(ctx)
	r.ErrorIs(err, ErrLookupClientUnavailable)

	_, err = (Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return nil, nil
		},
	}).Client(ctx)
	r.ErrorIs(err, ErrLookupClientUnavailable)
}

func TestLookupHandlerUsesInterfaceClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeLookupClient{
		parts: []inventree.Part{{PK: 42, Name: "resistor"}},
	}
	deps := Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return fake, nil
		},
	}

	handler := LookupHandler[partSearchClient, SearchInput, searchPartsOutput](deps, "sample_search_parts",
		func(ctx context.Context, _ *mcp.CallToolRequest, client partSearchClient, input SearchInput) (*mcp.CallToolResult, searchPartsOutput, error) {
			parts, err := client.SearchParts(ctx, SearchValues(input))
			return TextResult("ok"), searchPartsOutput{Status: StatusOK, Count: len(parts)}, err
		})

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "resistor", Limit: 250})
	r.NoError(err)
	r.NotNil(result)
	a.Equal("ok", result.Content[0].(*mcp.TextContent).Text)
	a.Equal(searchPartsOutput{Status: StatusOK, Count: 1}, output)
	a.Equal(url.Values{"limit": []string{"100"}, "search": []string{"resistor"}}, fake.lastSearchPartsQuery)
}

func TestLookupHandlerReturnsClientResolutionError(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	wantErr := errors.New("credential missing")
	handler := LookupHandler[partSearchClient, SearchInput, searchPartsOutput](Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return nil, wantErr
		},
	}, "sample_search_parts", func(context.Context, *mcp.CallToolRequest, partSearchClient, SearchInput) (*mcp.CallToolResult, searchPartsOutput, error) {
		return nil, searchPartsOutput{}, nil
	})

	_, _, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{})
	r.ErrorIs(err, wantErr)
}

func TestLookupHandlerReturnsClarificationFromAmbiguousFakeClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	fake := &fakeLookupClient{
		parts: []inventree.Part{
			{PK: 10, Name: "10k resistor"},
			{PK: 11, Name: "10k resistor precision"},
		},
	}
	handler := LookupHandler[partSearchClient, SearchInput, partLookupOutput](Dependencies{
		ClientFromContext: func(context.Context) (any, error) {
			return fake, nil
		},
	}, "sample_search_parts", func(ctx context.Context, _ *mcp.CallToolRequest, client partSearchClient, input SearchInput) (*mcp.CallToolResult, partLookupOutput, error) {
		parts, err := client.SearchParts(ctx, SearchValues(input))
		if err != nil {
			return nil, partLookupOutput{}, err
		}
		if len(parts) > 1 {
			candidates := make([]ClarificationCandidate, 0, len(parts))
			for _, part := range parts {
				candidates = append(candidates, ClarificationCandidate{
					ID:    strconv.Itoa(part.PK),
					Label: part.Name,
				})
			}
			return TextResult("clarification required"), partLookupOutput{
				Clarification: NewClarification(
					"Which part should be used?",
					"part",
					"search matched multiple parts",
					"part_id",
					false,
					candidates,
					map[string]any{"search": input.Search},
				),
			}, nil
		}
		return TextResult("ok"), partLookupOutput{Status: StatusOK}, nil
	})

	result, output, err := handler(ctx, &mcp.CallToolRequest{}, SearchInput{Search: "10k"})
	r.NoError(err)
	r.NotNil(result)
	a.Equal("clarification required", result.Content[0].(*mcp.TextContent).Text)
	a.Equal(StatusClarificationRequired, output.Clarification.Status)
	a.Equal("part", output.Clarification.Field)
	a.Equal("search matched multiple parts", output.Clarification.Reason)
	a.Equal("part_id", output.Clarification.Retry)
	a.False(output.Clarification.HardError)
	r.Len(output.Clarification.Candidates, 2)
	a.Equal("10", output.Clarification.Candidates[0].ID)
	a.Equal("10k", output.Clarification.RetryValues["search"])
}

func TestLookupQueryHelpers(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.Equal(url.Values{"limit": []string{"20"}}, SearchValues(SearchInput{}))
	a.Equal(url.Values{"limit": []string{"7"}, "offset": []string{"3"}, "search": []string{"cap"}}, SearchValues(SearchInput{
		Search: "cap",
		Limit:  7,
		Offset: 3,
	}))
	a.Equal(url.Values{"limit": []string{"100"}}, SearchValues(SearchInput{Limit: 101}))
	a.Equal(url.Values{
		"limit":      []string{"20"},
		"model_id":   []string{"42"},
		"model_type": []string{"part"},
		"search":     []string{"datasheet"},
	}, ObjectLookupValues(ObjectLookupInput{ModelType: "part", ModelID: 42, Search: "datasheet"}))
}

func TestClarificationResponseUsesStableRetryFields(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	clarification := NewClarification("Which part?", "part", "multiple matches", "part_id", false, []ClarificationCandidate{
		{ID: "10", Label: "10k resistor", Fields: map[string]any{"ipn": "R-10K"}},
	}, map[string]any{"search": "10k"})

	a.Equal(StatusClarificationRequired, clarification.Status)
	a.Equal("Which part?", clarification.Question)
	a.Equal("part", clarification.Field)
	a.Equal("multiple matches", clarification.Reason)
	a.Equal("part_id", clarification.Retry)
	a.False(clarification.HardError)
	a.Equal("10", clarification.Candidates[0].ID)
	a.Equal("10k", clarification.RetryValues["search"])
}

type searchPartsOutput struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type partLookupOutput struct {
	Status        string                `json:"status,omitempty"`
	Clarification ClarificationResponse `json:"clarification,omitempty"`
}

type fakeLookupClient struct {
	parts                []inventree.Part
	lastSearchPartsQuery url.Values
}

func (f *fakeLookupClient) SearchParts(_ context.Context, query url.Values) ([]inventree.Part, error) {
	f.lastSearchPartsQuery = query
	return f.parts, nil
}
