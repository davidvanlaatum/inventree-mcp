package tools

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	partStocktakeMaxRecords  = 1000
	partStocktakePageSize    = 100
	partStocktakeMaxRequests = partStocktakeMaxRecords/partStocktakePageSize + 10
)

type PartStocktakeLookupClient interface {
	SearchPartStocktakesPage(context.Context, inventree.PartStocktakeQuery) (inventree.Page[inventree.PartStocktake], error)
	GetPartStocktake(context.Context, int) (inventree.PartStocktake, error)
}

type ListPartStocktakesInput struct {
	PartID int `json:"part_id" jsonschema:"Stable part ID."`
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum records to return; defaults to 50 and cannot exceed 1000."`
	Offset int `json:"offset,omitempty" jsonschema:"Zero-based result offset."`
}

type PartStocktakeListOutput struct {
	Status        string                    `json:"status"`
	Count         int                       `json:"count"`
	Results       []inventree.PartStocktake `json:"results"`
	Clarification *ClarificationResponse    `json:"clarification,omitempty"`
}

func registerPartStocktakeLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, ListPartStocktakesToolName, "List part stocktakes", "Lists bounded historical part stocktake snapshots; separate from stock count correction.", listPartStocktakes(deps))
	addReadOnlyTool(server, deps, GetPartStocktakeToolName, "Get part stocktake", "Retrieves one historical part stocktake snapshot by ID. Requires staff/admin InvenTree permission.", getPartStocktake(deps))
}
func listPartStocktakes(deps Dependencies) mcp.ToolHandlerFor[ListPartStocktakesInput, PartStocktakeListOutput] {
	return LookupHandler[PartStocktakeLookupClient, ListPartStocktakesInput, PartStocktakeListOutput](deps, ListPartStocktakesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartStocktakeLookupClient, input ListPartStocktakesInput) (*mcp.CallToolResult, PartStocktakeListOutput, error) {
			if input.PartID <= 0 || input.Offset < 0 || input.Limit < 0 || input.Limit > partStocktakeMaxRecords {
				clarification := NewClarification("Which bounded part stocktake history should be returned?", "part_id", "part_id must be positive, offset non-negative, and limit at most 1000", "part_id", true, nil, map[string]any{})
				return TextResult(StatusClarificationRequired), PartStocktakeListOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			limit := input.Limit
			if limit == 0 {
				limit = 50
			}
			records, err := loadPartStocktakes(ctx, client, input.PartID)
			if err != nil {
				return nil, PartStocktakeListOutput{}, err
			}
			sort.Slice(records, func(i, j int) bool { return records[i].PK < records[j].PK })
			count := len(records)
			start := min(input.Offset, count)
			end := min(start+limit, count)
			return TextResult(StatusOK), PartStocktakeListOutput{Status: StatusOK, Count: count, Results: records[start:end]}, nil
		})
}

func loadPartStocktakes(ctx context.Context, client PartStocktakeLookupClient, partID int) ([]inventree.PartStocktake, error) {
	result := []inventree.PartStocktake{}
	for offset, requests := 0, 0; ; offset, requests = offset+partStocktakePageSize, requests+1 {
		if requests >= partStocktakeMaxRequests || len(result) >= partStocktakeMaxRecords {
			return nil, errors.New("part stocktake list exceeded the bounded request or record budget")
		}
		page, err := client.SearchPartStocktakesPage(ctx, inventree.PartStocktakeQuery{PartID: partID, Limit: partStocktakePageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Results...)
		if len(result) > partStocktakeMaxRecords {
			return nil, errors.New("part stocktake list exceeded the bounded record budget")
		}
		if page.Next == nil || *page.Next == "" {
			return result, nil
		}
	}
}

func getPartStocktake(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.PartStocktake]] {
	return LookupHandler[PartStocktakeLookupClient, IDInput, RecordOutput[inventree.PartStocktake]](deps, GetPartStocktakeToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartStocktakeLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.PartStocktake], error) {
			if input.ID <= 0 {
				return recordOutput(inventree.PartStocktake{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound})
			}
			record, err := client.GetPartStocktake(ctx, input.ID)
			if err == nil && record.PK != input.ID {
				return nil, RecordOutput[inventree.PartStocktake]{}, errors.New("InvenTree returned a mismatched part-stocktake identity")
			}
			return recordOutput(record, err)
		})
}
