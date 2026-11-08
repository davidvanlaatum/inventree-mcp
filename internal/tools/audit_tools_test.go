package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/davidvanlaatum/dvgoutils"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAuditClient struct {
	pages    map[int]inventree.Page[inventree.AuditEntry]
	fallback inventree.Page[inventree.AuditEntry]
	entry    inventree.AuditEntry
	err      error
	queries  []inventree.AuditQuery
	gets     []inventree.AuditEntryQuery
}

func (f *fakeAuditClient) SearchAuditEntriesPage(_ context.Context, q inventree.AuditQuery) (inventree.Page[inventree.AuditEntry], error) {
	f.queries = append(f.queries, q)
	if p, ok := f.pages[q.Offset]; ok {
		return p, f.err
	}
	return f.fallback, f.err
}
func (f *fakeAuditClient) GetAuditEntry(_ context.Context, q inventree.AuditEntryQuery) (inventree.AuditEntry, error) {
	f.gets = append(f.gets, q)
	return f.entry, f.err
}
func auditTestDeps(f *fakeAuditClient) Dependencies {
	return Dependencies{ClientFromContext: func(context.Context) (any, error) { return f, nil }}
}
func auditTestEntry(id int) inventree.AuditEntry {
	return inventree.AuditEntry{AuditEntrySummary: inventree.AuditEntrySummary{PK: id, ModelType: "part.part", ModelID: 7, Timestamp: "today", Action: 1, Changes: map[string]any{"description": []any{"old", "new"}}, Deltas: map[string]any{}}, Notes: "exact-only reason"}
}

func TestAuditToolSchemasRequireScopeAndReplaceLegacyNames(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	tools := listedPolicyTools(t, ctx, Dependencies{})
	a.NotContains(tools, "list_stock_tracking_entries")
	a.NotContains(tools, "get_stock_tracking_entry")
	for _, name := range []string{ListAuditEntriesToolName, GetAuditEntryToolName} {
		r.Contains(tools, name)
		schema := tools[name].InputSchema.(map[string]any)
		properties := schema["properties"].(map[string]any)
		a.Equal([]any{"part.part", "company.company", "company.supplierpart", "company.manufacturerpart", "stock.stockitem", "order.purchaseorder"}, properties["model_type"].(map[string]any)["enum"])
		a.Contains(schema["required"], "model_type")
		a.Contains(schema["required"], "model_id")
		if name == GetAuditEntryToolName {
			a.Contains(schema["required"], "id")
		}
		a.True(tools[name].Annotations.ReadOnlyHint)
	}
}
func TestAuditListFollowsEmptyFilteredPagesAndOmitsNotes(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	f := &fakeAuditClient{pages: map[int]inventree.Page[inventree.AuditEntry]{0: {Count: 10, Next: dvgoutils.Ptr("more")}, 100: {Count: 10, Results: []inventree.AuditEntry{auditTestEntry(3), auditTestEntry(1), auditTestEntry(2)}}}}
	_, out, err := listAuditEntries(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, ListAuditEntriesInput{AuditScopeInput: AuditScopeInput{ModelType: "part.part", ModelID: 7}, Offset: 1, Limit: 1})
	r.NoError(err)
	a.Equal(3, out.Count)
	r.Len(out.Results, 1)
	a.Equal(2, out.Results[0].PK)
	r.Len(f.queries, 2)
	a.Equal(100, f.queries[1].Offset)
	encoded, err := json.Marshal(out)
	r.NoError(err)
	a.NotContains(string(encoded), "notes")
	a.NotContains(string(encoded), "exact-only")
}
func TestAuditToolsRejectInvalidScopesAndPaginationBeforeReading(t *testing.T) {
	for _, input := range []ListAuditEntriesInput{
		{}, {AuditScopeInput: AuditScopeInput{ModelType: "auth.user", ModelID: 1}},
		{AuditScopeInput: AuditScopeInput{ModelType: "part.part", ModelID: 0}},
		{AuditScopeInput: AuditScopeInput{ModelType: "part.part", ModelID: 7}, Limit: 1001},
		{AuditScopeInput: AuditScopeInput{ModelType: "part.part", ModelID: 7}, Offset: -1},
	} {
		t.Run(string(input.ModelType), func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			a := assert.New(t)
			f := &fakeAuditClient{}
			_, out, err := listAuditEntries(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, input)
			r.NoError(err)
			a.Equal(StatusClarificationRequired, out.Status)
			a.Empty(f.queries)
			if input.valid() {
				field := "limit"
				if input.Offset < 0 {
					field = "offset"
				}
				r.NotNil(out.Clarification)
				a.Equal(field, out.Clarification.Field)
			}
			if !input.valid() {
				_, detail, detailErr := getAuditEntry(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, GetAuditEntryInput{AuditScopeInput: input.AuditScopeInput, ID: 1})
				r.NoError(detailErr)
				a.Equal(StatusClarificationRequired, detail.Status)
				a.Empty(f.gets)
			}
		})
	}
}
func TestAuditListFailsClosedOnBudgetAndScopeViolations(t *testing.T) {
	tooMany := make([]inventree.AuditEntry, auditMaxRecords+1)
	for i := range tooMany {
		tooMany[i] = auditTestEntry(i + 1)
	}
	cases := []struct {
		name   string
		client *fakeAuditClient
	}{
		{"request budget", &fakeAuditClient{fallback: inventree.Page[inventree.AuditEntry]{Next: dvgoutils.Ptr("more")}}},
		{"record budget", &fakeAuditClient{fallback: inventree.Page[inventree.AuditEntry]{Results: tooMany}}},
		{"wrong scope", &fakeAuditClient{fallback: inventree.Page[inventree.AuditEntry]{Results: []inventree.AuditEntry{{AuditEntrySummary: inventree.AuditEntrySummary{PK: 1, ModelType: "part.part", ModelID: 8}}}}}},
		{"upstream error", &fakeAuditClient{err: errors.New("unavailable")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			_, out, err := listAuditEntries(auditTestDeps(tc.client))(ctx, &mcp.CallToolRequest{}, ListAuditEntriesInput{AuditScopeInput: AuditScopeInput{ModelType: "part.part", ModelID: 7}})
			r.Error(err)
			r.Empty(out.Results)
		})
	}
}
func TestAuditDetailChecksIdentityScopeAndNotes(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	f := &fakeAuditClient{entry: auditTestEntry(4)}
	input := GetAuditEntryInput{AuditScopeInput: AuditScopeInput{ModelType: "part.part", ModelID: 7}, ID: 4}
	_, out, err := getAuditEntry(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	r.NotNil(out.Record)
	a.Equal("exact-only reason", out.Record.Notes)
	for _, id := range []int{0, -1} {
		input.ID = id
		_, missing, readErr := getAuditEntry(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, input)
		r.NoError(readErr)
		a.Equal(StatusNotFound, missing.Status)
	}
	input.ID = 5
	_, _, err = getAuditEntry(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, input)
	r.Error(err)
	input.ID = 4
	input.ModelID = 8
	_, _, err = getAuditEntry(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, input)
	r.Error(err)
	f.err = &inventree.APIError{StatusCode: http.StatusNotFound, Kind: inventree.ErrorKindNotFound}
	_, missing, err := getAuditEntry(auditTestDeps(f))(ctx, &mcp.CallToolRequest{}, input)
	r.NoError(err)
	a.Equal(StatusNotFound, missing.Status)
}
