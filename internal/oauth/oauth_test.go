package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientMetadataFetcherValidatesCIMDAndRedirect(t *testing.T) {
	t.Run("accepts matching HTTPS metadata and strips credentials on redirect", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		r := require.New(t)
		a := assert.New(t)
		redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
		var metadataPath string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			a.Empty(req.Header.Get("Authorization"))
			a.Empty(req.Header.Get("Cookie"))
			switch req.URL.Path {
			case "/metadata":
				http.Redirect(w, req, metadataPath+"/redirected", http.StatusTemporaryRedirect)
			case "/metadata/redirected":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"client_id":                  metadataPath,
					"redirect_uris":              []string{redirectURI},
					"token_endpoint_auth_method": "none",
					"grant_types":                []string{"authorization_code"},
					"response_types":             []string{"code"},
					"future_extension":           "allowed",
				})
			default:
				http.NotFound(w, req)
			}
		}))
		defer server.Close()
		metadataPath = server.URL + "/metadata"
		jar, err := cookiejar.New(nil)
		r.NoError(err)
		serverURL, err := url.Parse(server.URL)
		r.NoError(err)
		jar.SetCookies(serverURL, []*http.Cookie{{Name: "session", Value: "must-not-forward"}})
		client := server.Client()
		client.Jar = jar

		fetcher := ClientMetadataFetcher{
			HTTPClient:     client,
			AllowedOrigins: []string{server.URL},
		}
		metadata, err := fetcher.FetchAndValidate(ctx, metadataPath, redirectURI)
		r.NoError(err)
		a.Equal(metadataPath, metadata.ClientID)
	})

	t.Run("rejects bad client_id and metadata mismatches before authorization", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		r := require.New(t)
		redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Query().Get("case") {
			case "wrong_redirect":
				_ = json.NewEncoder(w).Encode(ClientMetadata{
					RedirectURIs:            []string{"https://chatgpt.com/connector/oauth/other"},
					TokenEndpointAuthMethod: "none",
				})
			case "mismatch":
				_ = json.NewEncoder(w).Encode(ClientMetadata{
					ClientID:                "https://chatgpt.com/.well-known/client-metadata",
					RedirectURIs:            []string{redirectURI},
					TokenEndpointAuthMethod: "none",
				})
			default:
				http.Error(w, "no metadata", http.StatusBadGateway)
			}
		}))
		defer server.Close()
		fetcher := ClientMetadataFetcher{
			HTTPClient:     server.Client(),
			AllowedOrigins: []string{server.URL},
		}

		_, err := fetcher.FetchAndValidate(ctx, "http://chatgpt.com/metadata", redirectURI)
		r.ErrorIs(err, ErrInvalidClientMetadata)
		_, err = (ClientMetadataFetcher{HTTPClient: server.Client()}).FetchAndValidate(ctx, server.URL+"/metadata", redirectURI)
		r.ErrorIs(err, ErrInvalidClientMetadata)
		_, err = fetcher.FetchAndValidate(ctx, server.URL+"/metadata?case=wrong_redirect", redirectURI)
		r.ErrorIs(err, ErrInvalidClientMetadata)
		_, err = fetcher.FetchAndValidate(ctx, server.URL+"/metadata?case=mismatch", redirectURI)
		r.ErrorIs(err, ErrInvalidClientMetadata)
		_, err = fetcher.FetchAndValidate(ctx, server.URL+"/metadata?case=fetch_failure", redirectURI)
		r.ErrorIs(err, ErrInvalidClientMetadata)
	})
}

func TestClientMetadataFetcherRejectsOversizeTimeoutAndUnsafeRedirects(t *testing.T) {
	t.Run("bounded read", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		r := require.New(t)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"redirect_uris":["https://chatgpt.com/connector/oauth/callback_123"],"token_endpoint_auth_method":"none"}`))
		}))
		defer server.Close()
		_, err := (ClientMetadataFetcher{
			HTTPClient:     server.Client(),
			AllowedOrigins: []string{server.URL},
			MaxBytes:       8,
		}).FetchAndValidate(ctx, server.URL+"/metadata", "https://chatgpt.com/connector/oauth/callback_123")
		r.ErrorIs(err, ErrInvalidClientMetadata)
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		r := require.New(t)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(50 * time.Millisecond)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()
		_, err := (ClientMetadataFetcher{
			HTTPClient:     server.Client(),
			AllowedOrigins: []string{server.URL},
			Timeout:        time.Nanosecond,
		}).FetchAndValidate(ctx, server.URL+"/metadata", "https://chatgpt.com/connector/oauth/callback_123")
		r.ErrorIs(err, ErrInvalidClientMetadata)
	})

	t.Run("cross origin and scheme downgrade redirects", func(t *testing.T) {
		t.Parallel()
		ctx, _, _ := testhandler.SetupTestHandler(t)
		r := require.New(t)
		other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer other.Close()
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Query().Get("to") == "http" {
				http.Redirect(w, req, "http://example.com/metadata", http.StatusFound)
				return
			}
			http.Redirect(w, req, other.URL+"/metadata", http.StatusFound)
		}))
		defer server.Close()
		fetcher := ClientMetadataFetcher{HTTPClient: server.Client(), AllowedOrigins: []string{server.URL}}
		_, err := fetcher.FetchAndValidate(ctx, server.URL+"/metadata", "https://chatgpt.com/connector/oauth/callback_123")
		r.ErrorIs(err, ErrInvalidClientMetadata)
		_, err = fetcher.FetchAndValidate(ctx, server.URL+"/metadata?to=http", "https://chatgpt.com/connector/oauth/callback_123")
		r.ErrorIs(err, ErrInvalidClientMetadata)
	})
}

func TestEnvelopeCodecOpaqueTokensAndKeyringValidation(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	key := bytesOf('k', 32)
	keyring, err := NewKeyring([]Key{{ID: "active", Material: key, State: KeyStateActive}})
	r.NoError(err)
	codec := EnvelopeCodec{Keyring: keyring, Random: counterRandom{}}
	aad := AssociatedData{Issuer: "https://mcp.example.com", Audience: "https://mcp.example.com/mcp", ClientID: "https://chatgpt.com/client-metadata", Type: TokenTypeAccess}

	token, err := codec.Seal(ctx, aad, TokenClaims{
		Type:      TokenTypeAccess,
		Issuer:    aad.Issuer,
		Audience:  aad.Audience,
		Subject:   "user-1",
		ClientID:  aad.ClientID,
		ExpiresAt: time.Now().Add(time.Minute),
		Credential: Credential{
			Scheme: inventree.AuthSchemeToken,
			Token:  "secret-upstream-token",
		},
	})
	r.NoError(err)
	a.True(strings.HasPrefix(token, "mcp1.active."))
	a.NotContains(token, "secret-upstream-token")
	a.False(strings.HasPrefix(token, "eyJ"))
	payload := strings.TrimPrefix(token, "mcp1.active.")
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	r.NoError(err)
	a.NotContains(string(raw), "secret-upstream-token")

	var opened TokenClaims
	r.NoError(codec.Open(token, aad, &opened))
	a.Equal("user-1", opened.Subject)
	a.Equal("secret-upstream-token", opened.Credential.Token)
	a.ErrorIs(codec.Open(token, AssociatedData{
		Issuer:   aad.Issuer,
		Audience: "https://other.example.com/mcp",
		ClientID: aad.ClientID,
		Type:     aad.Type,
	}, &opened), ErrInvalidToken)

	_, err = NewKeyring([]Key{{ID: "a", Material: key, State: KeyStateActive}, {ID: "b", Material: key, State: KeyStateActive}})
	r.ErrorContains(err, "exactly one active")
	_, err = NewKeyring([]Key{{ID: "short", Material: []byte("too-short"), State: KeyStateActive}})
	r.ErrorContains(err, "32 bytes")
	_, err = NewKeyring([]Key{{ID: "a", Material: key, State: KeyStateDecryptOnly}})
	r.ErrorContains(err, "exactly one active")
}

func TestEnvelopeCodecOpensDecryptOnlyKeyAndSealsWithActiveKey(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	oldKey := Key{ID: "old", Material: bytesOf('o', 32), State: KeyStateActive}
	oldKeyring, err := NewKeyring([]Key{oldKey})
	r.NoError(err)
	aad := AssociatedData{Issuer: "https://mcp.example.com", Audience: "https://mcp.example.com/mcp", ClientID: "https://chatgpt.com/client-metadata", Type: TokenTypeAccess}
	oldToken, err := (EnvelopeCodec{Keyring: oldKeyring, Random: counterRandom{}}).Seal(ctx, aad, TokenClaims{
		Type:      TokenTypeAccess,
		Issuer:    aad.Issuer,
		Audience:  aad.Audience,
		Subject:   "user-1",
		ClientID:  aad.ClientID,
		ExpiresAt: time.Now().Add(time.Minute),
		Credential: Credential{
			Scheme: inventree.AuthSchemeToken,
			Token:  "secret-upstream-token",
		},
	})
	r.NoError(err)
	rotatedKeyring, err := NewKeyring([]Key{
		{ID: "old", Material: oldKey.Material, State: KeyStateDecryptOnly},
		{ID: "new", Material: bytesOf('n', 32), State: KeyStateActive},
	})
	r.NoError(err)
	rotatedCodec := EnvelopeCodec{Keyring: rotatedKeyring, Random: counterRandom{}}
	var opened TokenClaims
	r.NoError(rotatedCodec.Open(oldToken, aad, &opened))
	newToken, err := rotatedCodec.Seal(ctx, aad, opened)
	r.NoError(err)
	a.True(strings.HasPrefix(newToken, "mcp1.new."))
}

func TestKeyringConfigDecodesBase64Material(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	material := bytesOf('c', 32)
	keyring, err := (KeyringConfig{Keys: []KeyConfig{{
		ID:             "configured",
		MaterialBase64: base64.RawStdEncoding.EncodeToString(material),
		State:          KeyStateActive,
	}}}).Keyring()
	r.NoError(err)
	a.Equal("configured", keyring.active.ID)

	_, err = (KeyringConfig{Keys: []KeyConfig{{
		ID:             "bad",
		MaterialBase64: "not base64",
		State:          KeyStateActive,
	}}}).Keyring()
	r.ErrorContains(err, "base64")
}

func TestOAuthSensitiveLogKeysAreRedacted(t *testing.T) {
	a := assert.New(t)
	for _, key := range []string{"code", "access_token", "refresh_token", "token", "authorization", "state"} {
		attr := platform.RedactLogAttr(nil, slog.String(key, "sensitive-value"))
		a.Equal("[REDACTED]", attr.Value.String(), key)
	}
	a.Equal("visible", platform.RedactLogAttr(nil, slog.String("client_id", "visible")).Value.String())
}

func TestServiceIssuesOneTimeCodeAndDefaultTokenLifetimes(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	clock := fakeClock{now: now}
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var metadataURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ClientMetadata{
			ClientID:                metadataURL,
			RedirectURIs:            []string{redirectURI},
			TokenEndpointAuthMethod: "none",
		})
	}))
	defer server.Close()
	metadataURL = server.URL + "/metadata"
	codec := testCodec(t)
	service := Service{
		Codec: codec,
		MetadataFetcher: ClientMetadataFetcher{
			HTTPClient:     server.Client(),
			AllowedOrigins: []string{server.URL},
		},
		CodeStore:   NewCodeStore(8, clock.Now),
		Clock:       clock,
		IDGenerator: fixedIDGenerator{id: "code-id-1"},
	}
	pkceVerifier := "verifier-with-enough-entropy-for-test"
	req := AuthorizationRequest{
		Issuer:        "https://mcp.example.com",
		Audience:      "https://mcp.example.com/mcp",
		Subject:       "connector-user",
		ClientID:      metadataURL,
		RedirectURI:   redirectURI,
		PKCEChallenge: PKCEChallengeS256(pkceVerifier),
		Scopes:        []string{"inventree.read"},
		Credential:    Credential{Scheme: inventree.AuthSchemeToken, Token: "upstream-token"},
	}

	code, err := service.IssueAuthorizationCode(ctx, req)
	r.NoError(err)
	pair, err := service.ExchangeAuthorizationCode(ctx, code, AssociatedData{
		Issuer:   req.Issuer,
		Audience: req.Audience,
		ClientID: req.ClientID,
	}, redirectURI, pkceVerifier)
	r.NoError(err)
	a.Equal(now.Add(DefaultAccessTokenLifetime), pair.AccessExpiresAt)
	a.Equal(now.Add(DefaultRefreshTokenLifetime), pair.RefreshExpiresAt)
	a.Equal(now.Add(DefaultSessionLifetime), pair.SessionExpiresAt)
	_, err = service.ExchangeAuthorizationCode(ctx, code, AssociatedData{
		Issuer:   req.Issuer,
		Audience: req.Audience,
		ClientID: req.ClientID,
	}, redirectURI, pkceVerifier)
	r.ErrorIs(err, ErrCodeAlreadyUsed)

	var accessClaims TokenClaims
	r.NoError(codec.Open(pair.AccessToken, AssociatedData{
		Issuer:   req.Issuer,
		Audience: req.Audience,
		ClientID: req.ClientID,
		Type:     TokenTypeAccess,
	}, &accessClaims))
	a.Equal(TokenTypeAccess, accessClaims.Type)
	a.Equal("upstream-token", accessClaims.Credential.Token)
	a.Equal(now.Add(DefaultAccessTokenLifetime), accessClaims.ExpiresAt)
}

func TestServiceRejectsWrongPKCEVerifier(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	now := time.Date(2026, 7, 7, 10, 30, 0, 0, time.UTC)
	clock := fakeClock{now: now}
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var metadataURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ClientMetadata{
			ClientID:                metadataURL,
			RedirectURIs:            []string{redirectURI},
			TokenEndpointAuthMethod: "none",
		})
	}))
	defer server.Close()
	metadataURL = server.URL + "/metadata"
	service := Service{
		Codec: testCodec(t),
		MetadataFetcher: ClientMetadataFetcher{
			HTTPClient:     server.Client(),
			AllowedOrigins: []string{server.URL},
		},
		CodeStore:   NewCodeStore(8, clock.Now),
		Clock:       clock,
		IDGenerator: fixedIDGenerator{id: "code-id-2"},
	}
	code, err := service.IssueAuthorizationCode(ctx, AuthorizationRequest{
		Issuer:        "https://mcp.example.com",
		Audience:      "https://mcp.example.com/mcp",
		Subject:       "connector-user",
		ClientID:      metadataURL,
		RedirectURI:   redirectURI,
		PKCEChallenge: PKCEChallengeS256("correct-verifier"),
		Credential:    Credential{Scheme: inventree.AuthSchemeToken, Token: "upstream-token"},
	})
	r.NoError(err)
	_, err = service.ExchangeAuthorizationCode(ctx, code, AssociatedData{
		Issuer:   "https://mcp.example.com",
		Audience: "https://mcp.example.com/mcp",
		ClientID: metadataURL,
	}, redirectURI, "wrong-verifier")
	r.ErrorIs(err, ErrInvalidCode)
}

func TestServiceIssueAuthorizationCodeRejectsBadMetadataBeforeStoringCode(t *testing.T) {
	redirectURI := "https://chatgpt.com/connector/oauth/callback_123"
	var metadataURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Query().Get("case") {
		case "wrong_redirect":
			_ = json.NewEncoder(w).Encode(ClientMetadata{
				ClientID:                metadataURL + "?case=wrong_redirect",
				RedirectURIs:            []string{"https://chatgpt.com/connector/oauth/other"},
				TokenEndpointAuthMethod: "none",
			})
		case "mismatch":
			_ = json.NewEncoder(w).Encode(ClientMetadata{
				ClientID:                "https://chatgpt.com/.well-known/client-metadata",
				RedirectURIs:            []string{redirectURI},
				TokenEndpointAuthMethod: "none",
			})
		default:
			http.Error(w, "metadata unavailable", http.StatusBadGateway)
		}
	}))
	defer server.Close()
	metadataURL = server.URL + "/metadata"

	cases := []struct {
		name           string
		clientID       string
		allowedOrigins []string
	}{
		{
			name:           "bad client id",
			clientID:       "not-a-url",
			allowedOrigins: []string{server.URL},
		},
		{
			name:           "non HTTPS client id",
			clientID:       "http://chatgpt.com/client-metadata",
			allowedOrigins: []string{server.URL},
		},
		{
			name:     "missing allowed origins",
			clientID: metadataURL,
		},
		{
			name:           "wrong redirect",
			clientID:       metadataURL + "?case=wrong_redirect",
			allowedOrigins: []string{server.URL},
		},
		{
			name:           "metadata mismatch",
			clientID:       metadataURL + "?case=mismatch",
			allowedOrigins: []string{server.URL},
		},
		{
			name:           "metadata fetch failure",
			clientID:       metadataURL + "?case=fetch_failure",
			allowedOrigins: []string{server.URL},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			now := time.Date(2026, 7, 7, 10, 45, 0, 0, time.UTC)
			clock := fakeClock{now: now}
			store := NewCodeStore(1, clock.Now)
			service := Service{
				Codec: testCodec(t),
				MetadataFetcher: ClientMetadataFetcher{
					HTTPClient:     server.Client(),
					AllowedOrigins: tc.allowedOrigins,
				},
				CodeStore:   store,
				Clock:       clock,
				IDGenerator: fixedIDGenerator{id: "fixed-code-id"},
			}
			code, err := service.IssueAuthorizationCode(ctx, AuthorizationRequest{
				Issuer:        "https://mcp.example.com",
				Audience:      "https://mcp.example.com/mcp",
				Subject:       "connector-user",
				ClientID:      tc.clientID,
				RedirectURI:   redirectURI,
				PKCEChallenge: PKCEChallengeS256("verifier"),
				Credential:    Credential{Scheme: inventree.AuthSchemeToken, Token: "upstream-token"},
			})
			r.Error(err)
			r.Empty(code)
			r.NoError(store.Store("fixed-code-id", now.Add(time.Minute)))
		})
	}
}

func TestServiceRefreshValidatesRefreshEnvelopeAndCredential(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	a := assert.New(t)
	now := time.Date(2026, 7, 7, 11, 0, 0, 0, time.UTC)
	clock := fakeClock{now: now}
	codec := testCodec(t)
	service := Service{
		Codec:               codec,
		Clock:               clock,
		CredentialValidator: fakeCredentialValidator{},
	}
	aad := AssociatedData{
		Issuer:   "https://mcp.example.com",
		Audience: "https://mcp.example.com/mcp",
		ClientID: "https://chatgpt.com/client-metadata",
		Type:     TokenTypeRefresh,
	}
	refreshClaims := TokenClaims{
		Type:             TokenTypeRefresh,
		Issuer:           aad.Issuer,
		Audience:         aad.Audience,
		Subject:          "connector-user",
		ClientID:         aad.ClientID,
		Scopes:           []string{"inventree.read"},
		IssuedAt:         now,
		ExpiresAt:        now.Add(time.Hour),
		SessionExpiresAt: now.Add(2 * time.Hour),
		Credential:       Credential{Scheme: inventree.AuthSchemeBearer, Token: "fresh-token"},
	}
	refreshToken, err := codec.Seal(ctx, aad, refreshClaims)
	r.NoError(err)
	pair, err := service.Refresh(ctx, refreshToken, aad)
	r.NoError(err)
	a.NotEmpty(pair.AccessToken)
	a.NotEmpty(pair.RefreshToken)
	a.Equal(now.Add(DefaultAccessTokenLifetime), pair.AccessExpiresAt)
	a.Equal(now.Add(2*time.Hour), pair.RefreshExpiresAt)
	a.Equal(now.Add(2*time.Hour), pair.SessionExpiresAt)

	badClaims := refreshClaims
	badClaims.Credential.Token = "revoked"
	badToken, err := codec.Seal(ctx, aad, badClaims)
	r.NoError(err)
	_, err = service.Refresh(ctx, badToken, aad)
	r.ErrorContains(err, "credential rejected")
}

func TestServiceRefreshRejectsInvalidRefreshClaims(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	now := time.Date(2026, 7, 7, 11, 30, 0, 0, time.UTC)
	codec := testCodec(t)
	service := Service{Codec: codec, Clock: fakeClock{now: now}}
	aad := AssociatedData{
		Issuer:   "https://mcp.example.com",
		Audience: "https://mcp.example.com/mcp",
		ClientID: "https://chatgpt.com/client-metadata",
		Type:     TokenTypeRefresh,
	}
	base := TokenClaims{
		Type:             TokenTypeRefresh,
		Issuer:           aad.Issuer,
		Audience:         aad.Audience,
		Subject:          "connector-user",
		ClientID:         aad.ClientID,
		Scopes:           []string{"inventree.read"},
		IssuedAt:         now.Add(-time.Minute),
		ExpiresAt:        now.Add(time.Hour),
		SessionExpiresAt: now.Add(2 * time.Hour),
		Credential:       Credential{Scheme: inventree.AuthSchemeToken, Token: "fresh-token"},
	}
	cases := []struct {
		name   string
		claims TokenClaims
		aad    AssociatedData
	}{
		{
			name: "expired refresh",
			claims: func() TokenClaims {
				claims := base
				claims.ExpiresAt = now.Add(-time.Second)
				return claims
			}(),
			aad: aad,
		},
		{
			name: "expired session",
			claims: func() TokenClaims {
				claims := base
				claims.SessionExpiresAt = now.Add(-time.Second)
				return claims
			}(),
			aad: aad,
		},
		{
			name: "access token on refresh path",
			claims: func() TokenClaims {
				claims := base
				claims.Type = TokenTypeAccess
				return claims
			}(),
			aad: AssociatedData{Issuer: aad.Issuer, Audience: aad.Audience, ClientID: aad.ClientID, Type: TokenTypeAccess},
		},
		{
			name:   "wrong audience",
			claims: base,
			aad:    AssociatedData{Issuer: aad.Issuer, Audience: "https://other.example.com/mcp", ClientID: aad.ClientID, Type: TokenTypeRefresh},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := codec.Seal(ctx, tc.aad, tc.claims)
			r.NoError(err)
			_, err = service.Refresh(ctx, token, aad)
			r.ErrorIs(err, ErrInvalidToken)
		})
	}
}

func TestCodeStoreBoundsExpiryAndOneTimeUse(t *testing.T) {
	r := require.New(t)
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	store := NewCodeStore(1, func() time.Time { return now })
	r.NoError(store.Store("one", now.Add(time.Minute)))
	r.ErrorContains(store.Store("two", now.Add(time.Minute)), "full")
	r.NoError(store.Consume("one"))
	r.ErrorIs(store.Consume("one"), ErrCodeAlreadyUsed)
	r.Error(store.Store("expired", now.Add(-time.Second)))

	r.NoError(store.Store("expires-later", now.Add(time.Minute)))
	now = now.Add(2 * time.Minute)
	r.ErrorIs(store.Consume("expires-later"), ErrCodeAlreadyUsed)
	r.NoError(store.Store("after-purge", now.Add(time.Minute)))
}

func testCodec(t *testing.T) EnvelopeCodec {
	t.Helper()
	keyring, err := NewKeyring([]Key{{ID: "test", Material: bytesOf('t', 32), State: KeyStateActive}})
	require.NoError(t, err)
	return EnvelopeCodec{Keyring: keyring, Random: counterRandom{}}
}

func bytesOf(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}

type counterRandom struct{}

func (counterRandom) ReadRandom(_ context.Context, out []byte) error {
	for i := range out {
		out[i] = byte(i + 1)
	}
	return nil
}

type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time {
	return c.now
}

func (c fakeClock) Since(t time.Time) time.Duration {
	return c.now.Sub(t)
}

type fixedIDGenerator struct {
	id string
}

func (g fixedIDGenerator) NewID(context.Context) (string, error) {
	return g.id, nil
}

type fakeCredentialValidator struct{}

func (fakeCredentialValidator) ValidateCredential(_ context.Context, credential Credential) error {
	if credential.Token == "revoked" {
		return errors.New("credential rejected")
	}
	return credential.Validate()
}

var _ platform.Clock = fakeClock{}
