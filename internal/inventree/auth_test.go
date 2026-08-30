package inventree

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		credential Credential
		wantErr    bool
	}{
		{name: "token", credential: Credential{Scheme: AuthSchemeToken, Token: "abc123"}},
		{name: "bearer", credential: Credential{Scheme: AuthSchemeBearer, Token: "abc123"}},
		{name: "basic", credential: Credential{Scheme: AuthSchemeBasic, Token: base64.StdEncoding.EncodeToString([]byte("user:pass"))}},
		{name: "empty token", credential: Credential{Scheme: AuthSchemeToken, Token: ""}, wantErr: true},
		{name: "unknown scheme", credential: Credential{Scheme: "Digest", Token: "abc123"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)
			err := tt.credential.Validate()
			if tt.wantErr {
				a.Error(err)
			} else {
				a.NoError(err)
			}
		})
	}
}

func TestCredentialApplyBasicScheme(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	encoded := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	credential := Credential{Scheme: AuthSchemeBasic, Token: encoded}

	req, err := http.NewRequest(http.MethodGet, "https://inventory.example.test/api/user/me/", nil)
	r.NoError(err)

	credential.Apply(req)

	r.Equal("Basic "+encoded, req.Header.Get("Authorization"))
}
