package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	DefaultLookupLimit = 20
	MaxLookupLimit     = 100

	StatusOK                    = "ok"
	StatusClarificationRequired = "clarification_required"
	StatusNotFound              = "not_found"
	StatusNoImage               = "no_image"
	StatusNotTrackable          = "not_trackable"
)

type SearchInput struct {
	Search string `json:"search,omitempty" jsonschema:"Search text passed to the InvenTree endpoint."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type IDInput struct {
	ID int `json:"id" jsonschema:"Stable InvenTree primary key."`
}

type ObjectLookupInput struct {
	ModelType string `json:"model_type" jsonschema:"InvenTree attachment model type. Use one of the attachment endpoint's short, unqualified values: part, stockitem, company, manufacturerpart, supplierpart, or purchaseorder. Do not use parameter model types such as part.part or order.purchaseorder."`
	ModelID   int    `json:"model_id" jsonschema:"Stable InvenTree object primary key."`
	Search    string `json:"search,omitempty" jsonschema:"Optional search text or filename filter."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of records to return. Defaults to 20 and is capped at 100."`
	Offset    int    `json:"offset,omitempty" jsonschema:"Pagination offset for deterministic retries."`
}

type ClarificationResponse struct {
	Status      string                   `json:"status"`
	Question    string                   `json:"question"`
	Field       string                   `json:"field"`
	Reason      string                   `json:"reason"`
	Candidates  []ClarificationCandidate `json:"candidates"`
	Retry       string                   `json:"retry"`
	RetryTool   string                   `json:"retry_tool,omitempty"`
	HardError   bool                     `json:"hard_error"`
	RetryValues map[string]any           `json:"retry_values,omitempty"`
}

type ClarificationCandidate struct {
	ID           string         `json:"id"`
	Label        string         `json:"label"`
	Summary      string         `json:"summary,omitempty"`
	WebURL       string         `json:"web_url,omitempty"`
	APIURL       string         `json:"api_url,omitempty"`
	ParentWebURL string         `json:"parent_web_url,omitempty"`
	Fields       map[string]any `json:"fields,omitempty"`
}

type LookupHandlerFunc[Client, In, Out any] func(context.Context, *mcp.CallToolRequest, Client, In) (*mcp.CallToolResult, Out, error)

func LookupHandler[Client, In, Out any](deps Dependencies, toolName string, handler LookupHandlerFunc[Client, In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With(slog.String("tool", toolName)))
		logging.FromContext(ctx).DebugContext(ctx, "tool called")

		rawClient, err := deps.Client(ctx)
		if err != nil {
			var zero Out
			return nil, zero, safeToolError(err)
		}
		client, ok := rawClient.(Client)
		if !ok {
			var zero Out
			return nil, zero, fmt.Errorf("%w: client does not implement required interface for %s", ErrLookupClientUnavailable, toolName)
		}
		result, output, err := handler(ctx, req, client, input)
		if err != nil {
			return result, output, safeToolError(err)
		}
		projectWebLinks(deps.WebLinks, toolName, &output)
		return result, output, nil
	}
}

func NormalizeLookupLimit(limit int) int {
	if limit <= 0 {
		return DefaultLookupLimit
	}
	if limit > MaxLookupLimit {
		return MaxLookupLimit
	}
	return limit
}

func NewClarification(
	question string,
	field string,
	reason string,
	retry string,
	hardError bool,
	candidates []ClarificationCandidate,
	retryValues map[string]any,
) ClarificationResponse {
	return ClarificationResponse{
		Status:      StatusClarificationRequired,
		Question:    question,
		Field:       field,
		Reason:      reason,
		Candidates:  candidates,
		Retry:       retry,
		HardError:   hardError,
		RetryValues: retryValues,
	}
}

func TextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
