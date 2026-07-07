package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdioServerCanInitializeAndListTools(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- New(tools.Dependencies{}).Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	result, err := session.ListTools(ctx, nil)
	r.NoError(err)
	expectedNames := expectedToolNames(false)
	r.Len(result.Tools, len(expectedNames))
	for _, tool := range result.Tools {
		a.True(expectedNames[tool.Name], tool.Name)
		a.True(tool.Annotations.ReadOnlyHint, tool.Name)
		a.NotNil(tool.Annotations.DestructiveHint, tool.Name)
		a.False(*tool.Annotations.DestructiveHint, tool.Name)
		a.NotNil(tool.Annotations.OpenWorldHint, tool.Name)
		a.False(*tool.Annotations.OpenWorldHint, tool.Name)
	}

	cancel()
	<-serverDone
}

func TestStdioServerListsOnlyMilestonePrompts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- New(tools.Dependencies{}).Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	result, err := session.ListPrompts(ctx, nil)
	r.NoError(err)
	names := make(map[string]bool, len(result.Prompts))
	for _, prompt := range result.Prompts {
		names[prompt.Name] = true
	}

	expectedPrompts := map[string][]string{
		tools.NewPartEntryChecklistPromptName: {
			"dry_run:true",
			"structured clarification",
			"stable IDs",
		},
		tools.ParameterReuseChecklistPromptName: {
			"structured clarification",
			"stable template_id",
			"Do not create new parameter templates",
		},
		tools.AttachmentImageChecklistPromptName: {
			"structured clarification",
			"Current milestone tools",
			"confirmed attachments",
		},
		tools.InitialStockEntryChecklistPromptName: {
			"dry_run:true",
			"structured clarification",
			"stable part_id",
		},
		tools.PurchasePreviewChecklistPromptName: {
			"no-write",
			"structured clarification",
			"must not create purchase orders",
		},
	}
	for name, snippets := range expectedPrompts {
		a.True(names[name], name)
		prompt, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name})
		r.NoError(err)
		r.Len(prompt.Messages, 1)
		text := prompt.Messages[0].Content.(*mcp.TextContent).Text
		for _, snippet := range snippets {
			a.Contains(text, snippet, name)
		}
	}

	for _, name := range []string{"receive_purchase_order_checklist", "bom_import_review", "stocktake_review"} {
		a.False(names[name], name)
		_, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name})
		a.Error(err, name)
	}

	cancel()
	<-serverDone
}

func expectedToolNames(includeWrites bool) map[string]bool {
	names := make(map[string]bool, len(tools.ToolAuthorizations))
	for _, auth := range tools.ToolAuthorizations {
		if includeWrites || auth.MutationClass == "read_only" {
			names[auth.Name] = true
		}
	}
	return names
}

func TestServerListsWriteToolsOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- New(tools.Dependencies{EnableWriteTools: true}).Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	result, err := session.ListTools(ctx, nil)
	r.NoError(err)
	expectedNames := expectedToolNames(true)
	r.Len(result.Tools, len(expectedNames))
	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	a.Equal(expectedNames, names)
	a.True(names[tools.CreatePartToolName])
	a.True(names[tools.CreateCompanyToolName])
	a.True(names[tools.CreateStockItemToolName])

	cancel()
	<-serverDone
}

func TestRunRejectsHTTPWriteToolsBeforeOAuthScopeEnforcement(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	err := Run(ctx, config.Config{Transport: config.TransportHTTP}, tools.Dependencies{EnableWriteTools: true})

	r.Error(err)
	a.Contains(err.Error(), "HTTP transport cannot register write tools")
}

func TestHealthVersionToolReturnsReadOnlyStatus(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- New(tools.Dependencies{}).Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tools.HealthVersionToolName})
	r.NoError(err)
	r.False(result.IsError)
	r.Len(result.Content, 1)
	a.Equal("ok", result.Content[0].(*mcp.TextContent).Text)
	structured := result.StructuredContent.(map[string]any)
	a.Equal("ok", structured["status"])
	a.Equal("dev", structured["version"])
	a.Equal("unknown", structured["commit"])
	a.Equal("unknown", structured["date"])

	cancel()
	<-serverDone
}

func TestHTTPHandlerUsesStatelessStreamableServer(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	handler := HTTPHandler(ctx, New(tools.Dependencies{}))

	initRecorder := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)
	r.Equal(http.StatusOK, initRecorder.Code)
	a.Contains(initRecorder.Body.String(), "inventree-mcp")

	listRecorder := postMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusOK, listRecorder.Code)
	a.Empty(listRecorder.Header().Get("Mcp-Session-Id"))
	a.Contains(listRecorder.Body.String(), tools.HealthVersionToolName)
	for name, auth := range tools.ToolAuthorizations {
		if auth.MutationClass != "read_only" {
			a.NotContains(listRecorder.Body.String(), name)
		}
	}
}

func postMCP(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	return recorder
}

func TestRequestAndToolScopedLoggersAreReattached(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, handler, _ := testhandler.SetupTestHandler(t)
	ctx = WithTransportLogger(ctx, "stdio")
	logging.FromContext(ctx).InfoContext(ctx, "request scoped")

	record := handler.FirstMatchingLogForAssert(func(record testhandler.LogRecord) bool {
		return record.Msg == "request scoped"
	})
	r.NotNil(record)
	a.Equal("stdio", record["transport"])
}
