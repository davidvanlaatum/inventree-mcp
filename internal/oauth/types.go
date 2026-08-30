package oauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
)

const (
	TokenTypeAccess          = "access"
	TokenTypeRefresh         = "refresh"
	TokenTypeCode            = "authorization_code"
	TokenTypeBootstrapAccess = "bootstrap_access"

	DefaultAccessTokenLifetime  = 15 * time.Minute
	DefaultRefreshTokenLifetime = 30 * 24 * time.Hour
	DefaultSessionLifetime      = 90 * 24 * time.Hour
	DefaultCodeLifetime         = 5 * time.Minute

	CredentialSourceDedicated = "dedicated_token"
	CredentialSourceSupplied  = "supplied_token"
)

var (
	ErrInvalidClientMetadata = errors.New("invalid client metadata")
	ErrInvalidToken          = errors.New("invalid OAuth token")
	ErrInvalidCode           = errors.New("invalid authorization code")
)

type Credential struct {
	Scheme inventree.AuthScheme `json:"scheme"`
	Token  string               `json:"token"`
}

func (c Credential) Validate() error {
	return inventree.Credential{Scheme: c.Scheme, Token: c.Token}.Validate()
}

// ValidateForEnvelope validates c the same way Validate does, and
// additionally rejects inventree.AuthSchemeBasic. Basic credentials are only
// valid for transient bootstrap-validation calls; they must never be sealed
// into a long-lived MCP token envelope, since InvenTree Basic auth carries
// the user's raw password rather than a revocable API token.
func (c Credential) ValidateForEnvelope() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Scheme == inventree.AuthSchemeBasic {
		return fmt.Errorf("InvenTree auth scheme %q must not be sealed into a token envelope", inventree.AuthSchemeBasic)
	}
	return nil
}

type TokenClaims struct {
	Type             string     `json:"typ"`
	Issuer           string     `json:"iss"`
	Audience         string     `json:"aud"`
	Subject          string     `json:"sub"`
	ClientID         string     `json:"client_id"`
	Scopes           []string   `json:"scopes,omitempty"`
	IssuedAt         time.Time  `json:"iat"`
	ExpiresAt        time.Time  `json:"exp"`
	SessionExpiresAt time.Time  `json:"session_exp,omitempty"`
	Credential       Credential `json:"credential"`
	CredentialSource string     `json:"credential_source,omitempty"`
}

func (c TokenClaims) validateForUse(now time.Time, expectedType string, aad AssociatedData) error {
	if expectedType != "" && c.Type != expectedType {
		return ErrInvalidToken
	}
	if c.Issuer != aad.Issuer || c.Audience != aad.Audience || c.ClientID != aad.ClientID {
		return ErrInvalidToken
	}
	if c.Subject == "" || c.IssuedAt.IsZero() || c.IssuedAt.After(now) {
		return ErrInvalidToken
	}
	if !c.ExpiresAt.After(now) {
		return ErrInvalidToken
	}
	if !c.ExpiresAt.After(c.IssuedAt) {
		return ErrInvalidToken
	}
	if c.SessionExpiresAt.IsZero() || !c.SessionExpiresAt.After(now) || c.ExpiresAt.After(c.SessionExpiresAt) {
		return ErrInvalidToken
	}
	if err := c.Credential.ValidateForEnvelope(); err != nil {
		return ErrInvalidToken
	}
	return nil
}

type AuthorizationCodeClaims struct {
	Type                string     `json:"typ"`
	CodeID              string     `json:"code_id"`
	Issuer              string     `json:"iss"`
	Audience            string     `json:"aud"`
	Subject             string     `json:"sub"`
	ClientID            string     `json:"client_id"`
	RedirectURI         string     `json:"redirect_uri"`
	PKCEChallenge       string     `json:"pkce_challenge"`
	PKCEChallengeMethod string     `json:"pkce_challenge_method"`
	Scopes              []string   `json:"scopes,omitempty"`
	IssuedAt            time.Time  `json:"iat"`
	ExpiresAt           time.Time  `json:"exp"`
	Credential          Credential `json:"credential"`
	CredentialSource    string     `json:"credential_source,omitempty"`
}

func (c AuthorizationCodeClaims) validateForUse(now time.Time, aad AssociatedData, redirectURI string, pkceVerifier string) error {
	if c.Type != TokenTypeCode {
		return ErrInvalidCode
	}
	if c.Issuer != aad.Issuer || c.Audience != aad.Audience || c.ClientID != aad.ClientID {
		return ErrInvalidCode
	}
	if c.RedirectURI != redirectURI {
		return ErrInvalidCode
	}
	if c.CodeID == "" || c.PKCEChallenge == "" || c.PKCEChallengeMethod != "S256" {
		return ErrInvalidCode
	}
	if !verifyPKCES256(pkceVerifier, c.PKCEChallenge) {
		return ErrInvalidCode
	}
	if !c.ExpiresAt.After(now) {
		return ErrInvalidCode
	}
	if err := c.Credential.ValidateForEnvelope(); err != nil {
		return ErrInvalidCode
	}
	return nil
}

type AssociatedData struct {
	Issuer   string
	Audience string
	ClientID string
	Type     string
}
