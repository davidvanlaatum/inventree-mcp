package inventree

import (
	"errors"
	"fmt"
	"net/http"
)

type AuthScheme string

const (
	AuthSchemeToken  AuthScheme = "Token"
	AuthSchemeBearer AuthScheme = "Bearer"
	// AuthSchemeBasic is for transient outbound calls that validate a
	// user-supplied credential (e.g. bearer bootstrap). Token must already
	// be the base64-encoded "user:pass" value; Apply does not encode it.
	// Never seal a Basic credential into a long-lived MCP token envelope.
	AuthSchemeBasic AuthScheme = "Basic"
)

type Credential struct {
	Scheme AuthScheme
	Token  string
}

func (c Credential) Validate() error {
	switch c.Scheme {
	case AuthSchemeToken, AuthSchemeBearer, AuthSchemeBasic:
	default:
		return fmt.Errorf("InvenTree auth scheme must be %q, %q, or %q", AuthSchemeToken, AuthSchemeBearer, AuthSchemeBasic)
	}
	if c.Token == "" {
		return errors.New("InvenTree token is required")
	}
	return nil
}

func (c Credential) Apply(req *http.Request) {
	req.Header.Set("Authorization", string(c.Scheme)+" "+c.Token)
}
