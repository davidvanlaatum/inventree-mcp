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
	stockTrackingMaxRecords  = 1000
	stockTrackingPageSize    = 100
	stockTrackingMaxRequests = stockTrackingMaxRecords/stockTrackingPageSize + 10

	partStocktakeMaxRecords  = 1000
	partStocktakePageSize    = 100
	partStocktakeMaxRequests = partStocktakeMaxRecords/partStocktakePageSize + 10
)

type StockTrackingLookupClient interface {
	SearchStockTrackingPage(context.Context, inventree.StockTrackingQuery) (inventree.Page[inventree.StockTracking], error)
	GetStockTrackingEntry(context.Context, int) (inventree.StockTracking, error)
}

type PartStocktakeLookupClient interface {
	SearchPartStocktakesPage(context.Context, inventree.PartStocktakeQuery) (inventree.Page[inventree.PartStocktake], error)
	GetPartStocktake(context.Context, int) (inventree.PartStocktake, error)
}

type ListStockTrackingEntriesInput struct {
	StockItemID int `json:"stock_item_id,omitempty" jsonschema:"Stable stock-item ID. Required unless part_id is given."`
	PartID      int `json:"part_id,omitempty" jsonschema:"Stable part ID. Required unless stock_item_id is given."`
	Limit       int `json:"limit,omitempty" jsonschema:"Maximum records to return; defaults to 50 and cannot exceed 1000."`
	Offset      int `json:"offset,omitempty" jsonschema:"Zero-based result offset."`
}

// StockTrackingEntryOutput is the concise per-event projection returned by
// list_stock_tracking_entries. Free-text notes are only exposed by the exact
// get_stock_tracking_entry projection. Deltas is the redacted, bounded
// representation produced by inventree.SanitizeStockTrackingDeltas; nested
// item, part, and user detail remain separate stable-ID lookups.
type StockTrackingEntryOutput struct {
	PK           int            `json:"pk"`
	Item         *int           `json:"item,omitempty"`
	Part         *int           `json:"part,omitempty"`
	User         *int           `json:"user,omitempty"`
	Date         string         `json:"date"`
	Label        string         `json:"label"`
	TrackingType int            `json:"tracking_type"`
	Deltas       map[string]any `json:"deltas"`
}

// StockTrackingEntryDetailOutput is the exact single-event projection; it
// adds Notes to the concise StockTrackingEntryOutput fields.
type StockTrackingEntryDetailOutput struct {
	StockTrackingEntryOutput
	Notes string `json:"notes"`
}

type StockTrackingEntryListOutput struct {
	Status        string                     `json:"status"`
	Count         int                        `json:"count"`
	Results       []StockTrackingEntryOutput `json:"results"`
	Clarification *ClarificationResponse     `json:"clarification,omitempty"`
}

type StockTrackingEntryDetailResult struct {
	Status string                          `json:"status"`
	Record *StockTrackingEntryDetailOutput `json:"record,omitempty"`
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

func registerStockTrackingLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, ListStockTrackingEntriesToolName, "List stock tracking entries", "Lists bounded stock tracking audit events for one stable stock item or part.", listStockTrackingEntries(deps))
	addReadOnlyTool(server, deps, GetStockTrackingEntryToolName, "Get stock tracking entry", "Retrieves one exact stock tracking audit event by stable event ID.", getStockTrackingEntry(deps))
	addReadOnlyTool(server, deps, ListPartStocktakesToolName, "List part stocktakes", "Lists bounded historical PartStocktake report snapshots for one stable part -- a separate InvenTree feature from the absolute-quantity stock-count-correction workflow. Snapshots only exist if generated through the InvenTree web UI or a report plugin; an empty result is common, not necessarily an error.", listPartStocktakes(deps))
	addReadOnlyTool(server, deps, GetPartStocktakeToolName, "Get part stocktake", "Retrieves one exact historical PartStocktake report snapshot by stable ID. Requires a staff/admin InvenTree credential; a non-staff credential returns InvenTree's ordinary 403.", getPartStocktake(deps))
}

func listStockTrackingEntries(deps Dependencies) mcp.ToolHandlerFor[ListStockTrackingEntriesInput, StockTrackingEntryListOutput] {
	return LookupHandler[StockTrackingLookupClient, ListStockTrackingEntriesInput, StockTrackingEntryListOutput](deps, ListStockTrackingEntriesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockTrackingLookupClient, input ListStockTrackingEntriesInput) (*mcp.CallToolResult, StockTrackingEntryListOutput, error) {
			if input.StockItemID <= 0 && input.PartID <= 0 {
				clarification := NewClarification("Which stock item or part's tracking history should be listed?", "stock_item_id", "stock_item_id or part_id is required", "stock_item_id", true, nil, map[string]any{})
				return TextResult(StatusClarificationRequired), StockTrackingEntryListOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			if input.StockItemID < 0 || input.PartID < 0 || input.Offset < 0 || input.Limit < 0 || input.Limit > stockTrackingMaxRecords {
				clarification := NewClarification("Which bounded stock tracking list should be returned?", "stock_item_id", "stock_item_id, part_id, and offset must be non-negative and limit at most 1000", "stock_item_id", true, nil, map[string]any{})
				return TextResult(StatusClarificationRequired), StockTrackingEntryListOutput{Status: StatusClarificationRequired, Clarification: &clarification}, nil
			}
			limit := input.Limit
			if limit == 0 {
				limit = 50
			}
			records, err := loadStockTrackingEntries(ctx, client, input.StockItemID, input.PartID)
			if err != nil {
				return nil, StockTrackingEntryListOutput{}, err
			}
			sort.Slice(records, func(i, j int) bool { return records[i].PK < records[j].PK })
			count := len(records)
			start := min(input.Offset, count)
			end := min(start+limit, count)
			results := make([]StockTrackingEntryOutput, 0, end-start)
			for _, record := range records[start:end] {
				results = append(results, stockTrackingEntryOutput(record))
			}
			return TextResult(StatusOK), StockTrackingEntryListOutput{Status: StatusOK, Count: count, Results: results}, nil
		})
}

func loadStockTrackingEntries(ctx context.Context, client StockTrackingLookupClient, itemID int, partID int) ([]inventree.StockTracking, error) {
	result := []inventree.StockTracking{}
	for offset, requests := 0, 0; ; offset, requests = offset+stockTrackingPageSize, requests+1 {
		if requests >= stockTrackingMaxRequests || len(result) >= stockTrackingMaxRecords {
			return nil, errors.New("stock tracking list exceeded the bounded request or record budget")
		}
		page, err := client.SearchStockTrackingPage(ctx, inventree.StockTrackingQuery{ItemID: itemID, PartID: partID, Limit: stockTrackingPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Results...)
		if len(result) > stockTrackingMaxRecords {
			return nil, errors.New("stock tracking list exceeded the bounded record budget")
		}
		if page.Next == nil || *page.Next == "" {
			return result, nil
		}
	}
}

func stockTrackingEntryOutput(entry inventree.StockTracking) StockTrackingEntryOutput {
	return StockTrackingEntryOutput{
		PK:           entry.PK,
		Item:         entry.Item,
		Part:         entry.Part,
		User:         entry.User,
		Date:         entry.Date,
		Label:        entry.Label,
		TrackingType: entry.TrackingType,
		Deltas:       entry.Deltas,
	}
}

func getStockTrackingEntry(deps Dependencies) mcp.ToolHandlerFor[IDInput, StockTrackingEntryDetailResult] {
	return LookupHandler[StockTrackingLookupClient, IDInput, StockTrackingEntryDetailResult](deps, GetStockTrackingEntryToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockTrackingLookupClient, input IDInput) (*mcp.CallToolResult, StockTrackingEntryDetailResult, error) {
			if input.ID <= 0 {
				return stockTrackingEntryDetailResult(inventree.StockTracking{}, &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound})
			}
			entry, err := client.GetStockTrackingEntry(ctx, input.ID)
			if err == nil && entry.PK != input.ID {
				return nil, StockTrackingEntryDetailResult{}, errors.New("InvenTree returned a mismatched stock-tracking-entry identity")
			}
			return stockTrackingEntryDetailResult(entry, err)
		})
}

func stockTrackingEntryDetailResult(entry inventree.StockTracking, err error) (*mcp.CallToolResult, StockTrackingEntryDetailResult, error) {
	if err != nil {
		if isNotFound(err) {
			return TextResult(StatusNotFound), StockTrackingEntryDetailResult{Status: StatusNotFound}, nil
		}
		return nil, StockTrackingEntryDetailResult{}, err
	}
	detail := StockTrackingEntryDetailOutput{StockTrackingEntryOutput: stockTrackingEntryOutput(entry), Notes: entry.Notes}
	return TextResult(StatusOK), StockTrackingEntryDetailResult{Status: StatusOK, Record: &detail}, nil
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
