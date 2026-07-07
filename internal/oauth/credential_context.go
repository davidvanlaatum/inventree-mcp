package oauth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

const tokenInfoCredentialKey = "inventree_mcp_credential"

var ErrCredentialUnavailable = errors.New("OAuth credential unavailable")

type credentialCarrier struct {
	credential Credential
}

func (credentialCarrier) String() string {
	return "[redacted]"
}

func (credentialCarrier) GoString() string {
	return "[redacted]"
}

func (credentialCarrier) LogValue() slog.Value {
	return slog.StringValue("[redacted]")
}

func (credentialCarrier) MarshalJSON() ([]byte, error) {
	return []byte(`"[redacted]"`), nil
}

func TokenInfoWithCredential(tokenInfo *auth.TokenInfo, credential Credential) *auth.TokenInfo {
	if tokenInfo == nil {
		tokenInfo = &auth.TokenInfo{}
	}
	extra := make(map[string]any, len(tokenInfo.Extra)+1)
	for key, value := range tokenInfo.Extra {
		extra[key] = value
	}
	extra[tokenInfoCredentialKey] = credentialCarrier{credential: credential}
	tokenInfo.Extra = extra
	return tokenInfo
}

func CredentialFromTokenInfo(tokenInfo *auth.TokenInfo) (Credential, error) {
	if tokenInfo == nil || tokenInfo.Extra == nil {
		return Credential{}, ErrCredentialUnavailable
	}
	carrier, ok := tokenInfo.Extra[tokenInfoCredentialKey].(credentialCarrier)
	if !ok {
		return Credential{}, ErrCredentialUnavailable
	}
	if err := carrier.credential.Validate(); err != nil {
		return Credential{}, ErrCredentialUnavailable
	}
	return carrier.credential, nil
}

func CredentialFromContext(ctx context.Context) (Credential, error) {
	return CredentialFromTokenInfo(auth.TokenInfoFromContext(ctx))
}
