//go:build !no_integration_tests

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/testenv"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/auth"
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

	codec := testOAuthCodec(t)
	issuer := "https://mcp.example.test"
	audience := issuer + "/mcp"
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var metadataURL string
	metadataServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID:                metadataURL,
			RedirectURIs:            []string{redirectURI},
			TokenEndpointAuthMethod: "none",
		})
	}))
	t.Cleanup(metadataServer.Close)
	metadataURL = metadataServer.URL + "/client-metadata"

	service := oauth.Service{
		Codec: codec,
		MetadataFetcher: oauth.ClientMetadataFetcher{
			HTTPClient:     metadataServer.Client(),
			AllowedOrigins: []string{metadataServer.URL},
		},
		CodeStore: oauth.NewCodeStore(8, time.Now),
	}
	pkceVerifier := "test-verifier-with-enough-entropy-for-oauth-flow"
	code, err := service.IssueAuthorizationCode(ctx, oauth.AuthorizationRequest{
		Issuer:        issuer,
		Audience:      audience,
		Subject:       account.Username,
		ClientID:      metadataURL,
		RedirectURI:   redirectURI,
		PKCEChallenge: oauth.PKCEChallengeS256(pkceVerifier),
		Scopes:        []string{tools.ScopeInventreeRead},
		Credential: oauth.Credential{
			Scheme: inventree.AuthSchemeToken,
			Token:  account.Token,
		},
	})
	r.NoError(err)

	pair, err := service.ExchangeAuthorizationCode(ctx, code, oauth.AssociatedData{
		Issuer:   issuer,
		Audience: audience,
		ClientID: metadataURL,
	}, redirectURI, pkceVerifier)
	r.NoError(err)
	r.NotEmpty(pair.AccessToken)
	r.NotEmpty(pair.RefreshToken)

	deps := tools.Dependencies{
		AuthorizationMode:   tools.AuthorizationModeOAuth,
		ResourceMetadataURL: issuer + "/.well-known/oauth-protected-resource",
		ClientFromContext:   OAuthClientFromContext(shared.Environment().BaseURL, nil),
	}
	protected := auth.RequireBearerToken(oauthEnvelopeTokenVerifier(codec, issuer, audience, metadataURL), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: deps.ResourceMetadataURL,
	})(HTTPHandler(ctx, New(deps)))

	recorder := postMCPWithBearer(t, protected, pair.AccessToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
	r.Equal(http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	a.Contains(body, `"status":"ok"`)
	a.Contains(body, part.Name)

	missingScopePair := issueOAuthTestPair(t, ctx, service, issuer, audience, metadataURL, redirectURI, account, nil)
	missingScopeRecorder := postMCPWithBearer(t, protected, missingScopePair.AccessToken, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
	r.Equal(http.StatusOK, missingScopeRecorder.Code)
	a.Contains(missingScopeRecorder.Body.String(), `"isError":true`)
	a.Contains(missingScopeRecorder.Body.String(), `insufficient_scope`)
	a.NotContains(missingScopeRecorder.Body.String(), part.Name)

	rawUpstreamRecorder := postMCPWithBearer(t, protected, account.Token, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_parts","arguments":{"search":"`+part.Name+`"}}}`)
	r.Equal(http.StatusUnauthorized, rawUpstreamRecorder.Code)
	a.NotContains(rawUpstreamRecorder.Body.String(), part.Name)
}

func testOAuthCodec(t *testing.T) oauth.EnvelopeCodec {
	t.Helper()

	keyring, err := oauth.NewKeyring([]oauth.Key{{
		ID:       "test-key",
		Material: []byte("0123456789abcdef0123456789abcdef"),
		State:    oauth.KeyStateActive,
	}})
	require.NoError(t, err)
	return oauth.EnvelopeCodec{Keyring: keyring}
}

func oauthEnvelopeTokenVerifier(codec oauth.EnvelopeCodec, issuer string, audience string, clientID string) auth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		var claims oauth.TokenClaims
		if err := codec.Open(token, oauth.AssociatedData{
			Issuer:   issuer,
			Audience: audience,
			ClientID: clientID,
			Type:     oauth.TokenTypeAccess,
		}, &claims); err != nil {
			return nil, auth.ErrInvalidToken
		}
		if claims.Type != oauth.TokenTypeAccess ||
			claims.Issuer != issuer ||
			claims.Audience != audience ||
			claims.ClientID != clientID ||
			!claims.ExpiresAt.After(time.Now()) ||
			(!claims.SessionExpiresAt.IsZero() && !claims.SessionExpiresAt.After(time.Now())) {
			return nil, auth.ErrInvalidToken
		}
		if err := claims.Credential.Validate(); err != nil {
			return nil, auth.ErrInvalidToken
		}
		return oauth.TokenInfoWithCredential(&auth.TokenInfo{
			Scopes:     append([]string(nil), claims.Scopes...),
			Expiration: claims.ExpiresAt,
			UserID:     claims.Subject,
		}, claims.Credential), nil
	}
}

func issueOAuthTestPair(
	t *testing.T,
	ctx context.Context,
	service oauth.Service,
	issuer string,
	audience string,
	clientID string,
	redirectURI string,
	account *testenv.Account,
	scopes []string,
) oauth.TokenPair {
	t.Helper()
	r := require.New(t)

	pkceVerifier := "test-verifier-" + strings.ToLower(account.Username)
	code, err := service.IssueAuthorizationCode(ctx, oauth.AuthorizationRequest{
		Issuer:        issuer,
		Audience:      audience,
		Subject:       account.Username,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		PKCEChallenge: oauth.PKCEChallengeS256(pkceVerifier),
		Scopes:        scopes,
		Credential: oauth.Credential{
			Scheme: inventree.AuthSchemeToken,
			Token:  account.Token,
		},
	})
	r.NoError(err)
	pair, err := service.ExchangeAuthorizationCode(ctx, code, oauth.AssociatedData{
		Issuer:   issuer,
		Audience: audience,
		ClientID: clientID,
	}, redirectURI, pkceVerifier)
	r.NoError(err)
	return pair
}
