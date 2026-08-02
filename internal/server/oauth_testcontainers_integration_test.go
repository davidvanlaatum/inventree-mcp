//go:build !no_integration_tests

package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPOAuthFlowAgainstInvenTreeContainer(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	ctx, _, _ := testhandler.SetupTestHandler(t)

	if testenv.SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree OAuth integration test excluded by %s or -short", testenv.EnvSkipDocker)
	}
	t.Parallel()

	opts := testenv.DefaultTestOptions(t)
	t.Logf("starting OAuth integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	shared, err := testenv.StartSharedInvenTree(ctx, opts)
	r.NoError(err)
	r.NotNil(shared)
	t.Cleanup(testenv.CleanupForTest(t, func() error {
		return shared.Close(context.WithoutCancel(ctx))
	}))

	run, err := shared.NewRun(t)
	r.NoError(err)
	account, err := shared.Account(ctx, run, testenv.AccountAdmin)
	r.NoError(err)
	part, err := shared.EnsureFixture(ctx, account, run, testenv.FixturePart)
	r.NoError(err)

	_, keyringConfig := testOAuthCodec(t)
	issuer := "https://mcp.example.test"
	audience := issuer + "/mcp"
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	var metadataURL string
	metadataServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/client-metadata":
			_ = json.NewEncoder(w).Encode(oauth.ClientMetadata{
				ClientID: metadataURL, RedirectURIs: []string{redirectURI},
				TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"}, JWKSURI: metadataServerURL(req) + "/jwks",
				GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			})
		case "/jwks":
			e := big.NewInt(int64(clientKey.PublicKey.E)).Bytes()
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "integration-key",
				"n": base64.RawURLEncoding.EncodeToString(clientKey.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e),
			}}})
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(metadataServer.Close)
	metadataURL = metadataServer.URL + "/client-metadata"

	pkceVerifier := "test-verifier-with-enough-entropy-for-oauth-flow"

	deps := tools.Dependencies{
		AuthorizationMode: tools.AuthorizationModeOAuth,
		ClientFromContext: OAuthClientFromContext(shared.Environment().BaseURL, nil),
	}
	httpConfig := config.Config{
		Transport:              config.TransportHTTP,
		Environment:            config.EnvironmentProduction,
		Path:                   "/mcp",
		InvenTreeURL:           shared.Environment().BaseURL,
		MCPMaxRequestBodyBytes: config.DefaultMCPMaxRequestBodyBytes,
		OAuthIssuerURL:         issuer,
		OAuthResourceURL:       audience,
		OAuthKeyring:           keyringConfig,
		OAuthClientIDs:         []string{metadataURL},
	}
	deps.ResourceMetadataURL = httpConfig.OAuthProtectedResourceMetadataURL()
	protected, err := httpMuxWithMetadataClient(ctx, httpConfig, New(deps), nil, metadataServer.Client())
	r.NoError(err)
	authorizePath := "/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {metadataURL}, "redirect_uri": {redirectURI}, "state": {"integration-state"},
		"resource": {audience}, "scope": {tools.ScopeInventreeRead}, "code_challenge": {oauth.PKCEChallengeS256(pkceVerifier)}, "code_challenge_method": {"S256"},
	}.Encode()
	setupPage := httptest.NewRecorder()
	protected.ServeHTTP(setupPage, httptest.NewRequest(http.MethodGet, authorizePath, nil))
	r.Equal(http.StatusOK, setupPage.Code)
	setupForm := url.Values{
		"setup_state": {oauthIntegrationHiddenValue(t, setupPage.Body.String(), "setup_state")},
		"client_id":   {metadataURL}, "csrf": {oauthIntegrationHiddenValue(t, setupPage.Body.String(), "csrf")},
		"credential_scheme": {"Token"}, "credential": {account.Token},
	}
	setupRequest := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(setupForm.Encode()))
	setupRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range setupPage.Result().Cookies() {
		setupRequest.AddCookie(cookie)
	}
	setupResult := httptest.NewRecorder()
	protected.ServeHTTP(setupResult, setupRequest)
	r.Equal(http.StatusFound, setupResult.Code)
	callback, err := url.Parse(setupResult.Header().Get("Location"))
	r.NoError(err)
	a.Equal("integration-state", callback.Query().Get("state"))
	code := callback.Query().Get("code")
	r.NotEmpty(code)

	tokenEndpoint := issuer + "/token"
	assertion := oauthIntegrationAssertion(t, clientKey, metadataURL, tokenEndpoint, "integration-code")
	tokenResult := oauthIntegrationPostForm(protected, "/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {metadataURL}, "client_assertion_type": {oauth.ClientAssertionTypeJWTBearer}, "client_assertion": {assertion},
		"code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {pkceVerifier}, "resource": {audience},
	})
	r.Equal(http.StatusOK, tokenResult.Code)
	var pair struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		CredentialSource string `json:"credential_source"`
	}
	r.NoError(json.Unmarshal(tokenResult.Body.Bytes(), &pair))
	r.NotEmpty(pair.AccessToken)
	r.NotEmpty(pair.RefreshToken)
	a.Equal(oauth.CredentialSourceDedicated, pair.CredentialSource)

	refreshResult := oauthIntegrationPostForm(protected, "/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {metadataURL}, "client_assertion_type": {oauth.ClientAssertionTypeJWTBearer},
		"client_assertion": {oauthIntegrationAssertion(t, clientKey, metadataURL, tokenEndpoint, "integration-refresh")},
		"refresh_token":    {pair.RefreshToken}, "resource": {audience},
	})
	r.Equal(http.StatusOK, refreshResult.Code)
	var refreshedPair struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		CredentialSource string `json:"credential_source"`
	}
	r.NoError(json.Unmarshal(refreshResult.Body.Bytes(), &refreshedPair))
	r.NotEmpty(refreshedPair.AccessToken)
	r.NotEmpty(refreshedPair.RefreshToken)
	a.Equal(oauth.CredentialSourceDedicated, refreshedPair.CredentialSource)

	recorder := postMCPWithBearer(t, protected, refreshedPair.AccessToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
	r.Equal(http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	a.Contains(body, `"status":"ok"`)
	a.Contains(body, part.Name)

	rawUpstreamRecorder := postMCPWithBearer(t, protected, account.Token, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
	r.Equal(http.StatusUnauthorized, rawUpstreamRecorder.Code)
	a.NotContains(rawUpstreamRecorder.Body.String(), part.Name)
}

func metadataServerURL(req *http.Request) string {
	return "https://" + req.Host
}

func oauthIntegrationHiddenValue(t *testing.T, body string, name string) string {
	t.Helper()
	match := regexp.MustCompile(fmt.Sprintf(`name="%s" value="([^"]+)"`, regexp.QuoteMeta(name))).FindStringSubmatch(body)
	require.Len(t, match, 2)
	return match[1]
}

func oauthIntegrationAssertion(t *testing.T, key *rsa.PrivateKey, clientID string, audience string, id string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer: clientID, Subject: clientID, Audience: jwt.ClaimStrings{audience}, ID: id,
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	})
	token.Header["kid"] = "integration-key"
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func oauthIntegrationPostForm(handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func testOAuthCodec(t *testing.T) (oauth.EnvelopeCodec, oauth.KeyringConfig) {
	t.Helper()

	keyringConfig := oauth.KeyringConfig{Keys: []oauth.KeyConfig{{
		ID:             "test-key",
		MaterialBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
		State:          oauth.KeyStateActive,
	}}}
	keyring, err := keyringConfig.Keyring()
	require.NoError(t, err)
	return oauth.EnvelopeCodec{Keyring: keyring}, keyringConfig
}
