package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseServeUsesEnvAndFlagPrecedence(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	cfg, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://flag.example.test",
		"--inventree-auth-scheme", "Bearer",
		"--inventree-timeout", "5s",
	}, mapEnv(map[string]string{
		EnvInvenTreeURL:        "https://env.example.test",
		EnvInvenTreeToken:      "env-token",
		EnvInvenTreeAuthScheme: "Token",
		EnvInvenTreeTimeout:    "10s",
	}), nil)
	r.NoError(err)
	r.Equal("https://flag.example.test", cfg.InvenTreeURL)
	r.Equal("env-token", cfg.InvenTreeToken)
	r.Equal(AuthSchemeBearer, cfg.InvenTreeAuthScheme)
	r.Equal(5*time.Second, cfg.InvenTreeTimeout)
	r.Equal(DefaultListen, cfg.Listen)
}

func TestParseServeConfiguresUploadPolicy(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	cfg, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
		"--upload-allow-root", "/flag/uploads",
		"--upload-max-bytes", "2048",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken:   "token",
		EnvUploadAllowRoots: "/env/one" + string(os.PathListSeparator) + "/env/two",
		EnvUploadMaxBytes:   "1024",
	}), nil)
	r.NoError(err)
	r.Equal([]string{"/env/one", "/env/two", "/flag/uploads"}, cfg.UploadAllowRoots)
	r.Equal(int64(2048), cfg.UploadMaxBytes)
}

func TestParseServeRejectsMissingStdioRequiredValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ParseServeWithEnv([]string{"--transport", "stdio"}, mapEnv(nil), nil)
	r.Error(err)

	a.Contains(err.Error(), "InvenTree URL is required")
	a.Contains(err.Error(), "InvenTree token is required for STDIO transport")
}

func TestParseServeRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "websocket",
		"--environment", "stage",
		"--inventree-url", "not-a-url",
		"--inventree-auth-scheme", "Basic",
		"--inventree-timeout", "0s",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil)
	r.Error(err)

	a.Contains(err.Error(), "transport must be")
	a.Contains(err.Error(), "environment must be")
	a.Contains(err.Error(), "InvenTree URL must be an absolute URL")
	a.Contains(err.Error(), "InvenTree auth scheme must be")
	a.Contains(err.Error(), "InvenTree timeout must be greater than zero")
}

func TestParseServeRejectsNonHTTPInvenTreeURL(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "ftp://inventory.example.test",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil)
	r.Error(err)
	r.Contains(err.Error(), "InvenTree URL scheme must be http or https")
}

func TestParseServeRejectsInvalidEnvDuration(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken:   "token",
		EnvInvenTreeTimeout: "not-a-duration",
	}), nil)
	r.Error(err)
	r.Contains(err.Error(), "InvenTree timeout must be greater than zero")
}

func TestParseServeRejectsProductionTLSSkipVerifyForAllTransports(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-tls-skip-verify",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil)
	r.Error(err)
	r.Contains(err.Error(), "production mode rejects InvenTree TLS skip verify")
}

func TestParseServeRejectsProductionHTTPBeforeOAuth(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-tls-skip-verify",
	}, mapEnv(nil), nil)
	r.Error(err)

	a.Contains(err.Error(), "production mode rejects InvenTree TLS skip verify")
	a.Contains(err.Error(), "OAuth issuer URL is required")
	a.Contains(err.Error(), "OAuth resource URL is required")
	a.Contains(err.Error(), "at least one OAuth client ID is required")
	a.Contains(err.Error(), "OAuth keyring requires at least one key")
}

func TestParseServeAllowsProductionHTTPWithOAuthConfig(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	cfg, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--inventree-url", "https://inventory.example.test",
		"--oauth-client-id", "https://chatgpt.com/client-metadata/b",
		"--oauth-key", "next:decrypt_only:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
	}, mapEnv(map[string]string{
		EnvOAuthIssuerURL:       "https://auth.example.test",
		EnvOAuthResourceURL:     "https://mcp.example.test/mcp",
		EnvOAuthClientIDs:       "https://chatgpt.com/client-metadata/a",
		EnvOAuthKeys:            "current:active:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
		EnvOAuthAccessLifetime:  "10m",
		EnvOAuthRefreshLifetime: "24h",
		EnvOAuthSessionLifetime: "720h",
	}), nil)
	r.NoError(err)

	a.Equal(TransportHTTP, cfg.Transport)
	a.Equal(EnvironmentProduction, cfg.Environment)
	a.Equal("https://mcp.example.test/.well-known/oauth-protected-resource", cfg.OAuthProtectedResourceMetadataURL())
	a.Equal([]string{"https://chatgpt.com/client-metadata/a", "https://chatgpt.com/client-metadata/b"}, cfg.OAuthClientIDs)
	a.Len(cfg.OAuthKeyring.Keys, 2)
	a.Equal(10*time.Minute, cfg.OAuthAccessLifetime)
	a.Equal(24*time.Hour, cfg.OAuthRefreshLifetime)
	a.Equal(720*time.Hour, cfg.OAuthSessionLifetime)
}

func TestParseServeRejectsInvalidProductionOAuthConfig(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
		"--oauth-issuer-url", "http://mcp.example.test",
		"--oauth-resource-url", "https://mcp.example.test/mcp?debug=true",
		"--oauth-client-id", "http://chatgpt.example.test/client",
		"--oauth-client-id", "https://user:pass@chatgpt.example.test/client",
		"--oauth-key", "bad:active:not-base64",
		"--oauth-access-lifetime", "48h",
		"--oauth-refresh-lifetime", "24h",
		"--oauth-session-lifetime", "12h",
	}, mapEnv(nil), nil)
	r.Error(err)

	a.Contains(err.Error(), "production HTTP mode rejects --dev-incomplete-oauth")
	a.Contains(err.Error(), "OAuth issuer URL must use https")
	a.Contains(err.Error(), "OAuth resource URL must not include query or fragment")
	a.Contains(err.Error(), "OAuth client ID must use https")
	a.Contains(err.Error(), "OAuth client ID must not include userinfo")
	a.Contains(err.Error(), `OAuth key "bad" must be 32 bytes`)
	a.Contains(err.Error(), "OAuth access token lifetime must be shorter than refresh token lifetime")
	a.Contains(err.Error(), "OAuth refresh token lifetime must not exceed session lifetime")
}

func TestParseServeRedactsMalformedOAuthKeyEnv(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--inventree-url", "https://inventory.example.test",
		"--oauth-issuer-url", "https://auth.example.test",
		"--oauth-resource-url", "https://mcp.example.test/mcp",
		"--oauth-client-id", "https://chatgpt.com/client-metadata",
	}, mapEnv(map[string]string{
		EnvOAuthKeys: "broken:active:super-secret-key-material:extra",
	}), nil)
	r.Error(err)

	a.Contains(err.Error(), `OAuth key "invalid_oauth_key_entry_1"`)
	a.NotContains(err.Error(), "super-secret-key-material")
	a.NotContains(err.Error(), "broken:active")
}

func TestParseServeAllowsDevelopmentHTTPOnlyWithExplicitIncompleteOAuthFlag(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(nil), nil)
	r.Error(err)
	r.Contains(err.Error(), "development HTTP mode requires --dev-incomplete-oauth")

	cfg, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(nil), nil)
	r.NoError(err)
	r.Equal(TransportHTTP, cfg.Transport)
}

func TestParseServeRejectsInvalidHTTPRequiredValues(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--listen", "",
		"--path", "",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(nil), nil)
	r.Error(err)

	a.Contains(err.Error(), "HTTP path must start with /")
	a.Contains(err.Error(), "HTTP listen address is required")
}

func TestParseServeRejectsHTTPConfiguredToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken: "raw-token",
	}), nil)
	r.Error(err)
	r.Contains(err.Error(), "configured InvenTree tokens are STDIO-only")
}

func TestParseServeRejectsTokenFlag(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-token", "raw-token",
	}, mapEnv(nil), nil)
	r.Error(err)
	r.Contains(err.Error(), "flag provided but not defined: -inventree-token")
}

func TestParseServeRejectsHTTPConfiguredAuthScheme(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-auth-scheme", "Bearer",
	}, mapEnv(nil), nil)
	r.Error(err)
	r.Contains(err.Error(), "configured InvenTree auth schemes are STDIO-only")
}

func TestParseServeHelpMentionsEnvVars(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var output strings.Builder
	_, err := ParseServeWithEnv([]string{"--help"}, mapEnv(nil), &output)
	r.Error(err)

	help := output.String()
	for _, envVar := range []string{
		EnvTransport,
		EnvEnvironment,
		EnvListen,
		EnvPath,
		EnvInvenTreeURL,
		EnvInvenTreeAuthScheme,
		EnvInvenTreeTimeout,
		EnvInvenTreeTLSSkipVerify,
		EnvLogLevel,
		EnvDevIncompleteOAuth,
		EnvOAuthIssuerURL,
		EnvOAuthResourceURL,
		EnvOAuthKeys,
		EnvOAuthClientIDs,
		EnvOAuthAccessLifetime,
		EnvOAuthRefreshLifetime,
		EnvOAuthSessionLifetime,
	} {
		a.Contains(help, envVar)
	}
	a.NotContains(help, EnvInvenTreeToken)
}

func mapEnv(values map[string]string) Env {
	return func(key string) string {
		return values[key]
	}
}
