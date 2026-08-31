package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
)

const (
	setupStateType       = "setup_state"
	setupCSRFTokenBytes  = 32
	setupRequestMaxBytes = 64 * 1024
	defaultSetupTimeout  = 15 * time.Second
	setupCookieName      = "inventree_mcp_setup_csrf"
	defaultRateLimit     = 10
)

type AuthorizationServer struct {
	Issuer   string
	Resource string
	// AuthorizePath and TokenPath are the exact HTTP paths the
	// authorization and token endpoints are registered and advertised at.
	// Callers derive these from the configured MCP path prefix (see
	// config.Config.OAuthAuthorizePath/OAuthTokenPath) rather than from the
	// OAuth issuer URL's own path, so they cannot collide with InvenTree
	// routes served at the issuer origin.
	AuthorizePath     string
	TokenPath         string
	Scopes            []string
	Service           Service
	MetadataFetcher   ClientMetadataFetcher
	AssertionVerifier PrivateKeyJWTVerifier
	CredentialBroker  CredentialBroker
	RateLimiter       *RequestRateLimiter
	SetupStore        *CodeStore
	Clock             platform.Clock
	SetupTimeout      time.Duration
}

type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

type setupState struct {
	Type                    string    `json:"typ"`
	ClientID                string    `json:"client_id"`
	RedirectURI             string    `json:"redirect_uri"`
	Resource                string    `json:"resource"`
	State                   string    `json:"state"`
	PKCEChallenge           string    `json:"code_challenge"`
	Scopes                  []string  `json:"scopes"`
	CSRFHash                string    `json:"csrf_hash"`
	SetupID                 string    `json:"setup_id"`
	AllowSuppliedCredential bool      `json:"allow_supplied_credential,omitempty"`
	ExpiresAt               time.Time `json:"exp"`
}

type setupPageData struct {
	Action        string
	SetupState    string
	ClientID      string
	CSRF          string
	Scopes        string
	AllowFallback bool
	Message       string
}

var setupPageTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Connect InvenTree</title></head><body><main>
<h1>Connect InvenTree</h1><p>Requested permissions: {{.Scopes}}</p>
<p>Authorizing creates a uniquely named dedicated InvenTree API token without rotating the credential you submit or an earlier connector token. MCP permissions limit connector tools, but do not reduce the permissions of the InvenTree credential or dedicated token. Revoke unused connector tokens in InvenTree after abandoned or expired authorizations.</p>
{{if .Message}}<p role="alert">{{.Message}}</p>{{end}}
<form method="post" action="{{.Action}}" autocomplete="off">
<input type="hidden" name="setup_state" value="{{.SetupState}}"><input type="hidden" name="client_id" value="{{.ClientID}}"><input type="hidden" name="csrf" value="{{.CSRF}}">
<label>Credential type <select name="credential_scheme"><option value="Token">Token</option><option value="Bearer">Bearer</option></select></label>
<label>InvenTree credential <input type="password" name="credential" required autocomplete="off"></label>
{{if .AllowFallback}}<label><input type="checkbox" name="use_supplied_credential" value="true" required> Use this supplied credential because a dedicated connector token could not be created</label>{{end}}
<button type="submit">Authorize connector</button>
<button type="submit" name="cancel" value="true" formnovalidate>Cancel</button></form></main></body></html>`))

func (s *AuthorizationServer) Register(mux *http.ServeMux) error {
	if mux == nil || s.Issuer == "" || s.Resource == "" || s.AuthorizePath == "" || s.TokenPath == "" || s.CredentialBroker == nil {
		return errors.New("OAuth authorization server configuration is incomplete")
	}
	metadataPath, authorizePath, tokenPath, err := s.paths()
	if err != nil {
		return err
	}
	if s.AssertionVerifier.TokenEndpoint == "" {
		s.AssertionVerifier.TokenEndpoint = endpointURL(s.Issuer, tokenPath)
	}
	if s.SetupStore == nil {
		s.SetupStore = NewCodeStore(1024, s.now)
	}
	mux.Handle(metadataPath, secureOAuthHeaders(http.HandlerFunc(s.handleMetadata)))
	mux.Handle(authorizePath, secureOAuthHeaders(http.HandlerFunc(s.handleAuthorize)))
	mux.Handle(tokenPath, secureOAuthHeaders(http.HandlerFunc(s.handleToken)))
	return nil
}

func (s *AuthorizationServer) handleMetadata(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, authorizePath, tokenPath, err := s.paths()
	if err != nil {
		http.Error(w, "OAuth server unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, authorizationServerMetadata{
		Issuer: s.Issuer, AuthorizationEndpoint: endpointURL(s.Issuer, authorizePath), TokenEndpoint: endpointURL(s.Issuer, tokenPath),
		ClientIDMetadataDocumentSupported: true,
		ResponseTypesSupported:            []string{"code"}, GrantTypesSupported: []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported: []string{"S256"}, TokenEndpointAuthMethodsSupported: []string{"private_key_jwt"},
		ScopesSupported: append([]string(nil), s.Scopes...),
	})
}

func (s *AuthorizationServer) handleAuthorize(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		s.beginAuthorization(w, req)
	case http.MethodPost:
		s.completeAuthorization(w, req)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *AuthorizationServer) beginAuthorization(w http.ResponseWriter, req *http.Request) {
	if !s.allow(req) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many authorization attempts", http.StatusTooManyRequests)
		return
	}
	values := req.URL.Query()
	clientID := values.Get("client_id")
	redirectURI := values.Get("redirect_uri")
	if values.Get("response_type") != "code" || values.Get("code_challenge_method") != "S256" || !validPKCEChallenge(values.Get("code_challenge")) || values.Get("state") == "" || values.Get("resource") != s.Resource {
		http.Error(w, "Invalid OAuth authorization request", http.StatusBadRequest)
		return
	}
	if !s.allowClient(clientID) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many authorization attempts", http.StatusTooManyRequests)
		return
	}
	if _, err := s.MetadataFetcher.FetchAndValidate(req.Context(), clientID, redirectURI); err != nil {
		http.Error(w, "Invalid OAuth client", http.StatusBadRequest)
		return
	}
	scopes, ok := parseScopes(values.Get("scope"), s.Scopes)
	if !ok {
		http.Error(w, "Invalid OAuth scope", http.StatusBadRequest)
		return
	}
	state := setupState{Type: setupStateType, ClientID: clientID, RedirectURI: redirectURI, Resource: s.Resource, State: values.Get("state"), PKCEChallenge: values.Get("code_challenge"), Scopes: scopes, ExpiresAt: s.now().Add(defaultDuration(s.Service.CodeLifetime, DefaultCodeLifetime))}
	s.renderSetup(w, req, state, "")
}

func (s *AuthorizationServer) completeAuthorization(w http.ResponseWriter, req *http.Request) {
	if !s.allow(req) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many setup attempts", http.StatusTooManyRequests)
		return
	}
	if !isFormRequest(req) {
		http.Error(w, "Invalid setup submission", http.StatusUnsupportedMediaType)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, setupRequestMaxBytes)
	if err := req.ParseForm(); err != nil {
		http.Error(w, "Invalid setup submission", http.StatusBadRequest)
		return
	}
	clientID := req.PostForm.Get("client_id")
	if !s.allowClient(clientID) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many setup attempts", http.StatusTooManyRequests)
		return
	}
	var state setupState
	if err := s.Service.Codec.Open(req.PostForm.Get("setup_state"), AssociatedData{Issuer: s.Issuer, Audience: s.Resource, ClientID: clientID, Type: setupStateType}, &state); err != nil || state.Type != setupStateType || state.ClientID != clientID || !state.ExpiresAt.After(s.now()) {
		http.Error(w, "Invalid or expired setup session", http.StatusBadRequest)
		return
	}
	if !validSetupCSRF(req, state) {
		http.Error(w, "Invalid setup session", http.StatusForbidden)
		return
	}
	if err := s.SetupStore.Consume(state.SetupID); err != nil {
		http.Error(w, "Invalid or expired setup session", http.StatusBadRequest)
		return
	}
	if req.PostForm.Get("cancel") == "true" {
		redirectAuthorizationError(w, req, state.RedirectURI, state.State, "access_denied")
		return
	}
	credential := Credential{Scheme: inventree.AuthScheme(req.PostForm.Get("credential_scheme")), Token: strings.TrimSpace(req.PostForm.Get("credential"))}
	if err := credential.ValidateForEnvelope(); err != nil {
		// The setup page's own <select> only offers Token/Bearer; this also
		// rejects Basic, which is valid only for the separate bootstrap flow
		// and must never be sealed into an OAuth token envelope.
		s.renderSetup(w, req, state, "The InvenTree credential could not be validated.")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), defaultDuration(s.SetupTimeout, defaultSetupTimeout))
	defer cancel()
	subject, err := s.CredentialBroker.ValidateCredential(ctx, credential)
	if err != nil {
		s.renderSetup(w, req, state, "The InvenTree credential could not be validated.")
		return
	}
	selectedCredential, source := credential, CredentialSourceSupplied
	if state.AllowSuppliedCredential && req.PostForm.Get("use_supplied_credential") == "true" {
		// Explicit fallback consent is bound into the sealed setup state.
	} else {
		selectedCredential, err = s.CredentialBroker.CreateDedicatedCredential(ctx, credential, dedicatedTokenName(state.SetupID))
		if err != nil {
			state.AllowSuppliedCredential = true
			s.renderSetup(w, req, state, "A dedicated connector token could not be created. Cancel is recommended. To continue anyway, re-enter the credential and explicitly choose to seal that credential with its full upstream permissions.")
			return
		}
		source = CredentialSourceDedicated
	}
	code, err := s.Service.IssueAuthorizationCode(ctx, AuthorizationRequest{
		Issuer: s.Issuer, Audience: s.Resource, Subject: subject, ClientID: state.ClientID, RedirectURI: state.RedirectURI,
		PKCEChallenge: state.PKCEChallenge, Scopes: state.Scopes, Credential: selectedCredential, CredentialSource: source,
	})
	if err != nil {
		http.Error(w, "Authorization could not be completed", http.StatusBadRequest)
		return
	}
	redirectAuthorizationCode(w, req, state.RedirectURI, state.State, code)
}

func redirectAuthorizationCode(w http.ResponseWriter, req *http.Request, redirectURI string, state string, code string) {
	redirect, _ := url.Parse(redirectURI)
	query := redirect.Query()
	query.Set("code", code)
	query.Set("state", state)
	redirect.RawQuery = query.Encode()
	http.Redirect(w, req, redirect.String(), http.StatusFound)
}

func redirectAuthorizationError(w http.ResponseWriter, req *http.Request, redirectURI string, state string, code string) {
	redirect, _ := url.Parse(redirectURI)
	query := redirect.Query()
	query.Set("error", code)
	query.Set("state", state)
	redirect.RawQuery = query.Encode()
	http.Redirect(w, req, redirect.String(), http.StatusFound)
}

func (s *AuthorizationServer) renderSetup(w http.ResponseWriter, req *http.Request, state setupState, message string) {
	csrfBytes := make([]byte, setupCSRFTokenBytes)
	if _, err := rand.Read(csrfBytes); err != nil {
		http.Error(w, "Setup unavailable", http.StatusInternalServerError)
		return
	}
	csrf := base64.RawURLEncoding.EncodeToString(csrfBytes)
	setupIDBytes := make([]byte, setupCSRFTokenBytes)
	if _, err := rand.Read(setupIDBytes); err != nil {
		http.Error(w, "Setup unavailable", http.StatusInternalServerError)
		return
	}
	state.SetupID = base64.RawURLEncoding.EncodeToString(setupIDBytes)
	if err := s.SetupStore.Store(state.SetupID, state.ExpiresAt); err != nil {
		http.Error(w, "Setup unavailable", http.StatusServiceUnavailable)
		return
	}
	hash := sha256.Sum256([]byte(csrf))
	state.CSRFHash = base64.RawURLEncoding.EncodeToString(hash[:])
	token, err := s.Service.Codec.Seal(req.Context(), AssociatedData{Issuer: s.Issuer, Audience: s.Resource, ClientID: state.ClientID, Type: setupStateType}, state)
	if err != nil {
		http.Error(w, "Setup unavailable", http.StatusInternalServerError)
		return
	}
	_, authorizePath, _, _ := s.paths()
	http.SetCookie(w, &http.Cookie{Name: setupCookieName, Value: csrf, Path: authorizePath, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: max(1, int(state.ExpiresAt.Sub(s.now()).Seconds()))})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupPageTemplate.Execute(w, setupPageData{Action: authorizePath, SetupState: token, ClientID: state.ClientID, CSRF: csrf, Scopes: strings.Join(state.Scopes, " "), AllowFallback: state.AllowSuppliedCredential, Message: message}); err != nil {
		return
	}
}

func (s *AuthorizationServer) handleToken(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allow(req) {
		w.Header().Set("Retry-After", "60")
		writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable")
		return
	}
	if !isFormRequest(req) {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_request")
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, setupRequestMaxBytes)
	if err := req.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	clientID := req.PostForm.Get("client_id")
	if !s.allowClient(clientID) {
		w.Header().Set("Retry-After", "60")
		writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable")
		return
	}
	var metadata ClientMetadata
	var err error
	if req.PostForm.Get("grant_type") == "authorization_code" {
		metadata, err = s.MetadataFetcher.FetchAndValidate(req.Context(), clientID, req.PostForm.Get("redirect_uri"))
	} else {
		metadata, err = s.MetadataFetcher.Fetch(req.Context(), clientID)
	}
	if err != nil || s.AssertionVerifier.Verify(req.Context(), clientID, metadata, req.PostForm.Get("client_assertion_type"), req.PostForm.Get("client_assertion")) != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if req.PostForm.Get("resource") != s.Resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	aad := AssociatedData{Issuer: s.Issuer, Audience: s.Resource, ClientID: clientID}
	var pair TokenPair
	switch req.PostForm.Get("grant_type") {
	case "authorization_code":
		pair, err = s.Service.ExchangeAuthorizationCode(req.Context(), req.PostForm.Get("code"), aad, req.PostForm.Get("redirect_uri"), req.PostForm.Get("code_verifier"))
	case "refresh_token":
		pair, err = s.Service.Refresh(req.Context(), req.PostForm.Get("refresh_token"), aad)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": pair.AccessToken, "token_type": "Bearer", "expires_in": max(0, int(pair.AccessExpiresAt.Sub(s.now()).Seconds())),
		"refresh_token": pair.RefreshToken, "scope": strings.Join(pair.Scopes, " "), "credential_source": pair.CredentialSource,
	})
}

func (s *AuthorizationServer) paths() (string, string, string, error) {
	issuer, err := url.Parse(s.Issuer)
	if err != nil || issuer.Scheme == "" || issuer.Host == "" {
		return "", "", "", errors.New("invalid OAuth issuer URL")
	}
	base := strings.TrimSuffix(issuer.Path, "/")
	metadata := "/.well-known/oauth-authorization-server" + base
	return metadata, s.AuthorizePath, s.TokenPath, nil
}

func (s *AuthorizationServer) allow(req *http.Request) bool {
	limiter := s.RateLimiter
	if limiter == nil {
		return true
	}
	host := "unknown"
	if sourceIP, ok := requestctx.SourceIP(req.Context()); ok {
		host = sourceIP.String()
	} else if remoteHost, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		host = remoteHost
	} else if req.RemoteAddr != "" {
		host = req.RemoteAddr
	}
	return limiter.Allow(req.URL.Path + "\x00ip:" + host)
}

func (s *AuthorizationServer) allowKey(key string) bool {
	if s.RateLimiter == nil {
		return true
	}
	return s.RateLimiter.Allow(key)
}

func (s *AuthorizationServer) allowClient(clientID string) bool {
	if len(clientID) == 0 || len(clientID) > 2048 {
		return false
	}
	if len(s.MetadataFetcher.AllowedClientIDs) > 0 && !slices.Contains(s.MetadataFetcher.AllowedClientIDs, clientID) {
		return false
	}
	hash := sha256.Sum256([]byte(clientID))
	return s.allowKey("client:" + base64.RawURLEncoding.EncodeToString(hash[:]))
}

func (s *AuthorizationServer) now() time.Time {
	if s.Clock != nil {
		return s.Clock.Now()
	}
	if s.Service.Clock != nil {
		return s.Service.Clock.Now()
	}
	return time.Now()
}

func dedicatedTokenName(setupID string) string {
	return "inventree-mcp-chatgpt-" + setupID
}

func validSetupCSRF(req *http.Request, state setupState) bool {
	cookie, err := req.Cookie(setupCookieName)
	if err != nil || cookie.Value == "" || req.PostForm.Get("csrf") == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(req.PostForm.Get("csrf"))) != 1 {
		return false
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	want, err := base64.RawURLEncoding.DecodeString(state.CSRFHash)
	return err == nil && subtle.ConstantTimeCompare(hash[:], want) == 1
}

func validPKCEChallenge(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && !strings.ContainsRune("-._~", char) {
			return false
		}
	}
	return true
}

func isFormRequest(req *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/x-www-form-urlencoded"
}

func parseScopes(raw string, supported []string) ([]string, bool) {
	values := strings.Fields(raw)
	if len(values) == 0 {
		return nil, false
	}
	for _, value := range values {
		if !slices.Contains(supported, value) {
			return nil, false
		}
	}
	return values, true
}

func secureOAuthHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, req)
	})
}

func endpointURL(issuer string, endpointPath string) string {
	parsed, _ := url.Parse(issuer)
	parsed.Path = endpointPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

type RequestRateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateLimitEntry
	limit   int
	window  time.Duration
	now     func() time.Time
}

type rateLimitEntry struct {
	started time.Time
	count   int
}

func NewRequestRateLimiter(limit int, window time.Duration, now func() time.Time) *RequestRateLimiter {
	if limit <= 0 {
		limit = defaultRateLimit
	}
	if window <= 0 {
		window = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &RequestRateLimiter{entries: make(map[string]rateLimitEntry), limit: limit, window: window, now: now}
}

func (l *RequestRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for candidate, value := range l.entries {
		if now.Sub(value.started) >= l.window {
			delete(l.entries, candidate)
		}
	}
	entry := l.entries[key]
	if entry.started.IsZero() && len(l.entries) >= 1024 {
		return false
	}
	if entry.started.IsZero() || now.Sub(entry.started) >= l.window {
		entry = rateLimitEntry{started: now}
	}
	entry.count++
	l.entries[key] = entry
	return entry.count <= l.limit
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
