package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	DefaultClientMetadataMaxBytes = 64 * 1024
	DefaultClientMetadataTimeout  = 5 * time.Second
)

type ClientMetadata struct {
	ClientID                string   `json:"client_id,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

type ClientMetadataFetcher struct {
	HTTPClient     *http.Client
	AllowedOrigins []string
	MaxBytes       int64
	Timeout        time.Duration
}

func (f ClientMetadataFetcher) FetchAndValidate(ctx context.Context, clientID string, redirectURI string) (ClientMetadata, error) {
	metadataURL, err := validateClientIDURL(clientID, f.AllowedOrigins)
	if err != nil {
		return ClientMetadata{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, defaultDuration(f.Timeout, DefaultClientMetadataTimeout))
	defer cancel()

	client := f.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	client = cloneClientWithSafeRedirects(client, metadataURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL.String(), nil)
	if err != nil {
		return ClientMetadata{}, fmt.Errorf("%w: cannot build metadata request", ErrInvalidClientMetadata)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "inventree-mcp-oauth-cimd")

	resp, err := client.Do(req)
	if err != nil {
		return ClientMetadata{}, fmt.Errorf("%w: fetch failed", ErrInvalidClientMetadata)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ClientMetadata{}, fmt.Errorf("%w: fetch returned HTTP %d", ErrInvalidClientMetadata, resp.StatusCode)
	}
	maxBytes := f.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultClientMetadataMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return ClientMetadata{}, fmt.Errorf("%w: read failed", ErrInvalidClientMetadata)
	}
	if int64(len(body)) > maxBytes {
		return ClientMetadata{}, fmt.Errorf("%w: metadata document exceeds byte limit", ErrInvalidClientMetadata)
	}
	var metadata ClientMetadata
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(&metadata); err != nil {
		return ClientMetadata{}, fmt.Errorf("%w: decode failed", ErrInvalidClientMetadata)
	}
	if err := validateMetadataShape(metadata, clientID, redirectURI); err != nil {
		return ClientMetadata{}, err
	}
	return metadata, nil
}

func validateClientIDURL(raw string, allowedOrigins []string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: client_id must be an absolute URL", ErrInvalidClientMetadata)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: client_id metadata URL must use HTTPS", ErrInvalidClientMetadata)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: client_id metadata URL must not include userinfo or fragment", ErrInvalidClientMetadata)
	}
	if len(allowedOrigins) == 0 {
		return nil, fmt.Errorf("%w: allowed client metadata origins are required", ErrInvalidClientMetadata)
	}
	if !originAllowed(parsed, allowedOrigins) {
		return nil, fmt.Errorf("%w: client_id metadata origin is not allowed", ErrInvalidClientMetadata)
	}
	return parsed, nil
}

func validateMetadataShape(metadata ClientMetadata, clientID string, redirectURI string) error {
	if metadata.ClientID != "" && metadata.ClientID != clientID {
		return fmt.Errorf("%w: metadata client_id mismatch", ErrInvalidClientMetadata)
	}
	if len(metadata.RedirectURIs) == 0 || !slices.Contains(metadata.RedirectURIs, redirectURI) {
		return fmt.Errorf("%w: redirect_uri is not registered by metadata", ErrInvalidClientMetadata)
	}
	if metadata.TokenEndpointAuthMethod != "" && metadata.TokenEndpointAuthMethod != "none" {
		return fmt.Errorf("%w: token_endpoint_auth_method must be none", ErrInvalidClientMetadata)
	}
	if len(metadata.ResponseTypes) > 0 && !slices.Contains(metadata.ResponseTypes, "code") {
		return fmt.Errorf("%w: response_types must include code", ErrInvalidClientMetadata)
	}
	if len(metadata.GrantTypes) > 0 && !slices.Contains(metadata.GrantTypes, "authorization_code") {
		return fmt.Errorf("%w: grant_types must include authorization_code", ErrInvalidClientMetadata)
	}
	return nil
}

func cloneClientWithSafeRedirects(client *http.Client, origin *url.URL) *http.Client {
	cloned := *client
	cloned.Jar = nil
	cloned.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		next := req.URL
		if next.Scheme != "https" || !sameOrigin(origin, next) {
			return errors.New("unsafe client metadata redirect")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		return nil
	}
	return &cloned
}

func originAllowed(u *url.URL, allowedOrigins []string) bool {
	for _, raw := range allowedOrigins {
		allowed, err := url.Parse(raw)
		if err != nil || allowed.Scheme == "" || allowed.Host == "" {
			continue
		}
		if sameOrigin(allowed, u) {
			return true
		}
	}
	return false
}

func sameOrigin(a *url.URL, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func defaultDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}
