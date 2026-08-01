package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
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
		serverDone <- New(tools.Dependencies{}).Run(ctx, loggingTransport{
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
	}

	a.Contains(methods, "inbound:server/discover")
	a.Contains(methods, "inbound:tools/list")
	a.NotContains(methods, "inbound:initialize")
	a.NotContains(methods, "inbound:notifications/initialized")
	a.Positive(outboundCount)
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
	a.Contains(deniedRecorder.Body.String(), `scope=\"inventree.write\"`)
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

	deps := tools.Dependencies{
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
		ClientFromContext:   OAuthClientFromContext(upstream.URL, upstream.Client()),
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
			recorder := postMCPWithBearer(t, protected, tt.token, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"10k"}}}`)
			r.Equal(http.StatusOK, recorder.Code)
			a.Contains(recorder.Body.String(), `"status":"ok"`)
			a.Contains(recorder.Body.String(), tt.wantHeader)
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
	a.Equal([]string{"oauth2:inventree.write"}, securitySchemeSummaries(listedTools[tools.CreatePartToolName].Meta[tools.MetaSecuritySchemesKey]))

	deniedRecorder := postMCPWithBearer(t, handler, accessToken, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_part","arguments":{"name":"10k resistor","category_id":20}}}`)
	r.Equal(http.StatusOK, deniedRecorder.Code)
	a.Contains(deniedRecorder.Body.String(), `"isError":true`)
	a.Contains(deniedRecorder.Body.String(), `insufficient_scope`)
	a.Contains(deniedRecorder.Body.String(), `scope=\"inventree.write\"`)
	a.Equal(int32(0), clientCalls.Load())

	rawUpstreamRecorder := postMCPWithBearer(t, handler, "secret-inventree-token", `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusUnauthorized, rawUpstreamRecorder.Code)
	a.NotContains(rawUpstreamRecorder.Body.String(), tools.CreatePartToolName)
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

	listener, err := net.Listen("tcp", cfg.Listen)
	r.NoError(err)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serveHTTP(rootCtx, httpServer, listener)
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

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
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

func (serverLookupClient) GetPart(_ context.Context, id int) (inventree.Part, error) {
	return inventree.Part{PK: id, Name: "test part", Active: true}, nil
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
