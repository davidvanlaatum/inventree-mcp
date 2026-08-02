package oauth

import (
	"bytes"
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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateKeyJWTVerifierValidatesJWKSClaimsAndReplay(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	clock := fakeClock{now: now}
	var clientID string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jwks":
			_ = json.NewEncoder(w).Encode(testJWKS(&key.PublicKey, "key-1"))
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	clientID = server.URL + "/client"
	metadata := ClientMetadata{ClientID: clientID, JWKSURI: server.URL + "/jwks"}
	verifier := PrivateKeyJWTVerifier{HTTPClient: server.Client(), TokenEndpoint: "https://mcp.example.test/token", ReplayStore: NewAssertionReplayStore(8, clock.Now), Clock: clock}

	assertion := signedClientAssertion(t, key, "key-1", clientID, verifier.TokenEndpoint, "assertion-1", now)
	r.NoError(verifier.Verify(ctx, clientID, metadata, ClientAssertionTypeJWTBearer, assertion))
	r.ErrorIs(verifier.Verify(ctx, clientID, metadata, ClientAssertionTypeJWTBearer, assertion), ErrInvalidClientAssertion)
	restartedVerifier := verifier
	restartedVerifier.ReplayStore = NewAssertionReplayStore(8, clock.Now)
	r.NoError(restartedVerifier.Verify(ctx, clientID, metadata, ClientAssertionTypeJWTBearer, assertion))

	wrongAudience := signedClientAssertion(t, key, "key-1", clientID, "https://other.example/token", "assertion-2", now)
	r.ErrorIs(verifier.Verify(ctx, clientID, metadata, ClientAssertionTypeJWTBearer, wrongAudience), ErrInvalidClientAssertion)
	missingType := signedClientAssertion(t, key, "key-1", clientID, verifier.TokenEndpoint, "assertion-3", now)
	r.ErrorIs(verifier.Verify(ctx, clientID, metadata, "", missingType), ErrInvalidClientAssertion)
}

func TestPrivateKeyJWTVerifierRejectsInvalidClaimsKeysAndAlgorithms(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	var clientID string
	tokenEndpoint := "https://mcp.example.test/token"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jwks":
			_ = json.NewEncoder(w).Encode(testJWKS(&key.PublicKey, "key-1"))
		case "/weak":
			weak, genErr := rsa.GenerateKey(rand.Reader, 1024)
			r.NoError(genErr)
			_ = json.NewEncoder(w).Encode(testJWKS(&weak.PublicKey, "key-1"))
		case "/malformed-ec":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{"kty": "EC", "crv": "P-256", "kid": "key-1", "x": "AQ", "y": "AQ"}}})
		case "/oversize":
			_, _ = w.Write(bytes.Repeat([]byte("x"), defaultJWKSMaxBytes+1))
		case "/redirect":
			http.Redirect(w, req, "https://other.example.test/jwks", http.StatusTemporaryRedirect)
		case "/slow":
			<-req.Context().Done()
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	clientID = server.URL + "/client"
	baseClaims := func() jwt.RegisteredClaims {
		return jwt.RegisteredClaims{Issuer: clientID, Subject: clientID, Audience: jwt.ClaimStrings{tokenEndpoint}, ID: "assertion", IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}
	}
	tests := []struct {
		name   string
		key    *rsa.PrivateKey
		kid    string
		method jwt.SigningMethod
		claims func(jwt.RegisteredClaims) jwt.RegisteredClaims
	}{
		{name: "wrong signature", key: otherKey},
		{name: "missing key id", kid: " "},
		{name: "unknown key id", kid: "other"},
		{name: "wrong issuer", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims { c.Issuer = "other"; return c }},
		{name: "wrong subject", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims { c.Subject = "other"; return c }},
		{name: "missing expiry", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims { c.ExpiresAt = nil; return c }},
		{name: "expired", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims {
			c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
			return c
		}},
		{name: "expiry too far", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims {
			c.ExpiresAt = jwt.NewNumericDate(now.Add(10 * time.Minute))
			return c
		}},
		{name: "missing issued at", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims { c.IssuedAt = nil; return c }},
		{name: "issued too old", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims {
			c.IssuedAt = jwt.NewNumericDate(now.Add(-10 * time.Minute))
			return c
		}},
		{name: "issued in future", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims {
			c.IssuedAt = jwt.NewNumericDate(now.Add(time.Minute))
			return c
		}},
		{name: "missing jti", claims: func(c jwt.RegisteredClaims) jwt.RegisteredClaims { c.ID = ""; return c }},
		{name: "disallowed algorithm", method: jwt.SigningMethodRS384},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := baseClaims()
			claims.ID = fmt.Sprintf("assertion-%d", index)
			if test.claims != nil {
				claims = test.claims(claims)
			}
			signingKey := test.key
			if signingKey == nil {
				signingKey = key
			}
			method := test.method
			if method == nil {
				method = jwt.SigningMethodRS256
			}
			token := jwt.NewWithClaims(method, claims)
			var kid string
			switch test.kid {
			case "":
				kid = "key-1"
			case " ":
				kid = ""
			default:
				kid = test.kid
			}
			if kid != "" {
				token.Header["kid"] = kid
			}
			signed, signErr := token.SignedString(signingKey)
			r.NoError(signErr)
			verifier := PrivateKeyJWTVerifier{HTTPClient: server.Client(), TokenEndpoint: tokenEndpoint, ReplayStore: NewAssertionReplayStore(32, func() time.Time { return now }), Clock: fakeClock{now: now}}
			r.ErrorIs(verifier.Verify(ctx, clientID, ClientMetadata{JWKSURI: server.URL + "/jwks"}, ClientAssertionTypeJWTBearer, signed), ErrInvalidClientAssertion)
		})
	}

	valid := signedClientAssertion(t, key, "key-1", clientID, tokenEndpoint, "key-case", now)
	for _, path := range []string{"/weak", "/malformed-ec", "/oversize", "/redirect"} {
		verifier := PrivateKeyJWTVerifier{HTTPClient: server.Client(), TokenEndpoint: tokenEndpoint, ReplayStore: NewAssertionReplayStore(8, func() time.Time { return now }), Clock: fakeClock{now: now}}
		r.ErrorIs(verifier.Verify(ctx, clientID, ClientMetadata{JWKSURI: server.URL + path}, ClientAssertionTypeJWTBearer, valid), ErrInvalidClientAssertion, path)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	verifier := PrivateKeyJWTVerifier{HTTPClient: server.Client(), TokenEndpoint: tokenEndpoint, ReplayStore: NewAssertionReplayStore(8, func() time.Time { return now }), Clock: fakeClock{now: now}}
	r.ErrorIs(verifier.Verify(timeoutCtx, clientID, ClientMetadata{JWKSURI: server.URL + "/slow"}, ClientAssertionTypeJWTBearer, valid), ErrInvalidClientAssertion)
}

func TestAuthorizationServerPrivateKeyJWTSetupCodeAndRefreshFlow(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var clientID string
	clientServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/client":
			_ = json.NewEncoder(w).Encode(ClientMetadata{
				ClientID: clientID, RedirectURIs: []string{redirectURI},
				TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"}, JWKSURI: clientServerURL(req) + "/jwks",
				GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(testJWKS(&key.PublicKey, "key-1"))
		default:
			http.NotFound(w, req)
		}
	}))
	defer clientServer.Close()
	clientID = clientServer.URL + "/client"

	issuer := "https://mcp.example.test"
	resource := issuer + "/mcp"
	codec := testCodec(t)
	broker := &fakeCredentialBroker{subject: "inventree-user:7:operator", dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-secret"}}
	service := Service{
		Codec:           codec,
		MetadataFetcher: ClientMetadataFetcher{HTTPClient: clientServer.Client(), AllowedOrigins: []string{clientServer.URL}},
		CodeStore:       NewCodeStore(8, time.Now),
		CredentialValidator: CredentialValidatorFunc(func(_ context.Context, credential Credential) error {
			if credential.Token != "dedicated-secret" {
				return ErrInvalidUpstreamCredential
			}
			return nil
		}),
	}
	authServer := &AuthorizationServer{
		Issuer: issuer, Resource: resource, Scopes: []string{"inventree.read", "inventree.write"}, Service: service,
		MetadataFetcher: service.MetadataFetcher, CredentialBroker: broker,
		AssertionVerifier: PrivateKeyJWTVerifier{HTTPClient: clientServer.Client(), ReplayStore: NewAssertionReplayStore(16, time.Now)},
		RateLimiter:       NewRequestRateLimiter(20, time.Minute, time.Now),
	}
	mux := http.NewServeMux()
	r.NoError(authServer.Register(mux))
	verifier := "verifier-with-at-least-forty-three-characters-1234"
	challenge := PKCEChallengeS256(verifier)
	authorizeURL := "/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"chatgpt-state"},
		"resource": {resource}, "scope": {"inventree.read inventree.write"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}.Encode()
	for name, query := range map[string]url.Values{
		"wrong response type": {"response_type": {"token"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"}, "resource": {resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}},
		"missing state":       {"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "resource": {resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}},
		"wrong resource":      {"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"}, "resource": {"https://other.example/mcp"}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}},
		"unsupported scope":   {"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"}, "resource": {resource}, "scope": {"inventree.destroy"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}},
		"bad PKCE method":     {"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"}, "resource": {resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"plain"}},
		"bad PKCE challenge":  {"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"}, "resource": {resource}, "scope": {"inventree.read"}, "code_challenge": {"short"}, "code_challenge_method": {"S256"}},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/authorize?"+query.Encode(), nil))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
	begin := httptest.NewRecorder()
	mux.ServeHTTP(begin, httptest.NewRequest(http.MethodGet, authorizeURL, nil))
	r.Equal(http.StatusOK, begin.Code)
	a.Equal("no-store", begin.Header().Get("Cache-Control"))
	a.Contains(begin.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	a.NotContains(begin.Body.String(), "dedicated-secret")
	setupToken := hiddenValue(t, begin.Body.String(), "setup_state")
	csrf := hiddenValue(t, begin.Body.String(), "csrf")
	cookies := begin.Result().Cookies()
	r.NotEmpty(cookies)

	form := url.Values{"setup_state": {setupToken}, "client_id": {clientID}, "csrf": {csrf}, "credential_scheme": {"Token"}, "credential": {"supplied-secret"}}
	completeReq := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(form.Encode()))
	completeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	completeReq.AddCookie(cookies[0])
	complete := httptest.NewRecorder()
	mux.ServeHTTP(complete, completeReq)
	r.Equal(http.StatusFound, complete.Code)
	redirect, err := url.Parse(complete.Header().Get("Location"))
	r.NoError(err)
	a.Equal("chatgpt-state", redirect.Query().Get("state"))
	code := redirect.Query().Get("code")
	r.NotEmpty(code)
	a.Equal(1, broker.validateCalls)
	a.Equal(1, broker.createCalls)
	r.Len(broker.tokenNames, 1)
	a.Regexp(`^inventree-mcp-chatgpt-[A-Za-z0-9_-]{43}$`, broker.tokenNames[0])

	tokenEndpoint := issuer + "/token"
	assertion := signedClientAssertion(t, key, "key-1", clientID, tokenEndpoint, "token-code-1", time.Now())
	tokenForm := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "client_assertion_type": {ClientAssertionTypeJWTBearer}, "client_assertion": {assertion},
		"code": {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}, "resource": {resource},
	}
	token := postForm(mux, "/token", tokenForm)
	r.Equal(http.StatusOK, token.Code)
	var pair map[string]any
	r.NoError(json.Unmarshal(token.Body.Bytes(), &pair))
	a.NotEmpty(pair["access_token"])
	a.NotEmpty(pair["refresh_token"])
	a.Equal(CredentialSourceDedicated, pair["credential_source"])

	reusedCode := postForm(mux, "/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "client_assertion_type": {ClientAssertionTypeJWTBearer},
		"client_assertion": {signedClientAssertion(t, key, "key-1", clientID, tokenEndpoint, "token-code-replay", time.Now())},
		"code":             {code}, "redirect_uri": {redirectURI}, "code_verifier": {verifier}, "resource": {resource},
	})
	r.Equal(http.StatusBadRequest, reusedCode.Code)

	wrongResource := postForm(mux, "/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "client_assertion_type": {ClientAssertionTypeJWTBearer},
		"client_assertion": {signedClientAssertion(t, key, "key-1", clientID, tokenEndpoint, "token-wrong-resource", time.Now())},
		"refresh_token":    {pair["refresh_token"].(string)}, "resource": {"https://other.example/mcp"},
	})
	r.Equal(http.StatusBadRequest, wrongResource.Code)

	unsupportedGrant := postForm(mux, "/token", url.Values{
		"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_assertion_type": {ClientAssertionTypeJWTBearer},
		"client_assertion": {signedClientAssertion(t, key, "key-1", clientID, tokenEndpoint, "token-unsupported-grant", time.Now())},
		"resource":         {resource},
	})
	r.Equal(http.StatusBadRequest, unsupportedGrant.Code)

	refreshAssertion := signedClientAssertion(t, key, "key-1", clientID, tokenEndpoint, "token-refresh-1", time.Now())
	refresh := postForm(mux, "/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "client_assertion_type": {ClientAssertionTypeJWTBearer}, "client_assertion": {refreshAssertion},
		"refresh_token": {pair["refresh_token"].(string)}, "resource": {resource},
	})
	r.Equal(http.StatusOK, refresh.Code)

	replayed := postForm(mux, "/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "client_assertion_type": {ClientAssertionTypeJWTBearer}, "client_assertion": {refreshAssertion},
		"refresh_token": {pair["refresh_token"].(string)}, "resource": {resource},
	})
	r.Equal(http.StatusUnauthorized, replayed.Code)
}

func TestAuthorizationServerRequiresExplicitSuppliedCredentialFallback(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	_ = ctx
	r := require.New(t)
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var clientID string
	clientServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(ClientMetadata{ClientID: clientID, RedirectURIs: []string{redirectURI}, TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"}, JWKSURI: clientServerURL(req) + "/jwks"})
	}))
	defer clientServer.Close()
	clientID = clientServer.URL + "/client"
	broker := &fakeCredentialBroker{subject: "inventree-user:7:operator", createErr: ErrDedicatedTokenUnavailable}
	service := Service{Codec: testCodec(t), MetadataFetcher: ClientMetadataFetcher{HTTPClient: clientServer.Client(), AllowedOrigins: []string{clientServer.URL}}, CodeStore: NewCodeStore(8, time.Now)}
	authServer := &AuthorizationServer{Issuer: "https://mcp.example.test", Resource: "https://mcp.example.test/mcp", Scopes: []string{"inventree.read"}, Service: service, MetadataFetcher: service.MetadataFetcher, CredentialBroker: broker}
	mux := http.NewServeMux()
	r.NoError(authServer.Register(mux))
	challenge := PKCEChallengeS256("verifier-with-at-least-forty-three-characters-1234")
	begin := httptest.NewRecorder()
	mux.ServeHTTP(begin, httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"}, "resource": {authServer.Resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}.Encode(), nil))
	r.Equal(http.StatusOK, begin.Code)

	first := submitSetup(mux, begin, clientID, "supplied-secret", false)
	r.Equal(http.StatusOK, first.Code)
	r.Contains(first.Body.String(), "explicitly choose")
	r.NotContains(first.Body.String(), "supplied-secret")
	replayedSetup := submitSetup(mux, begin, clientID, "supplied-secret", false)
	r.Equal(http.StatusBadRequest, replayedSetup.Code)

	second := submitSetup(mux, first, clientID, "supplied-secret", true)
	r.Equal(http.StatusFound, second.Code)
	r.Equal(2, broker.validateCalls)
	r.Equal(1, broker.createCalls)

	broker.validateErr = ErrInvalidUpstreamCredential
	invalidBegin := httptest.NewRecorder()
	mux.ServeHTTP(invalidBegin, httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state-2"}, "resource": {authServer.Resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}.Encode(), nil))
	r.Equal(http.StatusOK, invalidBegin.Code)
	invalid := submitSetup(mux, invalidBegin, clientID, "must-not-echo", false)
	r.Equal(http.StatusOK, invalid.Code)
	r.Contains(invalid.Body.String(), "could not be validated")
	r.NotContains(invalid.Body.String(), "must-not-echo")
}

func TestAuthorizationServerCancellationReturnsAccessDeniedWithoutUsingCredential(t *testing.T) {
	r := require.New(t)
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var clientID string
	clientServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ClientMetadata{
			ClientID: clientID, RedirectURIs: []string{redirectURI},
			TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"}, JWKSURI: clientID + "/jwks",
		})
	}))
	defer clientServer.Close()
	clientID = clientServer.URL + "/client"
	broker := &fakeCredentialBroker{}
	service := Service{Codec: testCodec(t), MetadataFetcher: ClientMetadataFetcher{HTTPClient: clientServer.Client(), AllowedOrigins: []string{clientServer.URL}}, CodeStore: NewCodeStore(8, time.Now)}
	authServer := &AuthorizationServer{Issuer: "https://mcp.example.test", Resource: "https://mcp.example.test/mcp", Scopes: []string{"inventree.read"}, Service: service, MetadataFetcher: service.MetadataFetcher, CredentialBroker: broker}
	mux := http.NewServeMux()
	r.NoError(authServer.Register(mux))
	challenge := PKCEChallengeS256("verifier-with-at-least-forty-three-characters-1234")
	begin := httptest.NewRecorder()
	mux.ServeHTTP(begin, httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"},
		"resource": {authServer.Resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}.Encode(), nil))
	r.Equal(http.StatusOK, begin.Code)

	form := url.Values{
		"setup_state": {hiddenValue(t, begin.Body.String(), "setup_state")}, "client_id": {clientID},
		"csrf": {hiddenValue(t, begin.Body.String(), "csrf")}, "cancel": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range begin.Result().Cookies() {
		req.AddCookie(cookie)
	}
	cancelled := httptest.NewRecorder()
	mux.ServeHTTP(cancelled, req)
	r.Equal(http.StatusFound, cancelled.Code)
	redirect, err := url.Parse(cancelled.Header().Get("Location"))
	r.NoError(err)
	r.Equal("access_denied", redirect.Query().Get("error"))
	r.Equal("state", redirect.Query().Get("state"))
	r.Zero(broker.validateCalls)
	r.Zero(broker.createCalls)
}

func TestAuthorizationServerEnforcesSetupRequestSecurityExpiryTimeoutAndRateLimit(t *testing.T) {
	r := require.New(t)
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var clientID string
	clientServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(ClientMetadata{
			ClientID: clientID, RedirectURIs: []string{redirectURI},
			TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"}, JWKSURI: clientServerURL(req) + "/jwks",
		})
	}))
	defer clientServer.Close()
	clientID = clientServer.URL + "/client"
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	broker := &fakeCredentialBroker{subject: "inventree-user:7:operator", dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated"}}
	service := Service{Codec: testCodec(t), Clock: clock, MetadataFetcher: ClientMetadataFetcher{HTTPClient: clientServer.Client(), AllowedOrigins: []string{clientServer.URL}}, CodeStore: NewCodeStore(8, clock.Now)}
	authServer := &AuthorizationServer{
		Issuer: "https://mcp.example.test", Resource: "https://mcp.example.test/mcp", Scopes: []string{"inventree.read"},
		Service: service, MetadataFetcher: service.MetadataFetcher, CredentialBroker: broker, Clock: clock, SetupTimeout: time.Millisecond,
	}
	mux := http.NewServeMux()
	r.NoError(authServer.Register(mux))
	challenge := PKCEChallengeS256("verifier-with-at-least-forty-three-characters-1234")
	authorizeURL := "/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"state"},
		"resource": {authServer.Resource}, "scope": {"inventree.read"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}.Encode()
	begin := httptest.NewRecorder()
	mux.ServeHTTP(begin, httptest.NewRequest(http.MethodGet, authorizeURL, nil))
	r.Equal(http.StatusOK, begin.Code)

	unsupportedMedia := httptest.NewRecorder()
	mux.ServeHTTP(unsupportedMedia, httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader("not-form")))
	r.Equal(http.StatusUnsupportedMediaType, unsupportedMedia.Code)
	oversize := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(strings.Repeat("x", setupRequestMaxBytes+1)))
	oversize.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oversizeRecorder := httptest.NewRecorder()
	mux.ServeHTTP(oversizeRecorder, oversize)
	r.Equal(http.StatusBadRequest, oversizeRecorder.Code)
	tokenUnsupportedMedia := httptest.NewRecorder()
	mux.ServeHTTP(tokenUnsupportedMedia, httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("not-form")))
	r.Equal(http.StatusUnsupportedMediaType, tokenUnsupportedMedia.Code)
	oversizeToken := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(strings.Repeat("x", setupRequestMaxBytes+1)))
	oversizeToken.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oversizeTokenRecorder := httptest.NewRecorder()
	mux.ServeHTTP(oversizeTokenRecorder, oversizeToken)
	r.Equal(http.StatusBadRequest, oversizeTokenRecorder.Code)

	badCSRFForm := url.Values{
		"setup_state": {hiddenValue(t, begin.Body.String(), "setup_state")}, "client_id": {clientID}, "csrf": {"wrong"},
		"credential_scheme": {"Token"}, "credential": {"secret"},
	}
	badCSRFRequest := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(badCSRFForm.Encode()))
	badCSRFRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range begin.Result().Cookies() {
		badCSRFRequest.AddCookie(cookie)
	}
	badCSRF := httptest.NewRecorder()
	mux.ServeHTTP(badCSRF, badCSRFRequest)
	r.Equal(http.StatusForbidden, badCSRF.Code)

	clock.now = now.Add(DefaultCodeLifetime + time.Second)
	expired := submitSetup(mux, begin, clientID, "secret", false)
	r.Equal(http.StatusBadRequest, expired.Code)

	clock.now = now
	broker.validateFunc = func(ctx context.Context, _ Credential) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	timeoutBegin := httptest.NewRecorder()
	mux.ServeHTTP(timeoutBegin, httptest.NewRequest(http.MethodGet, authorizeURL, nil))
	r.Equal(http.StatusOK, timeoutBegin.Code)
	timedOut := submitSetup(mux, timeoutBegin, clientID, "secret", false)
	r.Equal(http.StatusOK, timedOut.Code)
	r.Contains(timedOut.Body.String(), "could not be validated")

	limitedServer := *authServer
	limitedServer.RateLimiter = NewRequestRateLimiter(1, time.Minute, clock.Now)
	limitedServer.SetupStore = nil
	limitedMux := http.NewServeMux()
	r.NoError(limitedServer.Register(limitedMux))
	first := httptest.NewRecorder()
	limitedMux.ServeHTTP(first, httptest.NewRequest(http.MethodGet, authorizeURL, nil))
	r.Equal(http.StatusOK, first.Code)
	limitedSetup := submitSetup(limitedMux, first, clientID, "secret", false)
	r.Equal(http.StatusTooManyRequests, limitedSetup.Code)
	r.Equal("60", limitedSetup.Header().Get("Retry-After"))
	second := httptest.NewRecorder()
	limitedMux.ServeHTTP(second, httptest.NewRequest(http.MethodGet, authorizeURL, nil))
	r.Equal(http.StatusTooManyRequests, second.Code)
	r.Equal("60", second.Header().Get("Retry-After"))
	firstToken := httptest.NewRecorder()
	limitedMux.ServeHTTP(firstToken, httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("not-form")))
	r.Equal(http.StatusUnsupportedMediaType, firstToken.Code)
	secondToken := httptest.NewRecorder()
	limitedMux.ServeHTTP(secondToken, httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("not-form")))
	r.Equal(http.StatusTooManyRequests, secondToken.Code)
	r.Equal("60", secondToken.Header().Get("Retry-After"))
}

func TestRequestRateLimiterBoundsAttemptsAndWindow(t *testing.T) {
	now := time.Date(2026, 8, 2, 4, 0, 0, 0, time.UTC)
	limiter := NewRequestRateLimiter(1, time.Minute, func() time.Time { return now })
	a := assert.New(t)
	a.True(limiter.Allow("client"))
	a.False(limiter.Allow("client"))
	now = now.Add(time.Minute)
	a.True(limiter.Allow("client"))

	bounded := NewRequestRateLimiter(1, time.Minute, func() time.Time { return now })
	for index := range 1024 {
		a.True(bounded.Allow(fmt.Sprintf("client-%d", index)))
	}
	a.False(bounded.Allow("at-capacity"))
	now = now.Add(time.Minute)
	a.True(bounded.Allow("after-expiry"))
}

func TestInvenTreeCredentialBrokerValidatesAndCreatesDedicatedToken(t *testing.T) {
	t.Parallel()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		a.Equal("Token supplied-secret", req.Header.Get("Authorization"))
		switch req.URL.Path {
		case "/api/user/me/":
			_ = json.NewEncoder(w).Encode(inventree.CurrentUser{PK: 7, Username: "operator"})
		case "/api/user/me/token/":
			a.Equal("inventree-mcp-chatgpt-setup-id", req.URL.Query().Get("name"))
			_ = json.NewEncoder(w).Encode(inventree.UserToken{Name: "inventree-mcp-chatgpt-setup-id", Token: "dedicated-secret"})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	broker := InvenTreeCredentialBroker{BaseURL: server.URL, HTTPClient: server.Client()}
	supplied := Credential{Scheme: inventree.AuthSchemeToken, Token: "supplied-secret"}
	subject, err := broker.ValidateCredential(ctx, supplied)
	r.NoError(err)
	a.Equal("inventree-user:7:operator", subject)
	dedicated, err := broker.CreateDedicatedCredential(ctx, supplied, "inventree-mcp-chatgpt-setup-id")
	r.NoError(err)
	a.Equal(inventree.AuthSchemeToken, dedicated.Scheme)
	a.Equal("dedicated-secret", dedicated.Token)
}

type fakeCredentialBroker struct {
	subject       string
	dedicated     Credential
	validateErr   error
	createErr     error
	validateCalls int
	createCalls   int
	validateFunc  func(context.Context, Credential) (string, error)
	tokenNames    []string
}

func (b *fakeCredentialBroker) ValidateCredential(ctx context.Context, credential Credential) (string, error) {
	b.validateCalls++
	if b.validateFunc != nil {
		return b.validateFunc(ctx, credential)
	}
	return b.subject, b.validateErr
}

func (b *fakeCredentialBroker) CreateDedicatedCredential(_ context.Context, _ Credential, tokenName string) (Credential, error) {
	b.createCalls++
	b.tokenNames = append(b.tokenNames, tokenName)
	return b.dedicated, b.createErr
}

func signedClientAssertion(t *testing.T, key *rsa.PrivateKey, kid string, clientID string, audience string, id string, now time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Issuer: clientID, Subject: clientID, Audience: jwt.ClaimStrings{audience}, ID: id,
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func testJWKS(key *rsa.PublicKey, kid string) map[string]any {
	e := big.NewInt(int64(key.E)).Bytes()
	return map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e),
	}}}
}

func clientServerURL(req *http.Request) string {
	return "https://" + req.Host
}

func hiddenValue(t *testing.T, body string, name string) string {
	t.Helper()
	match := regexp.MustCompile(fmt.Sprintf(`name="%s" value="([^"]+)"`, regexp.QuoteMeta(name))).FindStringSubmatch(body)
	require.Len(t, match, 2)
	return match[1]
}

func submitSetup(handler http.Handler, page *httptest.ResponseRecorder, clientID string, credential string, fallback bool) *httptest.ResponseRecorder {
	form := url.Values{
		"setup_state": {hiddenValueFromBody(page.Body.String(), "setup_state")}, "client_id": {clientID}, "csrf": {hiddenValueFromBody(page.Body.String(), "csrf")},
		"credential_scheme": {"Token"}, "credential": {credential},
	}
	if fallback {
		form.Set("use_supplied_credential", "true")
	}
	req := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range page.Result().Cookies() {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func hiddenValueFromBody(body string, name string) string {
	match := regexp.MustCompile(fmt.Sprintf(`name="%s" value="([^"]+)"`, regexp.QuoteMeta(name))).FindStringSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func postForm(handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
