package oauth

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/golang-jwt/jwt/v5"
)

const (
	ClientAssertionTypeJWTBearer = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	defaultJWKSMaxBytes          = 64 * 1024
	defaultClientAssertionAge    = 5 * time.Minute
)

var ErrInvalidClientAssertion = errors.New("invalid OAuth client assertion")

type AssertionReplayStore struct {
	mu         sync.Mutex
	entries    map[string]time.Time
	maxEntries int
	now        func() time.Time
}

func NewAssertionReplayStore(maxEntries int, now func() time.Time) *AssertionReplayStore {
	if now == nil {
		now = time.Now
	}
	return &AssertionReplayStore{entries: make(map[string]time.Time), maxEntries: maxEntries, now: now}
}

func (s *AssertionReplayStore) Use(id string, expiresAt time.Time) error {
	if s == nil || id == "" || !expiresAt.After(s.now()) {
		return ErrInvalidClientAssertion
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, expiry := range s.entries {
		if !expiry.After(now) {
			delete(s.entries, key)
		}
	}
	if _, exists := s.entries[id]; exists || s.maxEntries <= 0 || len(s.entries) >= s.maxEntries {
		return ErrInvalidClientAssertion
	}
	s.entries[id] = expiresAt
	return nil
}

type PrivateKeyJWTVerifier struct {
	HTTPClient    *http.Client
	TokenEndpoint string
	ReplayStore   *AssertionReplayStore
	Clock         platform.Clock
	MaxJWKSBytes  int64
}

func (v PrivateKeyJWTVerifier) Verify(ctx context.Context, clientID string, metadata ClientMetadata, assertionType string, assertion string) error {
	if assertionType != ClientAssertionTypeJWTBearer || assertion == "" || metadata.JWKSURI == "" {
		return ErrInvalidClientAssertion
	}
	keys, err := v.fetchKeys(ctx, clientID, metadata.JWKSURI)
	if err != nil {
		return ErrInvalidClientAssertion
	}
	now := time.Now()
	if v.Clock != nil {
		now = v.Clock.Now()
	}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(assertion, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, ErrInvalidClientAssertion
		}
		for _, key := range keys {
			if key.Kid == kid && (key.Alg == "" || key.Alg == token.Method.Alg()) && (key.Use == "" || key.Use == "sig") {
				return key.publicKey()
			}
		}
		return nil, ErrInvalidClientAssertion
	}, jwt.WithAudience(v.TokenEndpoint), jwt.WithIssuer(clientID), jwt.WithSubject(clientID), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(30*time.Second), jwt.WithTimeFunc(func() time.Time { return now }), jwt.WithValidMethods([]string{"RS256", "PS256", "ES256"}))
	if err != nil || !token.Valid || claims.ExpiresAt == nil || claims.IssuedAt == nil || claims.ID == "" {
		return ErrInvalidClientAssertion
	}
	if claims.ExpiresAt.After(now.Add(defaultClientAssertionAge)) || claims.IssuedAt.Before(now.Add(-defaultClientAssertionAge)) || claims.IssuedAt.After(now.Add(30*time.Second)) {
		return ErrInvalidClientAssertion
	}
	if err := v.ReplayStore.Use(clientID+"\x00"+claims.ID, claims.ExpiresAt.Time); err != nil {
		return ErrInvalidClientAssertion
	}
	return nil
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	Kid string `json:"kid"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

func (v PrivateKeyJWTVerifier) fetchKeys(ctx context.Context, clientID string, rawJWKSURL string) ([]jwk, error) {
	clientURL, err := url.Parse(clientID)
	if err != nil {
		return nil, err
	}
	jwksURL, err := validateClientIDURL(rawJWKSURL, []string{clientURL.Scheme + "://" + clientURL.Host})
	if err != nil {
		return nil, err
	}
	client := v.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultClientMetadataTimeout}
	}
	client = cloneClientWithSafeRedirects(client, jwksURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/jwk-set+json, application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS fetch returned HTTP %d", resp.StatusCode)
	}
	limit := v.MaxJWKSBytes
	if limit == 0 {
		limit = defaultJWKSMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("invalid JWKS response")
	}
	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil || len(set.Keys) == 0 {
		return nil, errors.New("invalid JWKS response")
	}
	return set.Keys, nil
}

func (k jwk) publicKey() (any, error) {
	switch k.Kty {
	case "RSA":
		n, err := decodeBigInt(k.N)
		if err != nil || n.BitLen() < 2048 {
			return nil, err
		}
		e, err := decodeBigInt(k.E)
		if err != nil || !e.IsInt64() || e.Int64() < 3 || e.Bit(0) == 0 {
			return nil, ErrInvalidClientAssertion
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	case "EC":
		if k.Crv != "P-256" {
			return nil, ErrInvalidClientAssertion
		}
		x, err := decodeBigInt(k.X)
		if err != nil {
			return nil, err
		}
		y, err := decodeBigInt(k.Y)
		if err != nil || x.BitLen() > 256 || y.BitLen() > 256 {
			return nil, ErrInvalidClientAssertion
		}
		encoded := make([]byte, 1+2*((elliptic.P256().Params().BitSize+7)/8))
		encoded[0] = 4
		x.FillBytes(encoded[1 : 1+(len(encoded)-1)/2])
		y.FillBytes(encoded[1+(len(encoded)-1)/2:])
		if _, err := ecdh.P256().NewPublicKey(encoded); err != nil {
			return nil, ErrInvalidClientAssertion
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
	default:
		return nil, ErrInvalidClientAssertion
	}
}

func decodeBigInt(raw string) (*big.Int, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) == 0 {
		return nil, ErrInvalidClientAssertion
	}
	return new(big.Int).SetBytes(data), nil
}
