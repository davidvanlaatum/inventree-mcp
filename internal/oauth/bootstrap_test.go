package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sequentialIDGenerator struct {
	ids []string
	i   int
}

func (g *sequentialIDGenerator) NewID(context.Context) (string, error) {
	if g.i >= len(g.ids) {
		return "", errors.New("sequentialIDGenerator exhausted")
	}
	id := g.ids[g.i]
	g.i++
	return id, nil
}

func testBootstrapServer(t *testing.T, broker CredentialBroker, now time.Time) *BootstrapServer {
	t.Helper()
	return &BootstrapServer{
		Issuer:           "https://mcp.example.test",
		Resource:         "https://mcp.example.test/mcp",
		Codec:            testCodec(t),
		CredentialBroker: broker,
		Scopes:           []string{"inventree.read"},
		Clock:            fakeClock{now: now},
	}
}

func decodeBootstrapResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	return body
}

func TestBootstrapHandlerSealsEnvelopeForTokenCredential(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)

	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"},
	}
	server := testBootstrapServer(t, broker, now)
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Token supplied-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)
	body := decodeBootstrapResponse(t, rec)
	a.Equal("Bearer", body["token_type"])
	mcpToken, ok := body["mcp_token"].(string)
	r.True(ok)
	a.NotEmpty(mcpToken)

	var claims TokenClaims
	r.NoError(server.Codec.Open(mcpToken, AssociatedData{Issuer: server.Issuer, Audience: server.Resource, ClientID: "", Type: TokenTypeBootstrapAccess}, &claims))
	a.Equal("inventree-user:1:operator", claims.Subject)
	a.Equal("dedicated-token", claims.Credential.Token)
	a.Equal(inventree.AuthSchemeToken, claims.Credential.Scheme)
	a.Equal(CredentialSourceDedicated, claims.CredentialSource)
	a.Equal(server.Scopes, claims.Scopes)
	a.Equal(1, broker.createCalls)
	r.Len(broker.tokenNames, 1)
	a.Contains(broker.tokenNames[0], "inventree-mcp-static-")
}

func TestBootstrapHandlerSealsEnvelopeForBasicCredential(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)

	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	var receivedScheme inventree.AuthScheme
	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"},
		validateFunc: func(_ context.Context, credential Credential) (string, error) {
			receivedScheme = credential.Scheme
			return "inventree-user:1:operator", nil
		},
	}
	server := testBootstrapServer(t, broker, now)
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	encoded := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Basic "+encoded)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)
	a.Equal(inventree.AuthSchemeBasic, receivedScheme)
}

func TestBootstrapHandlerRejectsInvalidCredential(t *testing.T) {
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)

	broker := &fakeCredentialBroker{validateErr: ErrInvalidUpstreamCredential}
	server := testBootstrapServer(t, broker, time.Now())
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Token bad-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusUnauthorized, rec.Code)
	r.NotContains(rec.Body.String(), "bad-token")
	r.Equal(0, broker.createCalls)
	assertNoLogContains(t, handler, "bad-token")
}

func TestBootstrapHandlerRejectsInvalidBasicCredentialWithoutDisclosure(t *testing.T) {
	ctx, handler, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)

	broker := &fakeCredentialBroker{validateErr: ErrInvalidUpstreamCredential}
	server := testBootstrapServer(t, broker, time.Now())
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	encoded := base64.StdEncoding.EncodeToString([]byte("user:wrong-password"))
	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Basic "+encoded)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusUnauthorized, rec.Code)
	r.NotContains(rec.Body.String(), encoded)
	r.NotContains(rec.Body.String(), "user:wrong-password")
	assertNoLogContains(t, handler, encoded)
	assertNoLogContains(t, handler, "wrong-password")
}

func assertNoLogContains(t *testing.T, handler *testhandler.TestHandler, needle string) {
	t.Helper()
	for _, record := range handler.Logs() {
		require.NotContains(t, record.String(), needle)
	}
}

func TestBootstrapHandlerFailsClosedWhenDedicatedTokenCreationFails(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)

	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		createErr: ErrDedicatedTokenUnavailable,
	}
	server := testBootstrapServer(t, broker, time.Now())
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Token supplied-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusBadGateway, rec.Code)
	r.NotContains(rec.Body.String(), "supplied-token")
	r.NotContains(rec.Body.String(), "mcp_token")
	// The retry budget must actually be bounded: a broker that always fails
	// must be called exactly maxDedicatedTokenAttempts times, not retried
	// indefinitely.
	a.Equal(maxDedicatedTokenAttempts, broker.createCalls)
}

// TestBootstrapHandlerRejectsMisbehavingBrokerResult proves the fail-closed
// guarantee structurally rather than by response-body inspection: even if a
// CredentialBroker implementation violated its contract and returned the
// caller-supplied credential (or any non-Token-scheme credential) as the
// "dedicated" result, the handler's own ValidateForEnvelope guard refuses to
// seal it. A substring check on the JSON response cannot detect this class
// of bug, because a sealed credential is AES-GCM ciphertext, not plaintext.
func TestBootstrapHandlerRejectsMisbehavingBrokerResult(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)

	supplied := Credential{Scheme: inventree.AuthSchemeBasic, Token: base64.StdEncoding.EncodeToString([]byte("user:pass"))}
	broker := &fakeCredentialBroker{
		subject: "inventree-user:1:operator",
		createFunc: func(_ context.Context, credential Credential, _ string) (Credential, error) {
			// A misbehaving broker returns the caller's own supplied credential.
			return credential, nil
		},
	}
	server := testBootstrapServer(t, broker, time.Now())
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Basic "+supplied.Token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusBadGateway, rec.Code)
	r.NotContains(rec.Body.String(), "mcp_token")
}

func TestBootstrapHandlerRetriesDedicatedTokenCreationOnNameCollision(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)

	attempts := 0
	broker := &fakeCredentialBroker{
		subject: "inventree-user:1:operator",
		createFunc: func(_ context.Context, _ Credential, _ string) (Credential, error) {
			attempts++
			if attempts < 2 {
				return Credential{}, ErrDedicatedTokenUnavailable
			}
			return Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"}, nil
		},
	}
	server := testBootstrapServer(t, broker, time.Now())
	server.IDGenerator = &sequentialIDGenerator{ids: []string{"name-1", "name-2", "name-3"}}
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Token supplied-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)
	a.Equal(2, attempts)
	r.Len(broker.tokenNames, 2)
	a.NotEqual(broker.tokenNames[0], broker.tokenNames[1])
}

func TestBootstrapHandlerRejectsMalformedAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "missing", header: ""},
		{name: "no scheme separator", header: "supplied-token"},
		{name: "unknown scheme", header: "Digest supplied-token"},
		{name: "oversized", header: "Token " + strings.Repeat("a", maxBootstrapAuthHeaderBytes)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)

			broker := &fakeCredentialBroker{subject: "inventree-user:1:operator"}
			server := testBootstrapServer(t, broker, time.Now())
			mux := http.NewServeMux()
			r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

			req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			r.Equal(http.StatusBadRequest, rec.Code)
			r.Equal(0, broker.validateCalls)
		})
	}
}

func TestBootstrapHandlerRejectsNonPOST(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)

	server := testBootstrapServer(t, &fakeCredentialBroker{}, time.Now())
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodGet, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusMethodNotAllowed, rec.Code)
}

func TestBootstrapHandlerRateLimited(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)

	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"},
	}
	server := testBootstrapServer(t, broker, time.Now())
	server.RateLimiter = NewRequestRateLimiter(1, time.Minute, time.Now)
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
		req.Header.Set("Authorization", "Token supplied-token")
		req.RemoteAddr = "203.0.113.5:1234"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if i == 0 {
			r.Equal(http.StatusOK, rec.Code)
		} else {
			r.Equal(http.StatusTooManyRequests, rec.Code)
		}
	}
}

func TestBootstrapHandlerRateLimitsByCredentialAcrossDifferentIPs(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)

	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"},
	}
	server := testBootstrapServer(t, broker, time.Now())
	server.RateLimiter = NewRequestRateLimiter(1, time.Minute, time.Now)
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	// A distributed attacker spreading guesses of the SAME credential across
	// different source IPs must still be bounded: the second attempt, from a
	// different IP, is still rejected because it is rate-limited by a hash
	// of the credential itself, not only by source IP.
	for i, remoteAddr := range []string{"203.0.113.5:1234", "198.51.100.9:5678"} {
		req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
		req.Header.Set("Authorization", "Token supplied-token")
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if i == 0 {
			r.Equal(http.StatusOK, rec.Code)
		} else {
			r.Equal(http.StatusTooManyRequests, rec.Code)
		}
	}
}

func TestBootstrapHandlerResponseIsNotCacheable(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)

	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"},
	}
	server := testBootstrapServer(t, broker, time.Now())
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Token supplied-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)
	a.Equal("no-store", rec.Header().Get("Cache-Control"))
	a.Equal("DENY", rec.Header().Get("X-Frame-Options"))
}

func TestBootstrapEnvelopeExpiryHonoursConfiguredLifetime(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)

	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	broker := &fakeCredentialBroker{
		subject:   "inventree-user:1:operator",
		dedicated: Credential{Scheme: inventree.AuthSchemeToken, Token: "dedicated-token"},
	}
	server := testBootstrapServer(t, broker, now)
	server.EnvelopeLifetime = 48 * time.Hour
	mux := http.NewServeMux()
	r.NoError(server.Register(mux, "/mcp/auth/bootstrap"))

	req := httptest.NewRequest(http.MethodPost, "/mcp/auth/bootstrap", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Token supplied-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	r.Equal(http.StatusOK, rec.Code)
	body := decodeBootstrapResponse(t, rec)
	mcpToken, ok := body["mcp_token"].(string)
	r.True(ok)
	var claims TokenClaims
	r.NoError(server.Codec.Open(mcpToken, AssociatedData{Issuer: server.Issuer, Audience: server.Resource, ClientID: "", Type: TokenTypeBootstrapAccess}, &claims))
	a.Equal(now.Add(48*time.Hour), claims.ExpiresAt)
}

func TestParseBootstrapAuthorizationDispatchesByScheme(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	credential, err := parseBootstrapAuthorization("Bearer some-token")
	a.NoError(err)
	a.Equal(inventree.AuthSchemeBearer, credential.Scheme)
	a.Equal("some-token", credential.Token)

	_, err = parseBootstrapAuthorization("")
	a.ErrorIs(err, errInvalidBootstrapAuthorization)
}

func TestParseBootstrapAuthorizationSchemeIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	for _, header := range []string{"bearer some-token", "BEARER some-token", "BeArEr some-token"} {
		credential, err := parseBootstrapAuthorization(header)
		a.NoError(err, header)
		a.Equal(inventree.AuthSchemeBearer, credential.Scheme, header)
		a.Equal("some-token", credential.Token, header)
	}
}
