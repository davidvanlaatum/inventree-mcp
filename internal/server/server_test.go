package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
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
		serverDone <- New(tools.Dependencies{UploadMode: upload.ModeStdio}).Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	result, err := session.ListTools(ctx, nil)
	r.NoError(err)
	initializeResult := session.InitializeResult()
	r.NotNil(initializeResult)
	r.NotNil(initializeResult.ServerInfo)
	a.Equal([]mcp.Icon{tools.InvenTreeIcon()}, initializeResult.ServerInfo.Icons)
	a.Equal(serverInstructions, initializeResult.Instructions)
	a.Contains(initializeResult.Instructions, "Route image intent to the dedicated tools")
	a.Contains(initializeResult.Instructions, "download_part_image to read a part's current primary image")
	a.Contains(initializeResult.Instructions, "list_attachments and set_primary_image to assign or replace it")
	a.Contains(initializeResult.Instructions, "set_company_image, set_company_image_from_url, or clear_company_image")
	a.Contains(initializeResult.Instructions, "generic attachment tools only for separately addressable files or stored links")
	expectedNames := expectedToolNames(false)
	r.Len(result.Tools, len(expectedNames))
	seenNames := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		a.True(expectedNames[tool.Name], tool.Name)
		seenNames[tool.Name] = true
		a.True(tool.Annotations.ReadOnlyHint, tool.Name)
		a.NotNil(tool.Annotations.DestructiveHint, tool.Name)
		a.False(*tool.Annotations.DestructiveHint, tool.Name)
		a.NotNil(tool.Annotations.OpenWorldHint, tool.Name)
		a.False(*tool.Annotations.OpenWorldHint, tool.Name)
		a.Equal([]mcp.Icon{tools.InvenTreeIcon()}, tool.Icons, tool.Name)
	}
	a.Equal(expectedNames, seenNames)
	descriptions := map[string]string{
		tools.ListAttachmentsToolName:    "find a same-part image attachment before calling set_primary_image",
		tools.DownloadAttachmentToolName: "use download_part_image instead",
		tools.DownloadPartImageToolName:  "not a generic attachment",
	}
	seenDescriptions := make(map[string]bool, len(descriptions))
	for _, tool := range result.Tools {
		if expected, ok := descriptions[tool.Name]; ok {
			a.Contains(tool.Description, expected, tool.Name)
			seenDescriptions[tool.Name] = true
		}
	}
	for name := range descriptions {
		r.True(seenDescriptions[name], "missing routing description coverage for %s", name)
	}

	cancel()
	<-serverDone
}

func TestHTTPMuxExposesConfiguredPrometheusMetricsPath(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	runtime, err := telemetry.New(ctx, telemetry.Config{
		MetricsEnabled: true,
		MetricsPath:    "/internal/metrics",
		ServiceName:    "inventree-mcp",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })

	cfg := config.Config{
		Transport:              config.TransportHTTP,
		Environment:            config.EnvironmentDevelopment,
		Path:                   "/mcp",
		InvenTreeURL:           "http://inventory.example.test",
		MCPMaxRequestBodyBytes: config.DefaultMCPMaxRequestBodyBytes,
		Telemetry:              telemetry.Config{MetricsEnabled: true, MetricsPath: "/internal/metrics"},
		DevIncompleteOAuth:     true,
	}
	handler, err := httpMux(ctx, cfg, New(tools.Dependencies{}), nil)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "# HELP")
}

func TestTrafficLogCapturesStdioJSONRPCMessages(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var output strings.Builder
	traffic := &trafficLog{w: &output}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- New(tools.Dependencies{UploadMode: upload.ModeStdio}).Run(ctx, loggingTransport{
			transport: serverTransport,
			log:       traffic,
			name:      string(config.TransportStdio),
		})
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	result := session.InitializeResult()
	r.NotNil(result)
	a.Equal("2026-07-28", result.ProtocolVersion)
	_, err = session.ListTools(ctx, nil)
	r.NoError(err)
	r.NoError(session.Close())
	cancel()
	<-serverDone

	var methods []string
	var outboundCount int
	var foundServerIcon bool
	var foundToolIcons bool
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var entry trafficLogEntry
		r.NoError(json.Unmarshal([]byte(line), &entry))
		a.Equal(string(config.TransportStdio), entry.Transport)
		a.NotEmpty(entry.Time)
		if entry.Direction == "outbound" {
			outboundCount++
		}
		if len(entry.Message) == 0 {
			continue
		}
		var message map[string]any
		r.NoError(json.Unmarshal(entry.Message, &message))
		if method, ok := message["method"].(string); ok {
			methods = append(methods, entry.Direction+":"+method)
		}
		if entry.Direction != "outbound" {
			continue
		}
		result, ok := message["result"].(map[string]any)
		if !ok {
			continue
		}
		if _, isDiscoverResult := result["supportedVersions"]; isDiscoverResult {
			a.Equal(serverInstructions, result["instructions"])
			meta, ok := result["_meta"].(map[string]any)
			r.True(ok)
			serverInfo, ok := meta["io.modelcontextprotocol/serverInfo"].(map[string]any)
			r.True(ok)
			assertOfficialIconJSON(t, serverInfo["icons"], "serverInfo")
			foundServerIcon = true
		}
		if listedTools, ok := result["tools"].([]any); ok {
			r.Len(listedTools, len(expectedToolNames(false)))
			for _, listedTool := range listedTools {
				tool, ok := listedTool.(map[string]any)
				r.True(ok)
				name, _ := tool["name"].(string)
				assertOfficialIconJSON(t, tool["icons"], name)
			}
			foundToolIcons = true
		}
	}

	a.Contains(methods, "inbound:server/discover")
	a.Contains(methods, "inbound:tools/list")
	a.NotContains(methods, "inbound:initialize")
	a.NotContains(methods, "inbound:notifications/initialized")
	a.Positive(outboundCount)
	a.True(foundServerIcon)
	a.True(foundToolIcons)
}

func TestStdioLegacyInitializePublishesServerInstructions(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var output strings.Builder
	traffic := &trafficLog{w: &output}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	srv := New(tools.Dependencies{})
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "server/discover" {
				return &mcp.DiscoverResult{
					SupportedVersions: []string{"2025-11-25"},
					Capabilities:      &mcp.ServerCapabilities{},
				}, nil
			}
			return next(ctx, method, req)
		}
	})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Run(ctx, loggingTransport{
			transport: serverTransport,
			log:       traffic,
			name:      string(config.TransportStdio),
		})
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	initializeResult := session.InitializeResult()
	r.NotNil(initializeResult)
	a.Equal("2025-11-25", initializeResult.ProtocolVersion)
	a.Equal(serverInstructions, initializeResult.Instructions)
	r.NoError(session.Close())
	cancel()
	<-serverDone

	var foundInitializeRequest bool
	var foundInitializeResult bool
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var entry trafficLogEntry
		r.NoError(json.Unmarshal([]byte(line), &entry))
		var message map[string]any
		r.NoError(json.Unmarshal(entry.Message, &message))
		if entry.Direction == "inbound" && message["method"] == "initialize" {
			foundInitializeRequest = true
		}
		if entry.Direction != "outbound" {
			continue
		}
		result, ok := message["result"].(map[string]any)
		if !ok || result["protocolVersion"] != "2025-11-25" {
			continue
		}
		a.Equal(serverInstructions, result["instructions"])
		foundInitializeResult = true
	}
	a.True(foundInitializeRequest)
	a.True(foundInitializeResult)
}

func assertOfficialIconJSON(t *testing.T, value any, descriptor string) {
	t.Helper()
	r := require.New(t)
	a := assert.New(t)

	icons, ok := value.([]any)
	r.True(ok, descriptor)
	r.Len(icons, 1, descriptor)
	icon, ok := icons[0].(map[string]any)
	r.True(ok, descriptor)
	a.Equal(tools.InvenTreeIconSource, icon["src"], descriptor)
	a.Equal("image/png", icon["mimeType"], descriptor)
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
			"Use create_parameter_template only",
			"merge_parameter_templates with dry_run:true",
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
		tools.ReceivePurchaseOrderChecklistPromptName: {
			"stable IDs",
			"dry_run:true",
			"confirm_receive:true",
		},
		tools.StocktakeReviewPromptName: {
			"stable stock_item_id",
			"dry_run:true",
			"plan_hash",
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

	for _, name := range []string{"bom_import_review"} {
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
		serverDone <- New(tools.Dependencies{EnableWriteTools: true, UploadMode: upload.ModeStdio}).Run(ctx, serverTransport)
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
		a.Equal([]mcp.Icon{tools.InvenTreeIcon()}, tool.Icons, tool.Name)
	}
	a.Equal(expectedNames, names)
	a.True(names[tools.CreatePartToolName])
	a.True(names[tools.CreateCompanyToolName])
	a.True(names[tools.CreateStockItemToolName])
	descriptions := map[string]string{
		tools.CreatePartToolName:              "Primary part images are managed separately with set_primary_image",
		tools.UpdatePartToolName:              "Primary part images are managed separately with set_primary_image",
		tools.UpsertPartWorkflowToolName:      "Primary part images are managed separately with set_primary_image",
		tools.CreateCompanyToolName:           "Company primary images are managed separately with set_company_image",
		tools.UpdateCompanyToolName:           "Company primary images are managed separately with set_company_image",
		tools.UploadAttachmentToolName:        "follow with set_primary_image",
		tools.UploadAttachmentFromURLToolName: "follow with set_primary_image",
		tools.SetPrimaryImageToolName:         "dedicated part-image tool",
		tools.SetCompanyImageToolName:         "dedicated company-image tool",
		tools.SetCompanyImageFromURLToolName:  "dedicated company-image tool",
		tools.ClearCompanyImageToolName:       "dedicated company-image tool",
	}
	seenDescriptions := make(map[string]bool, len(descriptions))
	for _, tool := range result.Tools {
		if expected, ok := descriptions[tool.Name]; ok {
			a.Contains(tool.Description, expected, tool.Name)
			seenDescriptions[tool.Name] = true
		}
	}
	for name := range descriptions {
		r.True(seenDescriptions[name], "missing routing description coverage for %s", name)
	}

	cancel()
	<-serverDone
}

func TestRunRejectsHTTPWriteToolsBeforeOAuthScopeEnforcement(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	err := Run(ctx, config.Config{Transport: config.TransportHTTP}, tools.Dependencies{EnableWriteTools: true}, &serverRecordingNotifier{})

	r.Error(err)
	a.Contains(err.Error(), "HTTP transport cannot register write tools without per-tool OAuth scope enforcement")
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
	handler := HTTPHandler(ctx, New(tools.Dependencies{}), config.DefaultMCPMaxRequestBodyBytes)

	initRecorder := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)
	r.Equal(http.StatusOK, initRecorder.Code)
	a.Contains(initRecorder.Body.String(), "inventree-mcp")
	encodedInstructions, err := json.Marshal(serverInstructions)
	r.NoError(err)
	a.Contains(initRecorder.Body.String(), `"instructions":`+string(encodedInstructions))

	listRecorder := postMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusOK, listRecorder.Code)
	a.Empty(listRecorder.Header().Get("Mcp-Session-Id"))
	listedTools := decodeListedTools(t, listRecorder.Body.Bytes())
	a.Contains(listedTools, tools.HealthVersionToolName)
	for name, auth := range tools.ToolAuthorizations {
		if auth.MutationClass != "read_only" {
			a.NotContains(listedTools, name)
		}
	}
}

func TestHTTPHandlerNegotiates20260728ThroughDiscover(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	httpServer := httptest.NewServer(HTTPHandler(ctx, New(tools.Dependencies{}), config.DefaultMCPMaxRequestBodyBytes))
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	result := session.InitializeResult()
	r.NotNil(result)
	a.Equal("2026-07-28", result.ProtocolVersion)
	r.NotNil(result.ServerInfo)
	a.Equal("inventree-mcp", result.ServerInfo.Name)
	a.Equal(serverInstructions, result.Instructions)
	a.Empty(session.ID())

	listed, err := session.ListTools(ctx, nil)
	r.NoError(err)
	a.NotEmpty(listed.Tools)
}

func TestOAuthProtects20260728ServerDiscovery(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	resourceMetadataURL := "https://mcp.example.com/.well-known/oauth-protected-resource"
	protected := auth.RequireBearerToken(serverTokenVerifier(t), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: resourceMetadataURL,
	})(HTTPHandler(ctx, New(tools.Dependencies{}), config.DefaultMCPMaxRequestBodyBytes))

	missingBearer := postMCPDiscover(t, protected, "")
	r.Equal(http.StatusUnauthorized, missingBearer.Code)
	a.Contains(missingBearer.Header().Get("WWW-Authenticate"), resourceMetadataURL)

	httpServer := httptest.NewServer(protected)
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "oauth-discovery-test", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{
			base:  http.DefaultTransport,
			token: "read-token",
		}},
	}, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()
	r.NotNil(session.InitializeResult())
	a.Equal("2026-07-28", session.InitializeResult().ProtocolVersion)
}

func TestHTTPHandlerRejectsSessionOperationsInStatelessMode(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	handler := HTTPHandler(ctx, New(tools.Dependencies{}), config.DefaultMCPMaxRequestBodyBytes)
	post := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("Accept", "application/json, text/event-stream")
	post.Header.Set("Mcp-Session-Id", "ignored-session")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	r.Equal(http.StatusOK, postRecorder.Code)
	a.Empty(postRecorder.Header().Get("Mcp-Session-Id"))
	a.Contains(postRecorder.Body.String(), tools.HealthVersionToolName)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "legacy-session")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		r.Equal(http.StatusMethodNotAllowed, recorder.Code, method)
		a.Empty(recorder.Header().Get("Mcp-Session-Id"), method)
	}
}

func TestHTTPHandlerRejectsOversizedRequestBody(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	handler := HTTPHandler(ctx, New(tools.Dependencies{}), 1024)
	recorder := postMCP(t, handler, strings.Repeat(" ", 2048))

	r.Equal(http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestHTTPHandlerPropagatesAborted20260728RequestCancellation(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	started := make(chan struct{})
	handlerCanceled := make(chan struct{})
	srv := mcp.NewServer(&mcp.Implementation{Name: "cancellation-test", Version: "v0.0.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "wait_for_cancellation"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		close(started)
		<-ctx.Done()
		close(handlerCanceled)
		return nil, nil, ctx.Err()
	})
	httpServer := httptest.NewServer(HTTPHandler(ctx, srv, config.DefaultMCPMaxRequestBodyBytes))
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	r.NoError(err)
	defer func() {
		r.NoError(session.Close())
	}()

	callCtx, cancel := context.WithCancel(ctx)
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: "wait_for_cancellation"})
		callDone <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool handler did not start")
	}
	cancel()

	select {
	case <-handlerCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("tool handler context was not cancelled")
	}
	select {
	case err := <-callDone:
		r.Error(err)
		r.ErrorIs(err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled tool call did not return")
	}
}

func TestHTTPToolsExposeSecuritySchemesAndEnforcePerToolScopes(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	var clientCalls atomic.Int32
	deps := tools.Dependencies{
		EnableWriteTools:    true,
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
		ClientFromContext: func(context.Context) (any, error) {
			clientCalls.Add(1)
			return serverLookupClient{}, nil
		},
	}
	protected := auth.RequireBearerToken(serverTokenVerifier(t), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: deps.ResourceMetadataURL,
	})(HTTPHandler(ctx, New(deps), config.DefaultMCPMaxRequestBodyBytes))

	initRecorder := postMCPWithBearer(t, protected, "read-token", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)
	r.Equal(http.StatusOK, initRecorder.Code)

	listRecorder := postMCPWithBearer(t, protected, "read-token", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusOK, listRecorder.Code)
	listedTools := decodeListedTools(t, listRecorder.Body.Bytes())
	for name, authz := range tools.ToolAuthorizations {
		tool := listedTools[name]
		r.NotNil(tool, name)
		if len(authz.Scopes) == 0 {
			a.Empty(tool.Meta, name)
			continue
		}
		a.Equal([]string{"oauth2:" + strings.Join(authz.Scopes, " ")}, securitySchemeSummaries(tool.Meta[tools.MetaSecuritySchemesKey]), name)
		a.Equal([]string{"oauth2:" + strings.Join(authz.Scopes, " ")}, securitySchemeSummaries(tool.Meta[tools.MetaOpenAISecuritySchemesKey]), name)
	}

	deniedRecorder := postMCPWithBearer(t, protected, "read-token", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_part","arguments":{"name":"10k resistor","category_id":20}}}`)
	r.Equal(http.StatusOK, deniedRecorder.Code)
	a.Contains(deniedRecorder.Body.String(), `"isError":true`)
	a.Contains(deniedRecorder.Body.String(), `"mcp/www_authenticate"`)
	a.Contains(deniedRecorder.Body.String(), `scope=\"inventree.read inventree.write\"`)
	a.Contains(deniedRecorder.Body.String(), `error=\"insufficient_scope\"`)
	a.Contains(deniedRecorder.Body.String(), `error_description=`)
	a.NotContains(deniedRecorder.Body.String(), "secret-inventree-token")
	a.Equal(int32(0), clientCalls.Load())

	for _, tt := range []struct {
		name       string
		token      string
		tool       string
		arguments  string
		wantScopes string
	}{
		{
			name:       "operational scope",
			token:      "write-token",
			tool:       tools.CreateStockItemToolName,
			arguments:  `"part_id":10,"location_id":20,"quantity":1`,
			wantScopes: `scope=\"inventree.write inventree.operational\"`,
		},
		{
			name:       "upload scope",
			token:      "write-token",
			tool:       tools.UploadAttachmentToolName,
			arguments:  `"model_type":"part","model_id":10,"filename":"data.txt","content_type":"text/plain","inline_base64":"ZGF0YQ=="`,
			wantScopes: `scope=\"inventree.write inventree.upload\"`,
		},
		{
			name:       "destructive scope",
			token:      "write-upload-token",
			tool:       tools.DeleteAttachmentToolName,
			arguments:  `"id":90,"confirm":true`,
			wantScopes: `scope=\"inventree.write inventree.upload inventree.destructive\"`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			r := require.New(t)
			before := clientCalls.Load()

			recorder := postMCPWithBearer(t, protected, tt.token, `{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"`+tt.tool+`","arguments":{`+tt.arguments+`}}}`)

			r.Equal(http.StatusOK, recorder.Code)
			a.Contains(recorder.Body.String(), `"isError":true`)
			a.Contains(recorder.Body.String(), tt.wantScopes)
			a.Contains(recorder.Body.String(), `error=\"insufficient_scope\"`)
			a.Equal(before, clientCalls.Load())
		})
	}

	allowedRecorder := postMCPWithBearer(t, protected, "read-token", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"10k"}}}`)
	r.Equal(http.StatusOK, allowedRecorder.Code)
	a.Contains(allowedRecorder.Body.String(), `"status":"ok"`)
	a.Contains(allowedRecorder.Body.String(), "10k resistor")
}

func TestHTTPOAuthCredentialPropagationIsRequestScoped(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"pk":1,"name":`+strconv.Quote(authHeader)+`,"active":true}]`)
	}))
	defer upstream.Close()

	resolver, err := weblinks.New("https://trusted.inventory.example/inventree", "INVENTREE_WEB_URL", true)
	r.NoError(err)
	deps := tools.Dependencies{
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
		ClientFromContext:   OAuthClientFromContext(upstream.URL, upstream.Client()),
		WebLinks:            resolver,
	}
	protected := auth.RequireBearerToken(serverTokenVerifier(t), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: deps.ResourceMetadataURL,
	})(HTTPHandler(ctx, New(deps), config.DefaultMCPMaxRequestBodyBytes))

	var wg sync.WaitGroup
	for _, tt := range []struct {
		token      string
		wantHeader string
	}{
		{token: "credential-alpha", wantHeader: "Token alpha"},
		{token: "credential-beta", wantHeader: "Token beta"},
	} {
		tt := tt
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"https://argument.attacker.example"}}}`))
			req.Host = "internal.service:28686"
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", "Bearer "+tt.token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-Host", "forwarded.attacker.example")
			req.Header.Set("X-Forwarded-Proto", "http")
			req.Header.Set("X-Forwarded-Prefix", "/attacker")
			recorder := httptest.NewRecorder()
			protected.ServeHTTP(recorder, req)
			r.Equal(http.StatusOK, recorder.Code)
			a.Contains(recorder.Body.String(), `"status":"ok"`)
			a.Contains(recorder.Body.String(), tt.wantHeader)
			a.Contains(recorder.Body.String(), `"web_url":"https://trusted.inventory.example/inventree/part/1/"`)
			a.NotContains(recorder.Body.String(), "internal.service")
			a.NotContains(recorder.Body.String(), "attacker.example")
		}()
	}
	wg.Wait()
}

func TestSDKAuthMiddlewareProtectsStatelessHTTPAndPropagatesTokenInfo(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "auth-spike", Version: "v0.0.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "whoami",
		Title:       "Who am I",
		Description: "Returns the authenticated SDK token context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil {
			return nil, nil, auth.ErrInvalidToken
		}
		out := map[string]any{
			"user_id": tokenInfo.UserID,
			"scopes":  tokenInfo.Scopes,
			"tenant":  tokenInfo.Extra["tenant"],
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: tokenInfo.UserID}},
		}, out, nil
	})

	var verifierSawPath string
	var tokenVerifier auth.TokenVerifier = func(_ context.Context, token string, req *http.Request) (*auth.TokenInfo, error) {
		verifierSawPath = req.URL.Path
		switch token {
		case "valid-mcp-token":
			return &auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-1",
				Extra:      map[string]any{"tenant": "inventree-main"},
			}, nil
		case "expired-mcp-token":
			return &auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead},
				Expiration: time.Now().Add(-time.Hour),
				UserID:     "operator-1",
				Extra:      map[string]any{"tenant": "inventree-main"},
			}, nil
		case "wrong-scope-mcp-token":
			return &auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeWrite},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-1",
				Extra:      map[string]any{"tenant": "inventree-main"},
			}, nil
		default:
			return nil, auth.ErrInvalidToken
		}
	}
	protected := auth.RequireBearerToken(tokenVerifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
		Scopes:              []string{tools.ScopeInventreeRead},
	})(HTTPHandler(ctx, srv, config.DefaultMCPMaxRequestBodyBytes))

	missingBearer := postMCP(t, protected, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)
	r.Equal(http.StatusUnauthorized, missingBearer.Code)
	a.Contains(missingBearer.Header().Get("WWW-Authenticate"), `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`)
	a.Contains(missingBearer.Header().Get("WWW-Authenticate"), `scope="inventree.read"`)

	for _, tt := range []struct {
		name       string
		token      string
		wantStatus int
		wantBody   string
	}{
		{name: "invalid token", token: "not-an-mcp-token", wantStatus: http.StatusUnauthorized, wantBody: "invalid token"},
		{name: "expired token", token: "expired-mcp-token", wantStatus: http.StatusUnauthorized, wantBody: "token expired"},
		{name: "insufficient scope", token: "wrong-scope-mcp-token", wantStatus: http.StatusForbidden, wantBody: "insufficient scope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			r := require.New(t)

			recorder := postMCPWithBearer(t, protected, tt.token, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)

			r.Equal(tt.wantStatus, recorder.Code)
			a.Contains(recorder.Body.String(), tt.wantBody)
			a.Contains(recorder.Header().Get("WWW-Authenticate"), `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`)
			a.Contains(recorder.Header().Get("WWW-Authenticate"), `scope="inventree.read"`)
		})
	}

	validInit := postMCPWithBearer(t, protected, "valid-mcp-token", `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)
	r.Equal(http.StatusOK, validInit.Code)
	a.Contains(validInit.Body.String(), "auth-spike")
	a.Equal("/mcp", verifierSawPath)

	validCall := postMCPWithBearer(t, protected, "valid-mcp-token", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`)
	r.Equal(http.StatusOK, validCall.Code)
	a.Contains(validCall.Body.String(), "operator-1")
	a.Contains(validCall.Body.String(), "inventree-main")
}

func TestSDKProtectedResourceMetadataHandlerPublishesOAuthResourceMetadata(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	handler := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:             "https://mcp.example.com",
		AuthorizationServers: []string{"https://mcp.example.com"},
		ScopesSupported:      []string{tools.ScopeInventreeRead, tools.ScopeInventreeWrite},
		ResourceName:         "InvenTree MCP",
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	r.Equal(http.StatusOK, recorder.Code)
	a.Equal("*", recorder.Header().Get("Access-Control-Allow-Origin"))
	var metadata oauthex.ProtectedResourceMetadata
	r.NoError(json.Unmarshal(recorder.Body.Bytes(), &metadata))
	a.Equal("https://mcp.example.com", metadata.Resource)
	a.Equal([]string{"https://mcp.example.com"}, metadata.AuthorizationServers)
	a.Equal([]string{tools.ScopeInventreeRead, tools.ScopeInventreeWrite}, metadata.ScopesSupported)
	a.Equal("InvenTree MCP", metadata.ResourceName)
}

func TestProductionHTTPMuxProtectsMCPAndPublishesResourceMetadata(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	keyring, err := oauth.NewKeyring([]oauth.Key{{
		ID:       "current",
		Material: []byte("0123456789abcdef0123456789abcdef"),
		State:    oauth.KeyStateActive,
	}})
	r.NoError(err)
	cfg := config.Config{
		Transport:        config.TransportHTTP,
		Environment:      config.EnvironmentProduction,
		Path:             "/mcp",
		OAuthIssuerURL:   "https://auth.example.test",
		OAuthResourceURL: "https://mcp.example.test/mcp",
		OAuthClientIDs:   []string{"https://chatgpt.com/client-metadata"},
		OAuthKeyring: oauth.KeyringConfig{Keys: []oauth.KeyConfig{{
			ID:             "current",
			MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
			State:          oauth.KeyStateActive,
		}}},
	}
	var clientCalls atomic.Int32
	deps := tools.Dependencies{
		EnableWriteTools:    true,
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
		ClientFromContext: func(context.Context) (any, error) {
			clientCalls.Add(1)
			return serverLookupClient{}, nil
		},
	}
	handler, err := HTTPMux(ctx, cfg, New(deps))
	r.NoError(err)

	metadataReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil)
	metadataRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metadataRecorder, metadataReq)
	r.Equal(http.StatusOK, metadataRecorder.Code)
	var metadata oauthex.ProtectedResourceMetadata
	r.NoError(json.Unmarshal(metadataRecorder.Body.Bytes(), &metadata))
	a.Equal(cfg.OAuthResourceURL, metadata.Resource)
	a.Equal([]string{cfg.OAuthIssuerURL}, metadata.AuthorizationServers)
	a.ElementsMatch([]string{
		tools.ScopeInventreeRead,
		tools.ScopeInventreeWrite,
		tools.ScopeInventreeUpload,
		tools.ScopeInventreeOperational,
		tools.ScopeInventreeDestructive,
	}, metadata.ScopesSupported)
	rootMetadataRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rootMetadataRecorder, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	r.Equal(http.StatusNotFound, rootMetadataRecorder.Code)
	authorizationMetadataRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizationMetadataRecorder, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	r.Equal(http.StatusOK, authorizationMetadataRecorder.Code)
	var authorizationMetadata map[string]any
	r.NoError(json.Unmarshal(authorizationMetadataRecorder.Body.Bytes(), &authorizationMetadata))
	a.Equal(cfg.OAuthIssuerURL, authorizationMetadata["issuer"])
	a.Equal(cfg.OAuthIssuerURL+"/authorize", authorizationMetadata["authorization_endpoint"])
	a.Equal(cfg.OAuthIssuerURL+"/token", authorizationMetadata["token_endpoint"])
	a.Equal([]any{"private_key_jwt"}, authorizationMetadata["token_endpoint_auth_methods_supported"])
	a.Equal("no-store", authorizationMetadataRecorder.Header().Get("Cache-Control"))

	missingBearer := postMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test-client","version":"v0.0.0"},"capabilities":{}}}`)
	r.Equal(http.StatusUnauthorized, missingBearer.Code)
	a.Contains(missingBearer.Header().Get("WWW-Authenticate"), `resource_metadata="https://mcp.example.test/.well-known/oauth-protected-resource/mcp"`)

	now := time.Now()
	codec := oauth.EnvelopeCodec{Keyring: keyring}
	accessToken, err := codec.Seal(ctx, oauth.AssociatedData{
		Issuer:   cfg.OAuthIssuerURL,
		Audience: cfg.OAuthResourceURL,
		ClientID: cfg.OAuthClientIDs[0],
		Type:     oauth.TokenTypeAccess,
	}, oauth.TokenClaims{
		Type:             oauth.TokenTypeAccess,
		Issuer:           cfg.OAuthIssuerURL,
		Audience:         cfg.OAuthResourceURL,
		Subject:          "operator-1",
		ClientID:         cfg.OAuthClientIDs[0],
		Scopes:           []string{tools.ScopeInventreeRead},
		IssuedAt:         now,
		ExpiresAt:        now.Add(time.Hour),
		SessionExpiresAt: now.Add(2 * time.Hour),
		Credential:       oauth.Credential{Scheme: inventree.AuthSchemeToken, Token: "secret-inventree-token"},
	})
	r.NoError(err)

	listRecorder := postMCPWithBearer(t, handler, accessToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusOK, listRecorder.Code)
	listedTools := decodeListedTools(t, listRecorder.Body.Bytes())
	a.NotNil(listedTools[tools.CreatePartToolName])
	a.Equal([]string{"oauth2:inventree.read inventree.write"}, securitySchemeSummaries(listedTools[tools.CreatePartToolName].Meta[tools.MetaSecuritySchemesKey]))

	deniedRecorder := postMCPWithBearer(t, handler, accessToken, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_part","arguments":{"name":"10k resistor","category_id":20}}}`)
	r.Equal(http.StatusOK, deniedRecorder.Code)
	a.Contains(deniedRecorder.Body.String(), `"isError":true`)
	a.Contains(deniedRecorder.Body.String(), `insufficient_scope`)
	a.Contains(deniedRecorder.Body.String(), `scope=\"inventree.read inventree.write\"`)
	a.Equal(int32(0), clientCalls.Load())

	rawUpstreamRecorder := postMCPWithBearer(t, handler, "secret-inventree-token", `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusUnauthorized, rawUpstreamRecorder.Code)
	a.NotContains(rawUpstreamRecorder.Body.String(), tools.CreatePartToolName)
}

func TestProductionHTTPMuxPreservesCanonicalPrefixAndIgnoresForwardedURLHeaders(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var clientID string
	metadataServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/client" {
			http.NotFound(w, req)
			return
		}
		_ = json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID:                          clientID,
			RedirectURIs:                      []string{redirectURI},
			TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"},
			JWKSURI:                           "https://" + req.Host + "/jwks",
		})
	}))
	defer metadataServer.Close()
	clientID = metadataServer.URL + "/client"
	cfg := config.Config{
		Transport:            config.TransportHTTP,
		Environment:          config.EnvironmentProduction,
		Path:                 "/connectors/inventree/mcp",
		OAuthIssuerURL:       "https://public.example.test/connectors/inventree",
		OAuthResourceURL:     "https://public.example.test/connectors/inventree/mcp",
		OAuthClientIDs:       []string{clientID},
		TrustedProxyCIDRs:    []string{"10.0.0.0/8"},
		OAuthAccessLifetime:  oauth.DefaultAccessTokenLifetime,
		OAuthRefreshLifetime: oauth.DefaultRefreshTokenLifetime,
		OAuthSessionLifetime: oauth.DefaultSessionLifetime,
		OAuthKeyring: oauth.KeyringConfig{Keys: []oauth.KeyConfig{{
			ID:             "current",
			MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
			State:          oauth.KeyStateActive,
		}}},
	}
	handler, err := httpMuxWithOptions(ctx, cfg, New(tools.Dependencies{}), nil, httpMuxOptions{metadataClient: metadataServer.Client()})
	r.NoError(err)
	request := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "10.0.0.10:1234"
		req.Host = "internal.service:28686"
		req.Header.Set("X-Forwarded-For", "203.0.113.20")
		req.Header.Set("X-Forwarded-Host", "attacker.example")
		req.Header.Set("X-Forwarded-Proto", "http")
		req.Header.Set("X-Forwarded-Prefix", "/attacker")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	resourceMetadata := request("/.well-known/oauth-protected-resource/connectors/inventree/mcp")
	r.Equal(http.StatusOK, resourceMetadata.Code)
	a.Contains(resourceMetadata.Body.String(), cfg.OAuthResourceURL)
	a.Contains(resourceMetadata.Body.String(), cfg.OAuthIssuerURL)
	a.NotContains(resourceMetadata.Body.String(), "internal.service")
	a.NotContains(resourceMetadata.Body.String(), "attacker")

	authorizationMetadata := request("/.well-known/oauth-authorization-server/connectors/inventree")
	r.Equal(http.StatusOK, authorizationMetadata.Code)
	a.Contains(authorizationMetadata.Body.String(), `"authorization_endpoint":"https://public.example.test/connectors/inventree/authorize"`)
	a.Contains(authorizationMetadata.Body.String(), `"token_endpoint":"https://public.example.test/connectors/inventree/token"`)
	a.NotContains(authorizationMetadata.Body.String(), "internal.service")
	a.NotContains(authorizationMetadata.Body.String(), "attacker")

	protected := request(cfg.Path)
	r.Equal(http.StatusUnauthorized, protected.Code)
	a.Contains(protected.Header().Get("WWW-Authenticate"), `resource_metadata="https://public.example.test/.well-known/oauth-protected-resource/connectors/inventree/mcp"`)
	a.NotContains(protected.Header().Get("WWW-Authenticate"), "internal.service")
	a.NotContains(protected.Header().Get("WWW-Authenticate"), "attacker")

	unprefixed := request("/mcp")
	r.Equal(http.StatusNotFound, unprefixed.Code)

	authorizePath := "/connectors/inventree/authorize"
	authorizeQuery := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {"chatgpt-state"},
		"resource":              {cfg.OAuthResourceURL},
		"scope":                 {tools.ScopeInventreeRead},
		"code_challenge":        {strings.Repeat("a", 43)},
		"code_challenge_method": {"S256"},
	}
	setup := request(authorizePath + "?" + authorizeQuery.Encode())
	r.Equal(http.StatusOK, setup.Code)
	a.Contains(setup.Body.String(), `action="/connectors/inventree/authorize"`)
	a.NotContains(setup.Body.String(), "internal.service")
	a.NotContains(setup.Body.String(), "attacker")
	r.Len(setup.Result().Cookies(), 1)
	a.Equal(authorizePath, setup.Result().Cookies()[0].Path)

	hiddenValue := func(name string) string {
		match := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]+)"`).FindStringSubmatch(setup.Body.String())
		r.Len(match, 2)
		return match[1]
	}
	cancelForm := url.Values{
		"setup_state": {hiddenValue("setup_state")},
		"client_id":   {clientID},
		"csrf":        {hiddenValue("csrf")},
		"cancel":      {"true"},
	}
	cancelReq := httptest.NewRequest(http.MethodPost, authorizePath, strings.NewReader(cancelForm.Encode()))
	cancelReq.RemoteAddr = "10.0.0.10:1234"
	cancelReq.Host = "internal.service:28686"
	cancelReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cancelReq.Header.Set("X-Forwarded-For", "203.0.113.20")
	cancelReq.Header.Set("X-Forwarded-Host", "attacker.example")
	cancelReq.Header.Set("X-Forwarded-Proto", "http")
	cancelReq.Header.Set("X-Forwarded-Prefix", "/attacker")
	for _, cookie := range setup.Result().Cookies() {
		cancelReq.AddCookie(cookie)
	}
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, cancelReq)
	r.Equal(http.StatusFound, cancelled.Code)
	location, err := url.Parse(cancelled.Header().Get("Location"))
	r.NoError(err)
	a.Equal("chatgpt.com", location.Host)
	a.Equal("access_denied", location.Query().Get("error"))
	a.Equal("chatgpt-state", location.Query().Get("state"))
	a.NotContains(location.String(), "internal.service")
	a.NotContains(location.String(), "attacker")

	tokenErrorReq := httptest.NewRequest(http.MethodPost, "/connectors/inventree/token", strings.NewReader(url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "resource": {cfg.OAuthResourceURL},
	}.Encode()))
	tokenErrorReq.RemoteAddr = "198.51.100.10:1234"
	tokenErrorReq.Host = "internal.service:28686"
	tokenErrorReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenErrorReq.Header.Set("X-Forwarded-Host", "attacker.example")
	tokenErrorReq.Header.Set("X-Forwarded-Proto", "http")
	tokenErrorReq.Header.Set("X-Forwarded-Prefix", "/attacker")
	tokenError := httptest.NewRecorder()
	handler.ServeHTTP(tokenError, tokenErrorReq)
	r.Equal(http.StatusUnauthorized, tokenError.Code)
	a.Equal("{\"error\":\"invalid_client\"}\n", tokenError.Body.String())
	a.NotContains(tokenError.Body.String(), "internal.service")
	a.NotContains(tokenError.Body.String(), "attacker")

	keyring, err := cfg.OAuthKeyring.Keyring()
	r.NoError(err)
	codec := oauth.EnvelopeCodec{Keyring: keyring}
	sealAccessToken := func(audience string) string {
		token, sealErr := codec.Seal(ctx, oauth.AssociatedData{
			Issuer: cfg.OAuthIssuerURL, Audience: audience, ClientID: clientID, Type: oauth.TokenTypeAccess,
		}, oauth.TokenClaims{
			Type: oauth.TokenTypeAccess, Issuer: cfg.OAuthIssuerURL, Audience: audience, Subject: "operator-1", ClientID: clientID,
			Scopes: []string{tools.ScopeInventreeRead}, IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(2 * time.Hour),
			Credential: oauth.Credential{Scheme: inventree.AuthSchemeToken, Token: "secret-inventree-token"},
		})
		r.NoError(sealErr)
		return token
	}
	validToken := sealAccessToken(cfg.OAuthResourceURL)
	validMCP := postMCPAtPathWithBearer(t, handler, cfg.Path, validToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusOK, validMCP.Code)
	wrongAudienceToken := sealAccessToken("https://public.example.test/connectors/other/mcp")
	wrongAudience := postMCPAtPathWithBearer(t, handler, cfg.Path, wrongAudienceToken, `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusUnauthorized, wrongAudience.Code)
}

func TestProductionHTTPMuxRejectsCanonicalRouteCollision(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	cfg := config.Config{
		Transport:        config.TransportHTTP,
		Environment:      config.EnvironmentProduction,
		Path:             "/authorize",
		OAuthIssuerURL:   "https://public.example.test",
		OAuthResourceURL: "https://public.example.test/authorize",
		OAuthKeyring: oauth.KeyringConfig{Keys: []oauth.KeyConfig{{
			ID:             "current",
			MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
			State:          oauth.KeyStateActive,
		}}},
	}

	_, err := HTTPMux(ctx, cfg, New(tools.Dependencies{}))
	r.ErrorContains(err, `production HTTP canonical paths collide at "/authorize"`)
}

func TestProductionHTTPMuxRateLimitsByTrustedResolvedSourceIP(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	cfg := config.Config{
		Transport:            config.TransportHTTP,
		Environment:          config.EnvironmentProduction,
		Path:                 "/mcp",
		OAuthIssuerURL:       "https://public.example.test",
		OAuthResourceURL:     "https://public.example.test/mcp",
		OAuthClientIDs:       []string{"https://chatgpt.com/client-metadata"},
		TrustedProxyCIDRs:    []string{"10.0.0.0/8"},
		OAuthAccessLifetime:  oauth.DefaultAccessTokenLifetime,
		OAuthRefreshLifetime: oauth.DefaultRefreshTokenLifetime,
		OAuthSessionLifetime: oauth.DefaultSessionLifetime,
		OAuthKeyring: oauth.KeyringConfig{Keys: []oauth.KeyConfig{{
			ID:             "current",
			MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
			State:          oauth.KeyStateActive,
		}}},
	}
	fixedNow := time.Date(2026, 8, 2, 5, 0, 0, 0, time.UTC)
	handler, err := httpMuxWithOptions(ctx, cfg, New(tools.Dependencies{}), nil, httpMuxOptions{
		now:                    func() time.Time { return fixedNow },
		authorizationRateLimit: 1,
	})
	r.NoError(err)
	request := func(remoteAddress string, forwardedFor string) int {
		req := httptest.NewRequest(http.MethodGet, "/authorize", nil)
		req.RemoteAddr = remoteAddress
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}

	r.Equal(http.StatusBadRequest, request("10.0.0.10:1234", "203.0.113.10"))
	r.Equal(http.StatusTooManyRequests, request("10.0.0.10:1234", "203.0.113.10"))
	r.Equal(http.StatusBadRequest, request("10.0.0.10:1234", "203.0.113.11"))

	r.Equal(http.StatusBadRequest, request("198.51.100.10:1234", "203.0.113.12"))
	r.Equal(http.StatusTooManyRequests, request("198.51.100.10:1234", "203.0.113.13"))

	r.Equal(http.StatusBadRequest, request("10.0.0.20:1234", "malformed, 203.0.113.14"))
	r.Equal(http.StatusBadRequest, request("10.0.0.20:1234", "malformed, 203.0.113.15"))
	r.Equal(http.StatusTooManyRequests, request("10.0.0.20:1234", "different-malformed-value, 203.0.113.15"))
}

func TestProductionHTTPAuthenticatesBeforeDebugTrafficCapture(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	cfg := config.Config{
		Transport:              config.TransportHTTP,
		Environment:            config.EnvironmentProduction,
		Path:                   "/mcp",
		MCPMaxRequestBodyBytes: config.DefaultMCPMaxRequestBodyBytes,
		OAuthIssuerURL:         "https://auth.example.test",
		OAuthResourceURL:       "https://mcp.example.test/mcp",
		OAuthClientIDs:         []string{"https://chatgpt.com/client-metadata"},
		OAuthKeyring: oauth.KeyringConfig{Keys: []oauth.KeyConfig{{
			ID:             "current",
			MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
			State:          oauth.KeyStateActive,
		}}},
	}
	var output strings.Builder
	handler, err := httpMux(ctx, cfg, New(tools.Dependencies{}), &trafficLog{w: &output})
	r.NoError(err)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"secret":"unauthenticated"}`)))

	r.Equal(http.StatusUnauthorized, recorder.Code)
	a.Empty(output.String())
}

func TestHTTPServerCancellationGracefullyDrainsActiveRequests(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	type contextKey string
	const key contextKey = "root"
	rootCtx, cancel := context.WithCancel(context.WithValue(t.Context(), key, "value"))
	requestStarted := make(chan bool, 1)
	releaseRequest := make(chan struct{})
	requestCanceled := make(chan bool, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestStarted <- req.Context().Value(key) == "value"
		select {
		case <-releaseRequest:
			requestCanceled <- false
			w.WriteHeader(http.StatusNoContent)
		case <-req.Context().Done():
			requestCanceled <- true
		}
	})
	cfg := config.Config{Listen: "127.0.0.1:0"}
	httpServer := newHTTPServer(rootCtx, cfg, handler)
	a.Equal(defaultHTTPReadHeaderTimeout, httpServer.ReadHeaderTimeout)
	a.Equal(defaultHTTPReadTimeout, httpServer.ReadTimeout)

	listener, err := net.Listen("tcp", cfg.Listen)
	r.NoError(err)
	notifier := &serverRecordingNotifier{}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serveHTTP(rootCtx, httpServer, listener, notifier)
	}()
	requestErr := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String()) //nolint:noctx // Server root-context cancellation is the behavior under test.
		if response != nil {
			_ = response.Body.Close()
		}
		requestErr <- err
	}()

	select {
	case rootValuePresent := <-requestStarted:
		r.True(rootValuePresent)
	case <-time.After(2 * time.Second):
		r.Fail("HTTP client request did not reach the server")
	}
	cancel()
	select {
	case err := <-serveErr:
		r.Fail("HTTP server stopped before the active request drained", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseRequest)
	r.False(<-requestCanceled)
	select {
	case err := <-serveErr:
		r.ErrorIs(err, context.Canceled)
	case <-time.After(2 * time.Second):
		r.Fail("HTTP server did not stop after context cancellation")
	}
	select {
	case err := <-requestErr:
		r.NoError(err)
	case <-time.After(2 * time.Second):
		r.Fail("HTTP client request did not complete after server cancellation")
	}
	a.Equal(int32(1), notifier.stoppingCalls.Load())
}

func TestServeHTTPContinuesAfterWatchdogFailureUntilSystemdTerminatesIt(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err)
	defer func() {
		_ = listener.Close()
	}()
	httpServer := newHTTPServer(ctx, config.Config{Listen: listener.Addr().String()}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	degraded := make(chan struct{}, 1)
	notifier := &serverRecordingNotifier{
		watchdogFailure: errors.New("heartbeat failed"),
		degraded:        degraded,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serveHTTP(ctx, httpServer, listener, notifier)
	}()

	select {
	case <-degraded:
	case <-time.After(time.Second):
		r.Fail("watchdog failure did not publish degraded status")
	}
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + listener.Addr().String()) //nolint:noctx // Process survival after a watchdog failure is under test.
	r.NoError(err)
	r.NoError(response.Body.Close())
	a.Equal(http.StatusNoContent, response.StatusCode)
	select {
	case err := <-serveErr:
		r.Fail("watchdog failure stopped the HTTP service", "error: %v", err)
	default:
	}

	cancel()
	select {
	case err := <-serveErr:
		r.ErrorIs(err, context.Canceled)
	case <-time.After(time.Second):
		r.Fail("HTTP service did not stop after cancellation")
	}
}

func TestServeHTTPFailsBeforeServingWhenReadyNotificationFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err)
	defer func() {
		_ = listener.Close()
	}()
	wantErr := errors.New("ready failed")
	notifier := &serverRecordingNotifier{readyErr: wantErr}
	httpServer := newHTTPServer(ctx, config.Config{Listen: listener.Addr().String()}, http.NotFoundHandler())

	err = serveHTTP(ctx, httpServer, listener, notifier)

	r.ErrorIs(err, wantErr)
	a.False(notifier.watchdogStarted.Load())
}

func TestRunHTTPDoesNotNotifyReadyWhenListenerBindFails(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err)
	defer func() {
		_ = listener.Close()
	}()
	notifier := &serverRecordingNotifier{}
	cfg := config.Config{
		Transport:              config.TransportHTTP,
		Environment:            config.EnvironmentDevelopment,
		Listen:                 listener.Addr().String(),
		Path:                   "/mcp",
		MCPMaxRequestBodyBytes: config.DefaultMCPMaxRequestBodyBytes,
	}

	err = RunHTTP(ctx, cfg, New(tools.Dependencies{}), nil, notifier)

	r.Error(err)
	a.Zero(notifier.readyCalls.Load())
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

func postMCPWithBearer(t *testing.T, handler http.Handler, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postMCPAtPathWithBearer(t, handler, "/mcp", token, body)
}

func postMCPAtPathWithBearer(t *testing.T, handler http.Handler, path string, token string, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	return recorder
}

func postMCPDiscover(t *testing.T, handler http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()

	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test-client","version":"v0.0.0"},"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Method", "server/discover")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	return recorder
}

func serverTokenVerifier(t *testing.T) auth.TokenVerifier {
	t.Helper()

	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		switch token {
		case "read-token":
			return &auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-1",
			}, nil
		case "write-token":
			return &auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead, tools.ScopeInventreeWrite},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-1",
			}, nil
		case "write-upload-token":
			return &auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead, tools.ScopeInventreeWrite, tools.ScopeInventreeUpload},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-1",
			}, nil
		case "credential-alpha":
			return oauth.TokenInfoWithCredential(&auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-alpha",
			}, oauth.Credential{Scheme: inventree.AuthSchemeToken, Token: "alpha"}), nil
		case "credential-beta":
			return oauth.TokenInfoWithCredential(&auth.TokenInfo{
				Scopes:     []string{tools.ScopeInventreeRead},
				Expiration: time.Now().Add(time.Hour),
				UserID:     "operator-beta",
			}, oauth.Credential{Scheme: inventree.AuthSchemeToken, Token: "beta"}), nil
		default:
			return nil, auth.ErrInvalidToken
		}
	}
}

type listedTool struct {
	Name string         `json:"name"`
	Meta map[string]any `json:"_meta,omitempty"`
}

func decodeListedTools(t *testing.T, payload []byte) map[string]listedTool {
	t.Helper()
	r := require.New(t)

	var response struct {
		Result struct {
			Tools []listedTool `json:"tools"`
		} `json:"result"`
	}
	r.NoError(json.Unmarshal(mcpJSONPayload(payload), &response))
	toolsByName := make(map[string]listedTool, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		toolsByName[tool.Name] = tool
	}
	return toolsByName
}

func mcpJSONPayload(payload []byte) []byte {
	for _, line := range strings.Split(string(payload), "\n") {
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			return []byte(after)
		}
	}
	return payload
}

func securitySchemeSummaries(raw any) []string {
	rawSchemes, ok := raw.([]any)
	if !ok {
		return nil
	}
	summaries := make([]string, 0, len(rawSchemes))
	for _, rawScheme := range rawSchemes {
		scheme, ok := rawScheme.(map[string]any)
		if !ok {
			continue
		}
		scopes := make([]string, 0)
		if rawScopes, ok := scheme["scopes"].([]any); ok {
			for _, rawScope := range rawScopes {
				if scope, ok := rawScope.(string); ok {
					scopes = append(scopes, scope)
				}
			}
		}
		summaries = append(summaries, scheme["type"].(string)+":"+strings.Join(scopes, " "))
	}
	return summaries
}

type serverLookupClient struct{}

func (serverLookupClient) SearchParts(_ context.Context, query inventree.SearchQuery) ([]inventree.Part, error) {
	return []inventree.Part{{
		PK:          10,
		Name:        query.Search + " resistor",
		Description: "test part",
		Active:      true,
	}}, nil
}

func (serverLookupClient) GetPartDetail(_ context.Context, id int) (inventree.PartDetail, error) {
	return inventree.PartDetail{PK: id, Name: "test part", Active: true}, nil
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

func TestTrafficLogMiddlewareCapturesHTTPBodies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var output strings.Builder
	traffic := &trafficLog{w: &output}
	handler := traffic.middleware(string(config.TransportHTTP), config.DefaultMCPMaxRequestBodyBytes, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		r.NoError(err)
		a.Equal(`{"method":"tools/list"}`, string(body))
		w.WriteHeader(http.StatusAccepted)
		_, err = w.Write([]byte(`{"ok":true}`))
		r.NoError(err)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp?debug=true", strings.NewReader(`{"method":"tools/list"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	r.Equal(http.StatusAccepted, recorder.Code)
	r.Equal(`{"ok":true}`, recorder.Body.String())

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	r.Len(lines, 2)
	var inbound trafficLogEntry
	var outbound trafficLogEntry
	r.NoError(json.Unmarshal([]byte(lines[0]), &inbound))
	r.NoError(json.Unmarshal([]byte(lines[1]), &outbound))
	a.Equal("inbound", inbound.Direction)
	a.Equal(http.MethodPost, inbound.Method)
	a.Equal("/mcp?debug=true", inbound.Path)
	a.Equal(`{"method":"tools/list"}`, inbound.Body)
	a.Equal("outbound", outbound.Direction)
	a.Equal(http.StatusAccepted, outbound.Status)
	a.Equal(`{"ok":true}`, outbound.Body)
}

func TestTrafficLogTreatsCompleteAuthorizedURLsAsSensitiveResponseCapture(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	const response = `{"status":"ok","record":{"pk":7,"web_url":"https://internal.inventory.example/part/7/","link":"https://supplier.example/item?token=sensitive#datasheet"}}`

	var output strings.Builder
	traffic := &trafficLog{w: &output}
	handler := traffic.middleware(string(config.TransportHTTP), config.DefaultMCPMaxRequestBodyBytes, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(response))
		r.NoError(err)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	r.Len(lines, 2)
	var outbound trafficLogEntry
	r.NoError(json.Unmarshal([]byte(lines[1]), &outbound))
	r.Equal(response, outbound.Body)
	r.Contains(outbound.Body, "internal.inventory.example")
	r.Contains(outbound.Body, "?token=sensitive#datasheet")
}

func TestTrafficLogMiddlewareRejectsUnreadableHTTPRequestBody(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var output strings.Builder
	traffic := &trafficLog{w: &output}
	called := false
	handler := traffic.middleware(string(config.TransportHTTP), config.DefaultMCPMaxRequestBodyBytes, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Body = errReadCloser{err: errors.New("read failed")}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	a.False(called)
	a.Equal(http.StatusBadRequest, recorder.Code)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	r.Len(lines, 2)
	var inbound trafficLogEntry
	r.NoError(json.Unmarshal([]byte(lines[0]), &inbound))
	a.Equal("inbound", inbound.Direction)
	a.Equal("read failed", inbound.Error)
}

func TestTrafficLogMiddlewareRejectsOversizedHTTPRequestBody(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var output strings.Builder
	traffic := &trafficLog{w: &output}
	handler := traffic.middleware(string(config.TransportHTTP), maxHTTPDebugBodyBytes, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be called for oversized debug traffic bodies")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(strings.Repeat("x", maxHTTPDebugBodyBytes+1)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	a.Equal(http.StatusRequestEntityTooLarge, recorder.Code)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	r.Len(lines, 2)
	var inbound trafficLogEntry
	r.NoError(json.Unmarshal([]byte(lines[0]), &inbound))
	a.True(inbound.BodyTruncated)
	a.Len(inbound.Body, maxHTTPDebugBodyBytes)
}

func TestTrafficLogMiddlewareForwardsRequestsLargerThanCaptureLimit(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	requestBody := strings.Repeat("x", maxHTTPDebugBodyBytes+1)
	var output strings.Builder
	traffic := &trafficLog{w: &output}
	handler := traffic.middleware(string(config.TransportHTTP), int64(len(requestBody)+1), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		r.NoError(err)
		a.Equal(requestBody, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(requestBody)))
	r.Equal(http.StatusNoContent, recorder.Code)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	r.Len(lines, 2)
	var inbound trafficLogEntry
	r.NoError(json.Unmarshal([]byte(lines[0]), &inbound))
	a.True(inbound.BodyTruncated)
	a.Len(inbound.Body, maxHTTPDebugBodyBytes)
}

func TestTrafficLogMiddlewareCapsHTTPResponseBodyAndStreamsSSEChunks(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var largeOutput strings.Builder
	largeTraffic := &trafficLog{w: &largeOutput}
	largeHandler := largeTraffic.middleware(string(config.TransportHTTP), config.DefaultMCPMaxRequestBodyBytes, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(strings.Repeat("y", maxHTTPDebugBodyBytes+1)))
		r.NoError(err)
	}))
	largeRecorder := httptest.NewRecorder()
	largeHandler.ServeHTTP(largeRecorder, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	largeLines := strings.Split(strings.TrimSpace(largeOutput.String()), "\n")
	r.Len(largeLines, 2)
	var largeOutbound trafficLogEntry
	r.NoError(json.Unmarshal([]byte(largeLines[1]), &largeOutbound))
	a.True(largeOutbound.BodyTruncated)
	a.Len(largeOutbound.Body, maxHTTPDebugBodyBytes)

	var streamOutput strings.Builder
	streamTraffic := &trafficLog{w: &streamOutput}
	streamHandler := streamTraffic.middleware(string(config.TransportHTTP), config.DefaultMCPMaxRequestBodyBytes, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte("event: message\n\n"))
		r.NoError(err)
	}))
	streamRecorder := httptest.NewRecorder()
	streamHandler.ServeHTTP(streamRecorder, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	streamLines := strings.Split(strings.TrimSpace(streamOutput.String()), "\n")
	r.Len(streamLines, 3)
	var chunk trafficLogEntry
	r.NoError(json.Unmarshal([]byte(streamLines[1]), &chunk))
	a.Equal("outbound_chunk", chunk.Direction)
	a.Equal("event: message\n\n", chunk.Body)
	var final trafficLogEntry
	r.NoError(json.Unmarshal([]byte(streamLines[2]), &final))
	a.Equal("outbound", final.Direction)
	a.Empty(final.Body)
}

func TestOpenTrafficLogRejectsUnsafeExistingFiles(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	dir := t.TempDir()
	worldReadable := filepath.Join(dir, "traffic.jsonl")
	r.NoError(os.WriteFile(worldReadable, nil, 0o644))
	_, _, err := openTrafficLog(worldReadable)
	r.Error(err)
	r.Contains(err.Error(), "permissions")

	target := filepath.Join(dir, "target.jsonl")
	r.NoError(os.WriteFile(target, nil, 0o600))
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	_, _, err = openTrafficLog(link)
	r.Error(err)
	r.Contains(err.Error(), "symlink")
}

type errReadCloser struct {
	err error
}

type serverRecordingNotifier struct {
	readyErr        error
	watchdogFailure error
	degraded        chan<- struct{}
	readyCalls      atomic.Int32
	watchdogStarted atomic.Bool
	stoppingCalls   atomic.Int32
}

func (*serverRecordingNotifier) Starting() error { return nil }
func (n *serverRecordingNotifier) Ready() error {
	n.readyCalls.Add(1)
	return n.readyErr
}
func (n *serverRecordingNotifier) RunWatchdog(ctx context.Context, onFailure func(error)) {
	n.watchdogStarted.Store(true)
	if n.watchdogFailure != nil {
		onFailure(n.watchdogFailure)
		return
	}
	<-ctx.Done()
}
func (n *serverRecordingNotifier) Degraded() error {
	if n.degraded != nil {
		n.degraded <- struct{}{}
	}
	return nil
}
func (n *serverRecordingNotifier) Stopping() error {
	n.stoppingCalls.Add(1)
	return nil
}
func (*serverRecordingNotifier) Fatal() error { return nil }

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (t bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func (r errReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r errReadCloser) Close() error {
	return nil
}
