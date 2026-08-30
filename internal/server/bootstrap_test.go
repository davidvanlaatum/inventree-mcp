package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeBootstrapInvenTreeServer(t *testing.T) *httptest.Server {
	t.Helper()
	var tokenCounter int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/user/me/":
			if req.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"pk":1,"username":"operator","email":"operator@example.test"}`))
		case "/api/user/me/token/":
			tokenCounter++
			name := req.URL.Query().Get("name")
			_, _ = w.Write([]byte(`{"token":"dedicated-secret-` + strconv.Itoa(tokenCounter) + `","name":"` + name + `","expiry":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func bootstrapTestConfig(t *testing.T, inventreeURL string) config.Config {
	t.Helper()
	return config.Config{
		Transport:                 config.TransportHTTP,
		Environment:               config.EnvironmentProduction,
		Path:                      "/mcp",
		InvenTreeURL:              inventreeURL,
		InvenTreeTimeout:          5 * time.Second,
		OAuthIssuerURL:            "https://auth.example.test",
		OAuthResourceURL:          "https://mcp.example.test/mcp",
		OAuthClientIDs:            []string{"https://chatgpt.com/client-metadata"},
		OAuthKeyring:              oauth.KeyringConfig{Keys: []oauth.KeyConfig{{ID: "current", MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY", State: oauth.KeyStateActive}}},
		BootstrapEnabled:          true,
		BootstrapEnvelopeLifetime: 24 * time.Hour,
	}
}

func TestHTTPMuxRegistersBootstrapRouteOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	inventreeServer := fakeBootstrapInvenTreeServer(t)
	defer inventreeServer.Close()

	enabledCfg := bootstrapTestConfig(t, inventreeServer.URL)
	handler, err := HTTPMux(ctx, enabledCfg, New(tools.Dependencies{}))
	r.NoError(err)
	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil)
	req.Header.Set("Authorization", "Token supplied-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	r.Equal(http.StatusOK, rec.Code)

	disabledCfg := enabledCfg
	disabledCfg.BootstrapEnabled = false
	disabledHandler, err := HTTPMux(ctx, disabledCfg, New(tools.Dependencies{}))
	r.NoError(err)
	disabledReq := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil)
	disabledReq.Header.Set("Authorization", "Token supplied-token")
	disabledRec := httptest.NewRecorder()
	disabledHandler.ServeHTTP(disabledRec, disabledReq)
	r.Equal(http.StatusNotFound, disabledRec.Code)
}

func TestBootstrapEndToEndThroughMuxMintsUsableEnvelope(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	inventreeServer := fakeBootstrapInvenTreeServer(t)
	defer inventreeServer.Close()

	cfg := bootstrapTestConfig(t, inventreeServer.URL)
	deps := tools.Dependencies{
		EnableWriteTools:    true,
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
		ClientFromContext:   OAuthClientFromContext(inventreeServer.URL, inventreeServer.Client()),
	}
	handler, err := HTTPMux(ctx, cfg, New(deps))
	r.NoError(err)

	bootstrapReq := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil)
	bootstrapReq.Header.Set("Authorization", "Token supplied-token")
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrapReq)
	r.Equal(http.StatusOK, bootstrapRec.Code)

	var bootstrapBody map[string]any
	r.NoError(json.NewDecoder(bootstrapRec.Body).Decode(&bootstrapBody))
	mcpToken, ok := bootstrapBody["mcp_token"].(string)
	r.True(ok)
	a.NotEmpty(mcpToken)

	listRecorder := postMCPWithBearer(t, handler, mcpToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusOK, listRecorder.Code)
	listedTools := decodeListedTools(t, listRecorder.Body.Bytes())
	a.NotNil(listedTools[tools.CreatePartToolName])

	rawUpstream := postMCPWithBearer(t, handler, "supplied-token", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	r.Equal(http.StatusUnauthorized, rawUpstream.Code)
	a.NotContains(rawUpstream.Body.String(), tools.CreatePartToolName)
}

func TestBootstrapRejectsInvalidCredentialThroughMux(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	inventreeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer inventreeServer.Close()

	cfg := bootstrapTestConfig(t, inventreeServer.URL)
	handler, err := HTTPMux(ctx, cfg, New(tools.Dependencies{}))
	r.NoError(err)

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil)
	req.Header.Set("Authorization", "Token bad-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	r.Equal(http.StatusUnauthorized, rec.Code)
	r.NotContains(rec.Body.String(), "bad-token")
}

func TestBootstrapMuxRateLimitsByTrustedResolvedSourceIP(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	inventreeServer := fakeBootstrapInvenTreeServer(t)
	defer inventreeServer.Close()

	cfg := bootstrapTestConfig(t, inventreeServer.URL)
	cfg.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	fixedNow := time.Date(2026, 8, 2, 5, 0, 0, 0, time.UTC)
	handler, err := httpMuxWithOptions(ctx, cfg, New(tools.Dependencies{}), nil, httpMuxOptions{
		now:                func() time.Time { return fixedNow },
		bootstrapRateLimit: 1,
	})
	r.NoError(err)

	request := func(remoteAddress string, forwardedFor string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil)
		req.Header.Set("Authorization", "Token supplied-token")
		req.RemoteAddr = remoteAddress
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}

	// Both requests resolve to the same trusted-proxy-forwarded source IP
	// (requestctx.SourceIP), so the second is rate-limited even though the
	// immediate peer address differs from the forwarded client address.
	r.Equal(http.StatusOK, request("10.0.0.10:1234", "203.0.113.10"))
	r.Equal(http.StatusTooManyRequests, request("10.0.0.10:1234", "203.0.113.10"))
}

// TestBootstrapEnvelopeGrantsWriteScope proves the operator-confirmed "full
// scope set" decision actually reaches enforcement: a bootstrap envelope
// must authorize a write-scoped tool call, not just read-only ones (scope
// enforcement happens at call time, not at tools/list time).
func TestBootstrapEnvelopeGrantsWriteScope(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ctx, _, _ := testhandler.SetupTestHandler(t)
	inventreeServer := fakeBootstrapInvenTreeServer(t)
	defer inventreeServer.Close()

	cfg := bootstrapTestConfig(t, inventreeServer.URL)
	deps := tools.Dependencies{
		EnableWriteTools:    true,
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
		ClientFromContext:   OAuthClientFromContext(inventreeServer.URL, inventreeServer.Client()),
	}
	handler, err := HTTPMux(ctx, cfg, New(deps))
	r.NoError(err)

	bootstrapReq := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil)
	bootstrapReq.Header.Set("Authorization", "Token supplied-token")
	bootstrapRec := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRec, bootstrapReq)
	r.Equal(http.StatusOK, bootstrapRec.Code)
	var bootstrapBody map[string]any
	r.NoError(json.NewDecoder(bootstrapRec.Body).Decode(&bootstrapBody))
	mcpToken, ok := bootstrapBody["mcp_token"].(string)
	r.True(ok)

	callRecorder := postMCPWithBearer(t, handler, mcpToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_part","arguments":{"name":"10k resistor","category_id":20}}}`)
	r.Equal(http.StatusOK, callRecorder.Code)
	r.NotContains(callRecorder.Body.String(), "insufficient_scope")
}
