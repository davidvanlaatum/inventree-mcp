package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	})(HTTPHandler(ctx, New(deps)))

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
	})(HTTPHandler(ctx, New(deps)))

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
	})(HTTPHandler(ctx, srv))

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
