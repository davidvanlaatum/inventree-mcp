//go:build !no_integration_tests

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPBootstrapFlowAgainstInvenTreeContainer exercises F-S93's success
// path against a real InvenTree instance for both credential forms: Token
// and Basic. Each form proves the minted MCP bearer envelope works for a
// real tool call, and that the originally supplied credential remains
// independently usable afterward (bootstrap mints a dedicated token; it
// never rotates or consumes the credential the caller supplied).
func TestHTTPBootstrapFlowAgainstInvenTreeContainer(t *testing.T) {
	r := require.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	if testenv.SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree bootstrap integration test excluded by %s or -short", testenv.EnvSkipDocker)
	}
	t.Parallel()

	opts := testenv.DefaultTestOptions(t)
	t.Logf("starting bootstrap integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := testenv.StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(testenv.CleanupForTest(t, func() error {
		return shared.Close(context.WithoutCancel(ctx))
	}))

	_, keyringConfig := testOAuthCodec(t)
	mcpPath := "/mcp"
	bootstrapPath := mcpPath + "/auth/bootstrap"
	httpConfig := config.Config{
		Transport:                 config.TransportHTTP,
		Environment:               config.EnvironmentProduction,
		Path:                      mcpPath,
		InvenTreeURL:              shared.Environment().BaseURL,
		InvenTreeTimeout:          30 * time.Second,
		MCPMaxRequestBodyBytes:    config.DefaultMCPMaxRequestBodyBytes,
		OAuthIssuerURL:            "https://mcp.example.test",
		OAuthResourceURL:          "https://mcp.example.test" + mcpPath,
		OAuthKeyring:              keyringConfig,
		BootstrapEnabled:          true,
		BootstrapEnvelopeLifetime: 24 * time.Hour,
	}
	deps := tools.Dependencies{
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ClientFromContext:   OAuthClientFromContext(shared.Environment().BaseURL, nil),
		ResourceMetadataURL: httpConfig.OAuthProtectedResourceMetadataURL(),
	}
	protected, err := httpMuxWithMetadataClient(ctx, httpConfig, New(deps), nil, nil)
	r.NoError(err)

	t.Run("token credential", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		run, err := shared.NewRun(t)
		r.NoError(err)
		account, err := shared.Account(ctx, run, testenv.AccountAdmin)
		r.NoError(err)
		part, err := shared.EnsureFixture(ctx, account, run, testenv.FixturePart)
		r.NoError(err)

		bootstrapRec := bootstrapIntegrationPost(protected, bootstrapPath, "Token "+account.Token)
		r.Equal(http.StatusOK, bootstrapRec.Code)
		mcpToken := bootstrapIntegrationMCPToken(t, bootstrapRec)

		toolRec := oauthIntegrationPostMCP(protected, mcpPath, mcpToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
		r.Equal(http.StatusOK, toolRec.Code)
		a.Contains(toolRec.Body.String(), part.Name)

		client, err := shared.Client(account)
		r.NoError(err)
		user, err := client.GetCurrentUser(ctx)
		r.NoError(err)
		a.Equal(account.Username, user.Username)
	})

	t.Run("basic credential", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		a := assert.New(t)
		run, err := shared.NewRun(t)
		r.NoError(err)
		account, err := shared.Account(ctx, run, testenv.AccountAdmin)
		r.NoError(err)
		r.NotEmpty(account.Password)
		part, err := shared.EnsureFixture(ctx, account, run, testenv.FixturePart)
		r.NoError(err)

		encoded := base64.StdEncoding.EncodeToString([]byte(account.Username + ":" + account.Password))
		bootstrapRec := bootstrapIntegrationPost(protected, bootstrapPath, "Basic "+encoded)
		r.Equal(http.StatusOK, bootstrapRec.Code)
		mcpToken := bootstrapIntegrationMCPToken(t, bootstrapRec)

		toolRec := oauthIntegrationPostMCP(protected, mcpPath, mcpToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
		r.Equal(http.StatusOK, toolRec.Code)
		a.Contains(toolRec.Body.String(), part.Name)

		basicClient, err := inventree.NewClient(inventree.Config{
			BaseURL:    shared.Environment().BaseURL,
			Credential: inventree.Credential{Scheme: inventree.AuthSchemeBasic, Token: encoded},
		})
		r.NoError(err)
		user, err := basicClient.GetCurrentUser(ctx)
		r.NoError(err)
		a.Equal(account.Username, user.Username)
	})
}

func bootstrapIntegrationPost(handler http.Handler, path string, authorization string) *httptest.ResponseRecorder {
	req := oauthIntegrationProxyRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", authorization)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func bootstrapIntegrationMCPToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		MCPToken string `json:"mcp_token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body.MCPToken)
	return body.MCPToken
}
