package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/requestctx"
)

const (
	bootstrapRequestMaxBytes    = 4 * 1024
	maxBootstrapAuthHeaderBytes = 8 * 1024
	defaultBootstrapTimeout     = 15 * time.Second
	maxDedicatedTokenAttempts   = 3
	bootstrapTokenNameBytes     = 16
)

var errInvalidBootstrapAuthorization = errors.New("invalid bootstrap authorization header")

// BootstrapServer implements the F-S93 stateless per-user bearer bootstrap
// endpoint: a user exchanges InvenTree Basic (username/password) or an
// existing InvenTree Token/Bearer credential for an opaque, encrypted MCP
// bearer envelope containing a freshly minted dedicated InvenTree API
// token. No server-side token mapping is stored; the envelope carries
// everything a subsequent request needs. If dedicated-token creation
// fails, the request fails closed and the supplied credential is never
// sealed as a fallback.
type BootstrapServer struct {
	Issuer           string
	Resource         string
	Codec            EnvelopeCodec
	CredentialBroker CredentialBroker
	RateLimiter      *RequestRateLimiter
	Scopes           []string
	EnvelopeLifetime time.Duration
	IDGenerator      platform.IDGenerator
	Clock            platform.Clock
	RequestTimeout   time.Duration
}

func (s *BootstrapServer) Register(mux *http.ServeMux, path string) error {
	if mux == nil || path == "" || s.Issuer == "" || s.Resource == "" || s.CredentialBroker == nil {
		return errors.New("bootstrap server configuration is incomplete")
	}
	mux.Handle(path, secureOAuthHeaders(http.HandlerFunc(s.handleBootstrap)))
	return nil
}

func (s *BootstrapServer) handleBootstrap(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.allow(req) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many bootstrap attempts", http.StatusTooManyRequests)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, bootstrapRequestMaxBytes)
	// The bootstrap request carries no body fields; the credential travels
	// only in the Authorization header. Drain and discard defensively.
	if _, err := io.Copy(io.Discard, req.Body); err != nil {
		http.Error(w, "Invalid bootstrap request", http.StatusBadRequest)
		return
	}

	credential, err := parseBootstrapAuthorization(req.Header.Get("Authorization"))
	if err != nil {
		http.Error(w, "Invalid bootstrap request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), defaultDuration(s.RequestTimeout, defaultBootstrapTimeout))
	defer cancel()
	logger := logging.FromContext(ctx)

	// Rate-limit by IP first (bounds a single source), then by a hash of the
	// submitted credential (bounds distributed brute-forcing of one InvenTree
	// account regardless of source IP). Neither key retains the credential.
	if !s.allowCredential(credential) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many bootstrap attempts", http.StatusTooManyRequests)
		return
	}

	subject, err := s.CredentialBroker.ValidateCredential(ctx, credential)
	if err != nil {
		logger.WarnContext(ctx, "bootstrap credential validation failed")
		http.Error(w, "InvenTree credential could not be validated", http.StatusUnauthorized)
		return
	}

	dedicated, err := s.createDedicatedCredential(ctx, credential)
	if err != nil {
		logger.WarnContext(ctx, "bootstrap dedicated token creation failed", slog.String("subject", subject))
		http.Error(w, "Dedicated InvenTree token unavailable", http.StatusBadGateway)
		return
	}
	if err := dedicated.ValidateForEnvelope(); err != nil {
		// Defense in depth: the broker contract always returns a Token-scheme
		// credential, but never seal anything that doesn't independently pass
		// the same guard every other envelope-bound credential passes.
		logger.WarnContext(ctx, "bootstrap dedicated credential failed envelope validation", slog.String("subject", subject))
		http.Error(w, "Dedicated InvenTree token unavailable", http.StatusBadGateway)
		return
	}

	now := s.now()
	expiresAt := now.Add(defaultDuration(s.EnvelopeLifetime, DefaultBootstrapEnvelopeLifetime))
	claims := TokenClaims{
		Type:             TokenTypeBootstrapAccess,
		Issuer:           s.Issuer,
		Audience:         s.Resource,
		Subject:          subject,
		ClientID:         "",
		Scopes:           append([]string(nil), s.Scopes...),
		IssuedAt:         now,
		ExpiresAt:        expiresAt,
		SessionExpiresAt: expiresAt,
		Credential:       dedicated,
		CredentialSource: CredentialSourceDedicated,
	}
	token, err := s.Codec.Seal(ctx, AssociatedData{Issuer: s.Issuer, Audience: s.Resource, ClientID: "", Type: TokenTypeBootstrapAccess}, claims)
	if err != nil {
		logger.WarnContext(ctx, "bootstrap envelope seal failed", slog.String("subject", subject))
		http.Error(w, "Bootstrap unavailable", http.StatusInternalServerError)
		return
	}

	logger.InfoContext(ctx, "bootstrap issued MCP bearer envelope", slog.String("subject", subject))
	writeJSON(w, http.StatusOK, map[string]any{
		"mcp_token":  token,
		"token_type": "Bearer",
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

// allowCredential rate-limits by a hash of the submitted credential, so a
// distributed attacker cannot bypass the per-IP limit by spreading guesses
// against one InvenTree account across many source addresses. The hash is
// never stored beyond the limiter's bounded sliding-window entries.
func (s *BootstrapServer) allowCredential(credential Credential) bool {
	if s.RateLimiter == nil {
		return true
	}
	hash := sha256.Sum256([]byte(string(credential.Scheme) + "\x00" + credential.Token))
	return s.RateLimiter.Allow("credential:" + base64.RawURLEncoding.EncodeToString(hash[:]))
}

// createDedicatedCredential mints a fresh, uniquely named dedicated
// InvenTree API token. It never returns the caller-supplied credential: on
// exhaustion of the bounded retry budget, it returns an error and nothing is
// sealed into an envelope.
//
// InvenTree's token-issuance endpoint does not actually error on a colliding
// name — it deletes and recreates a same-named token, per
// docs/api-schema.yaml. The random 128-bit name (bootstrapTokenNameBytes)
// carries the "don't rotate an existing token" guarantee on its own; the
// bounded retry here only helps a transient upstream failure (a timeout or a
// momentary InvenTree error), and stops immediately once the request's
// context is done rather than burning the remaining attempts.
func (s *BootstrapServer) createDedicatedCredential(ctx context.Context, credential Credential) (Credential, error) {
	idGenerator := s.IDGenerator
	if idGenerator == nil {
		idGenerator = platform.RandomIDGenerator{Bytes: bootstrapTokenNameBytes}
	}
	var lastErr error
	for attempt := 0; attempt < maxDedicatedTokenAttempts; attempt++ {
		if ctx.Err() != nil {
			return Credential{}, ctx.Err()
		}
		name, err := idGenerator.NewID(ctx)
		if err != nil {
			return Credential{}, err
		}
		dedicated, err := s.CredentialBroker.CreateDedicatedCredential(ctx, credential, dedicatedBootstrapTokenName(name))
		if err == nil {
			return dedicated, nil
		}
		lastErr = err
	}
	return Credential{}, lastErr
}

func (s *BootstrapServer) allow(req *http.Request) bool {
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

func (s *BootstrapServer) now() time.Time {
	if s.Clock != nil {
		return s.Clock.Now()
	}
	return time.Now()
}

func dedicatedBootstrapTokenName(random string) string {
	return "inventree-mcp-static-" + random
}

// parseBootstrapAuthorization dispatches the bootstrap request's single
// Authorization header into a Credential by its scheme prefix. There is no
// separate JSON credential-form body: exactly one header, so there is no
// dual-credential precedence to resolve. The scheme token is matched
// case-insensitively per RFC 7235; the credential value is not (InvenTree
// tokens and Basic's base64 payload are both case-sensitive).
func parseBootstrapAuthorization(header string) (Credential, error) {
	header = strings.TrimSpace(header)
	if header == "" || len(header) > maxBootstrapAuthHeaderBytes {
		return Credential{}, errInvalidBootstrapAuthorization
	}
	// TrimSpace above already strips any trailing whitespace, so a matched
	// Cut can never leave an empty (whitespace-only) value: the header would
	// have ended in whitespace, and that whitespace would already be gone.
	scheme, value, ok := strings.Cut(header, " ")
	if !ok {
		return Credential{}, errInvalidBootstrapAuthorization
	}
	value = strings.TrimSpace(value)
	switch {
	case strings.EqualFold(scheme, string(inventree.AuthSchemeBasic)):
		return Credential{Scheme: inventree.AuthSchemeBasic, Token: value}, nil
	case strings.EqualFold(scheme, string(inventree.AuthSchemeToken)):
		return Credential{Scheme: inventree.AuthSchemeToken, Token: value}, nil
	case strings.EqualFold(scheme, string(inventree.AuthSchemeBearer)):
		return Credential{Scheme: inventree.AuthSchemeBearer, Token: value}, nil
	default:
		return Credential{}, errInvalidBootstrapAuthorization
	}
}
