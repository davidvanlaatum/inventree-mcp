package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise SDK serialization and output validation, which direct handler calls
// bypass. InvenTree sends a nested user; MCP exposes only the integer user ID.
func TestPurchaseOrderCreatorOutputOverMCP(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		method    string
		arguments map[string]any
	}{
		{GetPurchaseOrderToolName, http.MethodGet, map[string]any{"id": 120}},
		{UpdatePurchaseOrderToolName, http.MethodPatch, map[string]any{"id": 120, "description": "updated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			ctx, _, _ := testhandler.SetupTestHandler(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				a.Equal(tc.method, req.Method)
				a.Equal("/api/order/po/120/", req.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, err := fmt.Fprint(w, `{"pk":120,"reference":"PO-120","supplier":30,"description":"updated","created_by":{"pk":7,"username":"private-creator","email":"private@example.test"}}`)
				a.NoError(err)
			}))
			t.Cleanup(upstream.Close)
			client, err := inventree.NewClient(inventree.Config{
				BaseURL:    upstream.URL,
				Credential: inventree.Credential{Scheme: inventree.AuthSchemeToken, Token: "test-token"},
				HTTPClient: upstream.Client(),
			})
			r.NoError(err)
			session, closeSession := plannedChangesSession(t, ctx, client)
			defer closeSession()

			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.arguments})
			r.NoError(err)
			r.False(result.IsError, "%+v", result.Content)
			payload, err := json.Marshal(result.StructuredContent)
			r.NoError(err)
			var output struct {
				Status string `json:"status"`
				Record struct {
					PK        int `json:"pk"`
					CreatedBy int `json:"created_by"`
				} `json:"record"`
			}
			r.NoError(json.Unmarshal(payload, &output))
			a.Equal(StatusOK, output.Status)
			a.Equal(120, output.Record.PK)
			a.Equal(7, output.Record.CreatedBy)
			encodedResult, err := json.Marshal(result)
			r.NoError(err)
			a.NotContains(string(encodedResult), "private-creator")
			a.NotContains(string(encodedResult), "private@example.test")
		})
	}
}
