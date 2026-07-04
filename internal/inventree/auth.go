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
)

type Credential struct {
	Scheme AuthScheme
	Token  string
}

func (c Credential) Validate() error {
	switch c.Scheme {
	case AuthSchemeToken, AuthSchemeBearer:
	default:
		return fmt.Errorf("InvenTree auth scheme must be %q or %q", AuthSchemeToken, AuthSchemeBearer)
	}
	if c.Token == "" {
		return errors.New("InvenTree token is required")
	}
	return nil
}

func (c Credential) Apply(req *http.Request) {
	req.Header.Set("Authorization", string(c.Scheme)+" "+c.Token)
}
