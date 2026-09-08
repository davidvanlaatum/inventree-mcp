package tools

import (
	"context"
	"errors"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PartTestTemplateLookupClient is the narrow client surface
// search_part_test_templates/get_part_test_template need. GetPart is used
// to preflight the part's Testable flag: InvenTree's `part` list filter on
// /api/part/test-template/ is validated against a testable:true-only
// queryset (confirmed live against pinned InvenTree 1.5.2, the same shape
// F-S58 found for PartSalePriceBreak's salable-gated `part` filter), so a
// non-testable part id is rejected with an opaque
// `Invalid pk "<id>" - object does not exist.` error. Preflighting lets
// this tool return an actionable message instead.
type PartTestTemplateLookupClient interface {
	SearchPartTestTemplatesPage(context.Context, inventree.PartTestTemplateQuery) (inventree.Page[inventree.PartTestTemplate], error)
	GetPartTestTemplate(context.Context, int) (inventree.PartTestTemplate, error)
	GetPartDetail(context.Context, int) (inventree.PartDetail, error)
}

type SearchPartTestTemplatesInput struct {
	PartID             int    `json:"part_id" jsonschema:"Stable part primary key. The part must already have testable:true; use update_part to set it first."`
	Enabled            *bool  `json:"enabled,omitempty" jsonschema:"Optional filter for whether the template is enabled."`
	Required           *bool  `json:"required,omitempty" jsonschema:"Optional filter for whether passing this test is required."`
	RequiresValue      *bool  `json:"requires_value,omitempty" jsonschema:"Optional filter for whether recording a result requires a value."`
	RequiresAttachment *bool  `json:"requires_attachment,omitempty" jsonschema:"Optional filter for whether recording a result requires an attachment."`
	HasResults         *bool  `json:"has_results,omitempty" jsonschema:"Optional filter for whether the template already has recorded results."`
	Search             string `json:"search,omitempty" jsonschema:"Optional case-insensitive substring search across the template's test name and description."`
	Limit              int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset             int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type SearchPartTestTemplatesOutput struct {
	Status     string                       `json:"status"`
	Count      int                          `json:"count,omitempty"`
	HasMore    bool                         `json:"has_more,omitempty"`
	Results    []inventree.PartTestTemplate `json:"results,omitempty"`
	Validation *ValidationFailure           `json:"validation,omitempty"`
}

func registerPartTestingLookupTools(server *mcp.Server, deps Dependencies) {
	addReadOnlyTool(server, deps, SearchPartTestTemplatesToolName, "Search part test templates", "Searches the named tests defined against one testable part.", searchPartTestTemplates(deps))
	addReadOnlyTool(server, deps, GetPartTestTemplateToolName, "Get part test template", "Retrieves one part test template by stable ID.", getPartTestTemplate(deps))
	addReadOnlyTool(server, deps, SearchStockItemTestResultsToolName, "Search stock item test results", "Searches recorded test results for one stock item.", searchStockItemTestResults(deps))
	addReadOnlyTool(server, deps, GetStockItemTestResultToolName, "Get stock item test result", "Retrieves one stock item test result by stable ID.", getStockItemTestResult(deps))
	addReadOnlyTool(server, deps, DownloadStockItemTestResultAttachmentToolName, "Download stock item test result attachment", "Downloads bounded content for one stock item test result's attachment, if it has one.", downloadStockItemTestResultAttachment(deps))
}

func searchPartTestTemplates(deps Dependencies) mcp.ToolHandlerFor[SearchPartTestTemplatesInput, SearchPartTestTemplatesOutput] {
	return LookupHandler[PartTestTemplateLookupClient, SearchPartTestTemplatesInput, SearchPartTestTemplatesOutput](deps, SearchPartTestTemplatesToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartTestTemplateLookupClient, input SearchPartTestTemplatesInput) (*mcp.CallToolResult, SearchPartTestTemplatesOutput, error) {
			if input.PartID <= 0 {
				return partTestTemplateValidation("part_id must be a positive part primary key")
			}
			if input.Offset < 0 {
				return partTestTemplateValidation("offset must not be negative")
			}
			part, err := client.GetPartDetail(ctx, input.PartID)
			if isNotFound(err) {
				return TextResult(StatusNotFound), SearchPartTestTemplatesOutput{Status: StatusNotFound}, nil
			}
			if err != nil {
				return nil, SearchPartTestTemplatesOutput{}, err
			}
			if !part.Testable {
				return partTestTemplateValidation("the part must have testable:true before its test templates can be searched; use update_part to set it first")
			}
			limit := NormalizeLookupLimit(input.Limit)
			page, err := client.SearchPartTestTemplatesPage(ctx, inventree.PartTestTemplateQuery{
				Part:               input.PartID,
				Enabled:            input.Enabled,
				Required:           input.Required,
				RequiresValue:      input.RequiresValue,
				RequiresAttachment: input.RequiresAttachment,
				HasResults:         input.HasResults,
				Search:             input.Search,
				Limit:              limit,
				Offset:             input.Offset,
			})
			if err != nil {
				return nil, SearchPartTestTemplatesOutput{}, err
			}
			if len(page.Results) == 0 {
				return TextResult(StatusNotFound), SearchPartTestTemplatesOutput{Status: StatusNotFound}, nil
			}
			return TextResult(StatusOK), SearchPartTestTemplatesOutput{Status: StatusOK, Count: page.Count, HasMore: page.Next != nil && *page.Next != "", Results: page.Results}, nil
		})
}

func partTestTemplateValidation(message string) (*mcp.CallToolResult, SearchPartTestTemplatesOutput, error) {
	return TextResult(StatusValidationFailed), SearchPartTestTemplatesOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "search_part_test_templates", Messages: []string{message}}}}}, nil
}

func getPartTestTemplate(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[inventree.PartTestTemplate]] {
	return LookupHandler[PartTestTemplateLookupClient, IDInput, RecordOutput[inventree.PartTestTemplate]](deps, GetPartTestTemplateToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client PartTestTemplateLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[inventree.PartTestTemplate], error) {
			record, err := client.GetPartTestTemplate(ctx, input.ID)
			return recordOutput(record, err)
		})
}

// StockItemTestResultLookupClient is the narrow client surface
// search_stock_item_test_results/get_stock_item_test_result/
// download_stock_item_test_result_attachment need.
type StockItemTestResultLookupClient interface {
	SearchStockItemTestResultsPage(context.Context, inventree.StockItemTestResultQuery) (inventree.Page[inventree.StockItemTestResult], error)
	GetStockItemTestResult(context.Context, int) (inventree.StockItemTestResult, error)
	DownloadStockItemTestResultAttachment(context.Context, int, int64) (inventree.DownloadedStockItemTestResultAttachment, error)
}

// StockItemTestResultView is the allowlisted projection of
// inventree.StockItemTestResult. User and Template stay bare IDs -- see
// get_user and get_part_test_template for identity resolution -- because
// this repo's F-S104 privacy boundary never returns email through any tool
// surface, and the upstream user_detail/template_detail expansions this
// package deliberately never requests would carry it. Attachment is
// projected the same way the existing generic attachment tools project
// Attachment.Attachment: a has_attachment flag plus a credential- and
// query-stripped URL that still requires an authenticated InvenTree
// request to fetch (confirmed live: unauthenticated fetch returns 401),
// resolved through download_stock_item_test_result_attachment.
type StockItemTestResultView struct {
	PK               int     `json:"pk"`
	StockItem        int     `json:"stock_item"`
	Template         *int    `json:"template,omitempty"`
	Result           bool    `json:"result"`
	Value            string  `json:"value,omitempty"`
	Notes            string  `json:"notes,omitempty"`
	TestStation      string  `json:"test_station,omitempty"`
	StartedDatetime  *string `json:"started_datetime,omitempty"`
	FinishedDatetime *string `json:"finished_datetime,omitempty"`
	User             *int    `json:"user,omitempty"`
	Date             string  `json:"date"`
	HasAttachment    bool    `json:"has_attachment"`
	AttachmentURL    string  `json:"attachment_url,omitempty"`
}

func stockItemTestResultView(record inventree.StockItemTestResult) StockItemTestResultView {
	return StockItemTestResultView{
		PK:               record.PK,
		StockItem:        record.StockItem,
		Template:         record.Template,
		Result:           record.Result,
		Value:            record.Value,
		Notes:            record.Notes,
		TestStation:      record.TestStation,
		StartedDatetime:  record.StartedDatetime,
		FinishedDatetime: record.FinishedDatetime,
		User:             record.User,
		Date:             record.Date,
		HasAttachment:    record.Attachment != nil && *record.Attachment != "",
		AttachmentURL:    redactedMetadataURL(record.Attachment),
	}
}

type SearchStockItemTestResultsInput struct {
	StockItemID      int   `json:"stock_item_id" jsonschema:"Stable stock item primary key."`
	TemplateID       int   `json:"template_id,omitempty" jsonschema:"Optional filter for one specific test template."`
	Result           *bool `json:"result,omitempty" jsonschema:"Optional filter for a passing (true) or failing (false) result."`
	IncludeInstalled bool  `json:"include_installed,omitempty" jsonschema:"When true, also include test results recorded against stock items installed underneath this stock item."`
	Limit            int   `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset           int   `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type SearchStockItemTestResultsOutput struct {
	Status     string                    `json:"status"`
	Count      int                       `json:"count,omitempty"`
	HasMore    bool                      `json:"has_more,omitempty"`
	Results    []StockItemTestResultView `json:"results,omitempty"`
	Validation *ValidationFailure        `json:"validation,omitempty"`
}

func searchStockItemTestResults(deps Dependencies) mcp.ToolHandlerFor[SearchStockItemTestResultsInput, SearchStockItemTestResultsOutput] {
	return LookupHandler[StockItemTestResultLookupClient, SearchStockItemTestResultsInput, SearchStockItemTestResultsOutput](deps, SearchStockItemTestResultsToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockItemTestResultLookupClient, input SearchStockItemTestResultsInput) (*mcp.CallToolResult, SearchStockItemTestResultsOutput, error) {
			if input.StockItemID <= 0 {
				return stockItemTestResultValidation("stock_item_id must be a positive stock item primary key")
			}
			if input.Offset < 0 {
				return stockItemTestResultValidation("offset must not be negative")
			}
			if input.TemplateID < 0 {
				return stockItemTestResultValidation("template_id must not be negative")
			}
			var templateID *int
			if input.TemplateID > 0 {
				templateID = &input.TemplateID
			}
			limit := NormalizeLookupLimit(input.Limit)
			page, err := client.SearchStockItemTestResultsPage(ctx, inventree.StockItemTestResultQuery{
				StockItem:        input.StockItemID,
				Template:         templateID,
				Result:           input.Result,
				IncludeInstalled: input.IncludeInstalled,
				Limit:            limit,
				Offset:           input.Offset,
			})
			if err != nil {
				return nil, SearchStockItemTestResultsOutput{}, err
			}
			if len(page.Results) == 0 {
				return TextResult(StatusNotFound), SearchStockItemTestResultsOutput{Status: StatusNotFound}, nil
			}
			results := make([]StockItemTestResultView, 0, len(page.Results))
			for _, record := range page.Results {
				results = append(results, stockItemTestResultView(record))
			}
			return TextResult(StatusOK), SearchStockItemTestResultsOutput{Status: StatusOK, Count: page.Count, HasMore: page.Next != nil && *page.Next != "", Results: results}, nil
		})
}

func stockItemTestResultValidation(message string) (*mcp.CallToolResult, SearchStockItemTestResultsOutput, error) {
	return TextResult(StatusValidationFailed), SearchStockItemTestResultsOutput{Status: StatusValidationFailed, Validation: &ValidationFailure{Fields: []ValidationFieldError{{Field: "search_stock_item_test_results", Messages: []string{message}}}}}, nil
}

func getStockItemTestResult(deps Dependencies) mcp.ToolHandlerFor[IDInput, RecordOutput[StockItemTestResultView]] {
	return LookupHandler[StockItemTestResultLookupClient, IDInput, RecordOutput[StockItemTestResultView]](deps, GetStockItemTestResultToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockItemTestResultLookupClient, input IDInput) (*mcp.CallToolResult, RecordOutput[StockItemTestResultView], error) {
			record, err := client.GetStockItemTestResult(ctx, input.ID)
			if err != nil {
				return recordOutput(StockItemTestResultView{}, err)
			}
			return recordOutput(stockItemTestResultView(record), nil)
		})
}

type StockItemTestResultAttachmentDownloadInput struct {
	ID       int   `json:"id" jsonschema:"Stable stock item test result primary key. This is the test result ID, not a stock item ID."`
	MaxBytes int64 `json:"max_bytes,omitempty" jsonschema:"Maximum content bytes to return. Defaults to 5 MiB and is capped at 25 MiB."`
}

func downloadStockItemTestResultAttachment(deps Dependencies) mcp.ToolHandlerFor[StockItemTestResultAttachmentDownloadInput, DownloadOutput] {
	return LookupHandler[StockItemTestResultLookupClient, StockItemTestResultAttachmentDownloadInput, DownloadOutput](deps, DownloadStockItemTestResultAttachmentToolName,
		func(ctx context.Context, _ *mcp.CallToolRequest, client StockItemTestResultLookupClient, input StockItemTestResultAttachmentDownloadInput) (*mcp.CallToolResult, DownloadOutput, error) {
			download, err := client.DownloadStockItemTestResultAttachment(ctx, input.ID, normalizeDownloadMaxBytes(input.MaxBytes))
			if err != nil {
				if isNotFound(err) {
					return TextResult(StatusNotFound), DownloadOutput{Status: StatusNotFound, ID: input.ID}, nil
				}
				if errors.Is(err, inventree.ErrStockItemTestResultAttachmentMissing) {
					return TextResult(StatusNoAttachment), DownloadOutput{Status: StatusNoAttachment, ID: input.ID}, nil
				}
				return nil, DownloadOutput{}, err
			}
			return downloadOutput(input.ID, download.Filename, "original", download.ContentType, download.SourceURL, download.Content)
		})
}
