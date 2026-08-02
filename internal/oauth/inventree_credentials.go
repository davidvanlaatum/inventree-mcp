package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
)

var (
	ErrInvalidUpstreamCredential = errors.New("InvenTree credential validation failed")
	ErrDedicatedTokenUnavailable = errors.New("dedicated InvenTree token unavailable")
)

type CredentialBroker interface {
	ValidateCredential(context.Context, Credential) (string, error)
	CreateDedicatedCredential(context.Context, Credential, string) (Credential, error)
}

type InvenTreeCredentialBroker struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (b InvenTreeCredentialBroker) ValidateCredential(ctx context.Context, credential Credential) (string, error) {
	client, err := b.client(credential)
	if err != nil {
		return "", ErrInvalidUpstreamCredential
	}
	user, err := client.GetCurrentUser(ctx)
	if err != nil || user.PK <= 0 || strings.TrimSpace(user.Username) == "" {
		return "", ErrInvalidUpstreamCredential
	}
	return fmt.Sprintf("inventree-user:%d:%s", user.PK, user.Username), nil
}

func (b InvenTreeCredentialBroker) CreateDedicatedCredential(ctx context.Context, credential Credential, tokenName string) (Credential, error) {
	client, err := b.client(credential)
	if err != nil {
		return Credential{}, ErrDedicatedTokenUnavailable
	}
	name := strings.TrimSpace(tokenName)
	if name == "" || len(name) > 100 {
		return Credential{}, ErrDedicatedTokenUnavailable
	}
	token, err := client.CreateCurrentUserToken(ctx, name)
	if err != nil || strings.TrimSpace(token.Token) == "" {
		return Credential{}, ErrDedicatedTokenUnavailable
	}
	return Credential{Scheme: inventree.AuthSchemeToken, Token: token.Token}, nil
}

func (b InvenTreeCredentialBroker) client(credential Credential) (*inventree.Client, error) {
	if err := credential.Validate(); err != nil {
		return nil, err
	}
	return inventree.NewClient(inventree.Config{
		BaseURL: b.BaseURL,
		Credential: inventree.Credential{
			Scheme: credential.Scheme,
			Token:  credential.Token,
		},
		HTTPClient: b.HTTPClient,
	})
}
