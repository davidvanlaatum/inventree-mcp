package tools

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	auditMaxRecords  = 1000
	auditPageSize    = 100
	auditMaxRequests = 20
)

type auditModelType string

type AuditLookupClient interface {
	SearchAuditEntriesPage(context.Context, inventree.AuditQuery) (inventree.Page[inventree.AuditEntry], error)
	GetAuditEntry(context.Context, inventree.AuditEntryQuery) (inventree.AuditEntry, error)
}

type AuditScopeInput struct {
	ModelType       auditModelType `json:"model_type" jsonschema:"Qualified audited model type for one of the six supported ordinary object types."`
	ModelID         int            `json:"model_id" jsonschema:"Required positive object ID; unscoped audit reads are unavailable. Deleted objects may retain readable audit history."`
	IncludeChildren bool           `json:"include_children,omitempty" jsonschema:"Opt in to directly linked child audit entries, restricted to the same six supported model types; default false."`
}

func (i AuditScopeInput) scope() inventree.AuditScope {
	return inventree.AuditScope{ModelType: string(i.ModelType), ModelID: i.ModelID, IncludeChildren: i.IncludeChildren}
}
func (i AuditScopeInput) valid() bool {
	return inventree.ValidAuditModelType(string(i.ModelType)) && i.ModelID > 0
}

type ListAuditEntriesInput struct {
	AuditScopeInput
	Limit  int `json:"limit,omitempty" jsonschema:"Maximum returned entries, default 50 and at most 1000; the complete scoped scan must fit the request and record budgets."`
	Offset int `json:"offset,omitempty" jsonschema:"Zero-based offset after sorting by audit ID; audit history is not a transactional snapshot."`
}
type GetAuditEntryInput struct {
	AuditScopeInput
	ID int `json:"id" jsonschema:"Audit entry ID obtained from list_audit_entries; legacy stock-tracking IDs are not interchangeable."`
}
type AuditEntryListOutput struct {
	Status        string                        `json:"status"`
	Count         int                           `json:"count"`
	Results       []inventree.AuditEntrySummary `json:"results"`
	Clarification *ClarificationResponse        `json:"clarification,omitempty"`
}
type AuditEntryDetailOutput struct {
	Status        string                 `json:"status"`
	Record        *inventree.AuditEntry  `json:"record,omitempty"`
	Clarification *ClarificationResponse `json:"clarification,omitempty"`
}

func addAuditReadTool[In, Out any](server *mcp.Server, deps Dependencies, name, title, description string, handler mcp.ToolHandlerFor[In, Out]) {
	schema, err := jsonschema.For[In](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[auditModelType](): stringEnumSchema([]string{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"})}})
	if err != nil {
		panic(fmt.Sprintf("build %s input schema: %v", name, err))
	}
	tool := ToolDescriptor(name, title, description)
	tool.InputSchema = schema
	mcp.AddTool(server, tool, GuardTool(deps, name, handler))
}
func registerAuditLookupTools(server *mcp.Server, deps Dependencies) {
	addAuditReadTool(server, deps, ListAuditEntriesToolName, "List audit entries", "Lists bounded, redacted audit history for one supported object; optional child history stays within the six-type allowlist. Omits notes, actor expansions, network details and display expansions. Replaces legacy stock-tracking tools; use the returned audit IDs.", listAuditEntries(deps))
	addAuditReadTool(server, deps, GetAuditEntryToolName, "Get audit entry", "Reads an exact audit entry after matching its supported object scope; includes free-text notes with bounded/redacted deltas and approved field changes. Audit IDs differ from legacy stock-tracking IDs.", getAuditEntry(deps))
}

func auditScopeClarification() ClarificationResponse {
	return NewClarification("Which supported object's audit history should be read?", "model_type", "a supported model_type and positive model_id are required", "model_type", true, nil, map[string]any{})
}
func listAuditEntries(deps Dependencies) mcp.ToolHandlerFor[ListAuditEntriesInput, AuditEntryListOutput] {
	return LookupHandler[AuditLookupClient, ListAuditEntriesInput, AuditEntryListOutput](deps, ListAuditEntriesToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client AuditLookupClient, input ListAuditEntriesInput) (*mcp.CallToolResult, AuditEntryListOutput, error) {
		if !input.valid() {
			c := auditScopeClarification()
			return TextResult(StatusClarificationRequired), AuditEntryListOutput{Status: StatusClarificationRequired, Clarification: &c}, nil
		}
		if input.Offset < 0 || input.Limit < 0 || input.Limit > auditMaxRecords {
			field := "limit"
			rule := "limit must be 0 (default) through 1000"
			if input.Offset < 0 {
				field = "offset"
				rule = "offset must be non-negative"
			}
			c := NewClarification("Which valid audit page should be returned?", field, rule, field, true, nil, map[string]any{})
			return TextResult(StatusClarificationRequired), AuditEntryListOutput{Status: StatusClarificationRequired, Clarification: &c}, nil
		}
		records := []inventree.AuditEntrySummary{}
		for offset, requests := 0, 0; ; offset, requests = offset+auditPageSize, requests+1 {
			if requests >= auditMaxRequests {
				return nil, AuditEntryListOutput{}, errors.New("audit history exceeded the bounded request budget")
			}
			page, err := client.SearchAuditEntriesPage(ctx, inventree.AuditQuery{AuditScope: input.scope(), Limit: auditPageSize, Offset: offset})
			if err != nil {
				return nil, AuditEntryListOutput{}, err
			}
			for _, entry := range page.Results {
				if !inventree.ValidAuditModelType(entry.ModelType) || !input.scope().Contains(entry.AuditEntrySummary) {
					return nil, AuditEntryListOutput{}, errors.New("audit history returned an out-of-scope entry")
				}
				records = append(records, entry.AuditEntrySummary)
			}
			if len(records) > auditMaxRecords {
				return nil, AuditEntryListOutput{}, errors.New("audit history exceeded the bounded record budget")
			}
			if page.Next == nil || *page.Next == "" {
				break
			}
		}
		sort.Slice(records, func(i, j int) bool { return records[i].PK < records[j].PK })
		limit := input.Limit
		if limit == 0 {
			limit = 50
		}
		start := min(input.Offset, len(records))
		end := min(start+limit, len(records))
		return TextResult(StatusOK), AuditEntryListOutput{Status: StatusOK, Count: len(records), Results: records[start:end]}, nil
	})
}
func getAuditEntry(deps Dependencies) mcp.ToolHandlerFor[GetAuditEntryInput, AuditEntryDetailOutput] {
	return LookupHandler[AuditLookupClient, GetAuditEntryInput, AuditEntryDetailOutput](deps, GetAuditEntryToolName, func(ctx context.Context, _ *mcp.CallToolRequest, client AuditLookupClient, input GetAuditEntryInput) (*mcp.CallToolResult, AuditEntryDetailOutput, error) {
		if !input.valid() {
			c := auditScopeClarification()
			return TextResult(StatusClarificationRequired), AuditEntryDetailOutput{Status: StatusClarificationRequired, Clarification: &c}, nil
		}
		if input.ID <= 0 {
			return TextResult(StatusNotFound), AuditEntryDetailOutput{Status: StatusNotFound}, nil
		}
		entry, err := client.GetAuditEntry(ctx, inventree.AuditEntryQuery{AuditScope: input.scope(), ID: input.ID})
		if err != nil {
			if isNotFound(err) {
				return TextResult(StatusNotFound), AuditEntryDetailOutput{Status: StatusNotFound}, nil
			}
			return nil, AuditEntryDetailOutput{}, err
		}
		if entry.PK != input.ID || !inventree.ValidAuditModelType(entry.ModelType) || !input.scope().Contains(entry.AuditEntrySummary) {
			return nil, AuditEntryDetailOutput{}, errors.New("InvenTree returned a mismatched audit identity or scope")
		}
		return TextResult(StatusOK), AuditEntryDetailOutput{Status: StatusOK, Record: &entry}, nil
	})
}
