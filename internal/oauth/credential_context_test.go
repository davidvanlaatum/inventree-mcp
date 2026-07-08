package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialFromTokenInfoUsesPrivateExtraCarrier(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	tokenInfo := TokenInfoWithCredential(&auth.TokenInfo{
		UserID: "operator-1",
		Extra:  map[string]any{"tenant": "main"},
	}, Credential{Scheme: inventree.AuthSchemeToken, Token: "secret-inventree-token"})

	credential, err := CredentialFromTokenInfo(tokenInfo)
	r.NoError(err)
	a.Equal(inventree.AuthSchemeToken, credential.Scheme)
	a.Equal("secret-inventree-token", credential.Token)
	a.Equal("main", tokenInfo.Extra["tenant"])
	a.NotContains(tokenInfo.Extra, "credential")
	a.NotContains(tokenInfo.Extra, "token")
}

func TestCredentialFromTokenInfoDoesNotMarshalByDefault(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	tokenInfo := TokenInfoWithCredential(&auth.TokenInfo{
		UserID: "operator-1",
	}, Credential{Scheme: inventree.AuthSchemeToken, Token: "secret-inventree-token"})

	payload, err := json.Marshal(tokenInfo.Extra)
	r.NoError(err)
	a.NotContains(string(payload), "secret-inventree-token")
	a.Contains(string(payload), "redacted")
}

func TestCredentialCarrierRedactsFormattingAndLogs(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	tokenInfo := TokenInfoWithCredential(&auth.TokenInfo{
		UserID: "operator-1",
	}, Credential{Scheme: inventree.AuthSchemeToken, Token: "secret-inventree-token"})

	for _, formatted := range []string{
		fmt.Sprintf("%v", tokenInfo.Extra),
		fmt.Sprintf("%+v", tokenInfo.Extra),
		fmt.Sprintf("%#v", tokenInfo.Extra),
	} {
		a.NotContains(formatted, "secret-inventree-token")
		a.Contains(formatted, "redacted")
	}

	var textLog bytes.Buffer
	textLogger := slog.New(slog.NewTextHandler(&textLog, nil))
	textLogger.Info("credential", slog.Any("extra", tokenInfo.Extra[tokenInfoCredentialKey]))
	a.NotContains(textLog.String(), "secret-inventree-token")
	a.Contains(textLog.String(), "redacted")

	var jsonLog bytes.Buffer
	jsonLogger := slog.New(slog.NewJSONHandler(&jsonLog, nil))
	jsonLogger.Info("credential", slog.Any("extra", tokenInfo.Extra[tokenInfoCredentialKey]))
	a.NotContains(jsonLog.String(), "secret-inventree-token")
	a.Contains(jsonLog.String(), "redacted")
}

func TestCredentialFromContextRejectsMissingCredential(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := CredentialFromContext(context.Background())
	r.ErrorIs(err, ErrCredentialUnavailable)
}
