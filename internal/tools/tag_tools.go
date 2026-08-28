package tools

import (
	"context"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TagSearchClient backs search_tags. It is deliberately a single bounded
// page over /api/tag/, not a full scan: search_tags is a discovery lookup
// over InvenTree's shared cross-object tag taxonomy, not a source of
// exhaustive tag inventory.
type TagSearchClient interface {
	SearchTagsPage(context.Context, inventree.TagQuery) (inventree.TagPage, error)
}

type SearchTagsInput struct {
	ModelType string `json:"model_type,omitempty" jsonschema:"Optional qualified InvenTree app.model value (for example part.part, company.company, or stock.stocklocation) to scope results to tags currently referenced by that object type."`
	Search    string `json:"search,omitempty" jsonschema:"Optional tag-name search text."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset    int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type TagResult struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func searchTags(deps Dependencies) mcp.ToolHandlerFor[SearchTagsInput, LookupOutput[TagResult]] {
	return LookupHandler[TagSearchClient, SearchTagsInput, LookupOutput[TagResult]](deps, SearchTagsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client TagSearchClient, input SearchTagsInput) (*mcp.CallToolResult, LookupOutput[TagResult], error) {
			if input.Offset < 0 {
				return searchTagsClarification("Which result page should be searched?", "offset", "offset cannot be negative", input)
			}
			page, err := client.SearchTagsPage(ctx, inventree.TagQuery{
				ModelType: input.ModelType,
				Search:    input.Search,
				Limit:     NormalizeLookupLimit(input.Limit),
				Offset:    input.Offset,
			})
			if err != nil {
				return nil, LookupOutput[TagResult]{}, err
			}
			results := make([]TagResult, 0, len(page.Results))
			for _, tag := range page.Results {
				results = append(results, TagResult{ID: tag.PK, Name: tag.Name, Slug: tag.Slug})
			}
			return listOutput(results, nil)
		})
}

func searchTagsClarification(question, field, reason string, input SearchTagsInput) (*mcp.CallToolResult, LookupOutput[TagResult], error) {
	clarification := NewClarification(question, field, reason, field, true, nil, map[string]any{"model_type": input.ModelType, "search": input.Search, "limit": input.Limit, "offset": input.Offset})
	return TextResult(StatusClarificationRequired), LookupOutput[TagResult]{Status: StatusClarificationRequired, Clarification: &clarification}, nil
}
