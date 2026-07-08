package server

import (
	"context"
	"net/http"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
)

func OAuthClientFromContext(baseURL string, httpClient *http.Client) func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		credential, err := oauth.CredentialFromContext(ctx)
		if err != nil {
			return nil, err
		}
		return inventree.NewClient(inventree.Config{
			BaseURL: baseURL,
			Credential: inventree.Credential{
				Scheme: credential.Scheme,
				Token:  credential.Token,
			},
			HTTPClient: httpClient,
		})
	}
}
