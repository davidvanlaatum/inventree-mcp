//go:build !no_integration_tests

package tools

import (
	"encoding/json"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runAuditToolsIntegration(t *testing.T, shared *testenv.SharedInvenTree) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	f := newMilestoneToolFixture(t, shared)
	part := f.ensure(t, testenv.FixturePart)
	location := f.ensure(t, testenv.FixtureLocation)
	stock, err := f.client.CreateStockItem(ctx, inventree.StockItemCreate{Part: part.ID, Location: location.ID, Quantity: 3})
	r.NoError(err)
	const reason = "MCP-audit-integration-reason"
	r.NoError(f.client.AddStock(ctx, inventree.StockAdjustment{Items: []inventree.StockAdjustmentItem{{PK: stock.PK, Quantity: "1"}}, Notes: reason}))
	session, closeSession := plannedChangesSession(t, ctx, f.client)
	defer closeSession()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: ListAuditEntriesToolName, Arguments: map[string]any{"model_type": "stock.stockitem", "model_id": stock.PK}})
	r.NoError(err)
	r.False(result.IsError)
	encoded, err := json.Marshal(result.StructuredContent)
	r.NoError(err)
	a.NotContains(string(encoded), reason)
	a.NotContains(string(encoded), `"notes"`)
	var listed AuditEntryListOutput
	r.NoError(json.Unmarshal(encoded, &listed))
	r.NotEmpty(listed.Results)
	foundReason := false
	for _, entry := range listed.Results {
		a.Equal("stock.stockitem", entry.ModelType)
		a.Equal(stock.PK, entry.ModelID)
		detail, readErr := session.CallTool(ctx, &mcp.CallToolParams{Name: GetAuditEntryToolName, Arguments: map[string]any{"model_type": "stock.stockitem", "model_id": stock.PK, "id": entry.PK}})
		r.NoError(readErr)
		r.False(detail.IsError)
		body, marshalErr := json.Marshal(detail.StructuredContent)
		r.NoError(marshalErr)
		var exact AuditEntryDetailOutput
		r.NoError(json.Unmarshal(body, &exact))
		r.NotNil(exact.Record)
		a.Equal(entry.PK, exact.Record.PK)
		if exact.Record.Notes == reason {
			foundReason = true
		}
	}
	a.True(foundReason, "the exact MCP read must retain the requested audit reason")
	foreign, err := session.CallTool(ctx, &mcp.CallToolParams{Name: GetAuditEntryToolName, Arguments: map[string]any{"model_type": "stock.stockitem", "model_id": stock.PK + 1000000, "id": listed.Results[0].PK}})
	r.NoError(err)
	payload, err := json.Marshal(foreign.StructuredContent)
	r.NoError(err)
	var missing AuditEntryDetailOutput
	r.NoError(json.Unmarshal(payload, &missing))
	a.Equal(StatusNotFound, missing.Status)
	a.Nil(missing.Record)
}
