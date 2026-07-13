package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
)

const (
	EnvTransport              = "INVENTREE_MCP_TRANSPORT"
	EnvEnvironment            = "INVENTREE_MCP_ENVIRONMENT"
	EnvListen                 = "INVENTREE_MCP_LISTEN"
	EnvPath                   = "INVENTREE_MCP_PATH"
	EnvInvenTreeURL           = "INVENTREE_URL"
	EnvInvenTreeToken         = "INVENTREE_TOKEN"
	EnvInvenTreeAuthScheme    = "INVENTREE_AUTH_SCHEME"
	EnvInvenTreeTimeout       = "INVENTREE_TIMEOUT"
	EnvInvenTreeTLSSkipVerify = "INVENTREE_TLS_SKIP_VERIFY"
	EnvUploadAllowRoots       = "INVENTREE_UPLOAD_ALLOW_ROOTS"
	EnvUploadMaxBytes         = "INVENTREE_UPLOAD_MAX_BYTES"
	EnvLogLevel               = "INVENTREE_MCP_LOG_LEVEL"
	EnvDevIncompleteOAuth     = "INVENTREE_MCP_DEV_INCOMPLETE_OAUTH"
	EnvOAuthIssuerURL         = "INVENTREE_MCP_OAUTH_ISSUER_URL"
	EnvOAuthResourceURL       = "INVENTREE_MCP_OAUTH_RESOURCE_URL"
	EnvOAuthKeys              = "INVENTREE_MCP_OAUTH_KEYS"
	EnvOAuthClientIDs         = "INVENTREE_MCP_OAUTH_CLIENT_IDS"
	EnvOAuthAccessLifetime    = "INVENTREE_MCP_OAUTH_ACCESS_LIFETIME"
	EnvOAuthRefreshLifetime   = "INVENTREE_MCP_OAUTH_REFRESH_LIFETIME"
	EnvOAuthSessionLifetime   = "INVENTREE_MCP_OAUTH_SESSION_LIFETIME"

	invalidDuration = time.Duration(-1)
	DefaultListen   = "127.0.0.1:28686"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
)

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

type AuthScheme string

const (
	AuthSchemeToken  AuthScheme = "Token"
	AuthSchemeBearer AuthScheme = "Bearer"
)

type Config struct {
	Transport              Transport
	Environment            Environment
	Listen                 string
	Path                   string
	InvenTreeURL           string
	InvenTreeToken         string
	InvenTreeAuthScheme    AuthScheme
	InvenTreeTimeout       time.Duration
	InvenTreeTLSSkipVerify bool
	UploadAllowRoots       []string
	UploadMaxBytes         int64
	LogLevel               string
	DevIncompleteOAuth     bool
	OAuthIssuerURL         string
	OAuthResourceURL       string
	OAuthKeyring           oauth.KeyringConfig
	OAuthClientIDs         []string
	OAuthAccessLifetime    time.Duration
	OAuthRefreshLifetime   time.Duration
	OAuthSessionLifetime   time.Duration
}

type Env func(string) string

func ParseServe(args []string) (Config, error) {
	return ParseServeWithEnv(args, os.Getenv, io.Discard)
}

func ParseServeWithEnv(args []string, getenv Env, output io.Writer) (Config, error) {
	if output == nil {
		output = io.Discard
	}

	cfg := Config{
		Transport:           Transport(envDefault(getenv, EnvTransport, string(TransportStdio))),
		Environment:         Environment(envDefault(getenv, EnvEnvironment, string(EnvironmentProduction))),
		Listen:              envDefault(getenv, EnvListen, DefaultListen),
		Path:                envDefault(getenv, EnvPath, "/mcp"),
		InvenTreeURL:        getenv(EnvInvenTreeURL),
		InvenTreeToken:      getenv(EnvInvenTreeToken),
		InvenTreeAuthScheme: AuthScheme(envDefault(getenv, EnvInvenTreeAuthScheme, string(AuthSchemeToken))),
		InvenTreeTimeout:    durationDefault(getenv, EnvInvenTreeTimeout, 30*time.Second),
		UploadAllowRoots:    listEnv(getenv, EnvUploadAllowRoots),
		UploadMaxBytes:      int64Default(getenv, EnvUploadMaxBytes, 5*1024*1024),
		LogLevel:            envDefault(getenv, EnvLogLevel, "info"),
		OAuthIssuerURL:      getenv(EnvOAuthIssuerURL),
		OAuthResourceURL:    getenv(EnvOAuthResourceURL),
		OAuthKeyring:        oauth.KeyringConfig{Keys: keyListEnv(getenv, EnvOAuthKeys)},
		OAuthClientIDs:      commaListEnv(getenv, EnvOAuthClientIDs),
		OAuthAccessLifetime: durationDefault(getenv, EnvOAuthAccessLifetime, oauth.DefaultAccessTokenLifetime),
		OAuthRefreshLifetime: durationDefault(
			getenv,
			EnvOAuthRefreshLifetime,
			oauth.DefaultRefreshTokenLifetime,
		),
		OAuthSessionLifetime: durationDefault(getenv, EnvOAuthSessionLifetime, oauth.DefaultSessionLifetime),
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar((*string)(&cfg.Transport), "transport", string(cfg.Transport), flagHelp("transport to serve: stdio or http", EnvTransport))
	fs.StringVar((*string)(&cfg.Environment), "environment", string(cfg.Environment), flagHelp("runtime environment: development or production", EnvEnvironment))
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, flagHelp("HTTP listen address", EnvListen))
	fs.StringVar(&cfg.Path, "path", cfg.Path, flagHelp("HTTP MCP path", EnvPath))
	fs.StringVar(&cfg.InvenTreeURL, "inventree-url", cfg.InvenTreeURL, flagHelp("InvenTree base URL", EnvInvenTreeURL))
	fs.StringVar((*string)(&cfg.InvenTreeAuthScheme), "inventree-auth-scheme", string(cfg.InvenTreeAuthScheme), flagHelp("InvenTree auth scheme: Token or Bearer", EnvInvenTreeAuthScheme))
	fs.DurationVar(&cfg.InvenTreeTimeout, "inventree-timeout", cfg.InvenTreeTimeout, flagHelp("InvenTree request timeout", EnvInvenTreeTimeout))
	fs.BoolVar(&cfg.InvenTreeTLSSkipVerify, "inventree-tls-skip-verify", boolEnv(getenv, EnvInvenTreeTLSSkipVerify), flagHelp("skip upstream InvenTree TLS verification", EnvInvenTreeTLSSkipVerify))
	fs.Func("upload-allow-root", flagHelp("trusted STDIO local upload root; repeatable", EnvUploadAllowRoots), func(value string) error {
		value = strings.TrimSpace(value)
		if value != "" {
			cfg.UploadAllowRoots = append(cfg.UploadAllowRoots, value)
		}
		return nil
	})
	fs.Int64Var(&cfg.UploadMaxBytes, "upload-max-bytes", cfg.UploadMaxBytes, flagHelp("maximum bytes accepted from one upload source", EnvUploadMaxBytes))
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, flagHelp("log level", EnvLogLevel))
	fs.BoolVar(&cfg.DevIncompleteOAuth, "dev-incomplete-oauth", boolEnv(getenv, EnvDevIncompleteOAuth), flagHelp("allow development-only HTTP parsing before OAuth startup wiring is available", EnvDevIncompleteOAuth))
	fs.StringVar(&cfg.OAuthIssuerURL, "oauth-issuer-url", cfg.OAuthIssuerURL, flagHelp("public HTTPS OAuth issuer URL", EnvOAuthIssuerURL))
	fs.StringVar(&cfg.OAuthResourceURL, "oauth-resource-url", cfg.OAuthResourceURL, flagHelp("public HTTPS MCP resource URL", EnvOAuthResourceURL))
	fs.Func("oauth-key", flagHelp("OAuth envelope key as key-id:active|decrypt_only:base64-32-byte-key; repeatable", EnvOAuthKeys), func(value string) error {
		key, err := parseKeyConfig(value)
		if err != nil {
			return err
		}
		cfg.OAuthKeyring.Keys = append(cfg.OAuthKeyring.Keys, key)
		return nil
	})
	fs.Func("oauth-client-id", flagHelp("allowed OAuth client_id metadata URL; repeatable", EnvOAuthClientIDs), func(value string) error {
		value = strings.TrimSpace(value)
		if value != "" {
			cfg.OAuthClientIDs = append(cfg.OAuthClientIDs, value)
		}
		return nil
	})
	fs.DurationVar(&cfg.OAuthAccessLifetime, "oauth-access-lifetime", cfg.OAuthAccessLifetime, flagHelp("OAuth access token lifetime", EnvOAuthAccessLifetime))
	fs.DurationVar(&cfg.OAuthRefreshLifetime, "oauth-refresh-lifetime", cfg.OAuthRefreshLifetime, flagHelp("OAuth refresh token lifetime", EnvOAuthRefreshLifetime))
	fs.DurationVar(&cfg.OAuthSessionLifetime, "oauth-session-lifetime", cfg.OAuthSessionLifetime, flagHelp("OAuth maximum connector session lifetime", EnvOAuthSessionLifetime))

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var validationErrors []error

	switch c.Transport {
	case TransportStdio, TransportHTTP:
	default:
		validationErrors = append(validationErrors, fmt.Errorf("transport must be %q or %q", TransportStdio, TransportHTTP))
	}

	switch c.Environment {
	case EnvironmentDevelopment, EnvironmentProduction:
	default:
		validationErrors = append(validationErrors, fmt.Errorf("environment must be %q or %q", EnvironmentDevelopment, EnvironmentProduction))
	}

	if c.InvenTreeURL == "" {
		validationErrors = append(validationErrors, errors.New("InvenTree URL is required"))
	} else if parsed, err := url.ParseRequestURI(c.InvenTreeURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		validationErrors = append(validationErrors, errors.New("InvenTree URL must be an absolute URL"))
	} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
		validationErrors = append(validationErrors, errors.New("InvenTree URL scheme must be http or https"))
	}

	switch c.InvenTreeAuthScheme {
	case AuthSchemeToken, AuthSchemeBearer:
	default:
		validationErrors = append(validationErrors, fmt.Errorf("InvenTree auth scheme must be %q or %q", AuthSchemeToken, AuthSchemeBearer))
	}

	if c.InvenTreeTimeout <= 0 {
		validationErrors = append(validationErrors, errors.New("InvenTree timeout must be greater than zero"))
	}

	if c.InvenTreeTLSSkipVerify && c.Environment == EnvironmentProduction {
		validationErrors = append(validationErrors, errors.New("production mode rejects InvenTree TLS skip verify"))
	}

	if c.UploadMaxBytes <= 0 {
		validationErrors = append(validationErrors, errors.New("upload max bytes must be greater than zero"))
	}

	if c.Transport == TransportStdio {
		if c.InvenTreeToken == "" {
			validationErrors = append(validationErrors, errors.New("InvenTree token is required for STDIO transport"))
		}
	}

	if c.Transport == TransportHTTP {
		if c.Path == "" || !strings.HasPrefix(c.Path, "/") {
			validationErrors = append(validationErrors, errors.New("HTTP path must start with /"))
		}
		if c.Listen == "" {
			validationErrors = append(validationErrors, errors.New("HTTP listen address is required"))
		}
		if c.InvenTreeToken != "" {
			validationErrors = append(validationErrors, errors.New("configured InvenTree tokens are STDIO-only until HTTP OAuth startup wiring is available"))
		}
		if c.InvenTreeAuthScheme != AuthSchemeToken {
			validationErrors = append(validationErrors, errors.New("configured InvenTree auth schemes are STDIO-only until HTTP OAuth startup wiring is available"))
		}
		if c.Environment == EnvironmentProduction {
			validationErrors = append(validationErrors, c.validateProductionHTTP()...)
		}
		if c.Environment == EnvironmentDevelopment && !c.DevIncompleteOAuth {
			validationErrors = append(validationErrors, errors.New("development HTTP mode requires --dev-incomplete-oauth until OAuth startup and setup wiring is available"))
		}
	}

	return errors.Join(validationErrors...)
}

func (c Config) validateProductionHTTP() []error {
	var validationErrors []error
	if c.DevIncompleteOAuth {
		validationErrors = append(validationErrors, errors.New("production HTTP mode rejects --dev-incomplete-oauth"))
	}
	if err := validateHTTPSURL(c.OAuthIssuerURL, "OAuth issuer URL"); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if err := validateHTTPSURL(c.OAuthResourceURL, "OAuth resource URL"); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if len(c.OAuthClientIDs) == 0 {
		validationErrors = append(validationErrors, errors.New("at least one OAuth client ID is required for production HTTP"))
	}
	for _, clientID := range c.OAuthClientIDs {
		if err := validateHTTPSURL(clientID, "OAuth client ID"); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}
	if _, err := c.OAuthKeyring.Keyring(); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if c.OAuthAccessLifetime <= 0 {
		validationErrors = append(validationErrors, errors.New("OAuth access token lifetime must be greater than zero"))
	}
	if c.OAuthRefreshLifetime <= 0 {
		validationErrors = append(validationErrors, errors.New("OAuth refresh token lifetime must be greater than zero"))
	}
	if c.OAuthSessionLifetime <= 0 {
		validationErrors = append(validationErrors, errors.New("OAuth session lifetime must be greater than zero"))
	}
	if c.OAuthAccessLifetime > 0 && c.OAuthRefreshLifetime > 0 && c.OAuthAccessLifetime >= c.OAuthRefreshLifetime {
		validationErrors = append(validationErrors, errors.New("OAuth access token lifetime must be shorter than refresh token lifetime"))
	}
	if c.OAuthRefreshLifetime > 0 && c.OAuthSessionLifetime > 0 && c.OAuthRefreshLifetime > c.OAuthSessionLifetime {
		validationErrors = append(validationErrors, errors.New("OAuth refresh token lifetime must not exceed session lifetime"))
	}
	return validationErrors
}

func (c Config) OAuthProtectedResourceMetadataURL() string {
	if c.OAuthResourceURL == "" {
		return ""
	}
	parsed, err := url.Parse(c.OAuthResourceURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/.well-known/oauth-protected-resource"
}

func validateHTTPSURL(raw string, label string) error {
	if raw == "" {
		return fmt.Errorf("%s is required", label)
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", label)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", label)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include userinfo", label)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not include query or fragment", label)
	}
	return nil
}

func envDefault(getenv Env, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationDefault(getenv Env, key string, fallback time.Duration) time.Duration {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return invalidDuration
	}
	return parsed
}

func boolEnv(getenv Env, key string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(key))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func listEnv(getenv Env, key string) []string {
	raw := getenv(key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func keyListEnv(getenv Env, key string) []oauth.KeyConfig {
	raw := getenv(key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	keys := make([]oauth.KeyConfig, 0, len(parts))
	for index, part := range parts {
		key, err := parseKeyConfig(part)
		if err == nil {
			keys = append(keys, key)
			continue
		}
		keys = append(keys, oauth.KeyConfig{ID: fmt.Sprintf("invalid_oauth_key_entry_%d", index+1)})
	}
	return keys
}

func commaListEnv(getenv Env, key string) []string {
	raw := getenv(key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseKeyConfig(raw string) (oauth.KeyConfig, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 3 {
		return oauth.KeyConfig{}, errors.New("OAuth key must use key-id:active|decrypt_only:base64-32-byte-key")
	}
	id := strings.TrimSpace(parts[0])
	state := oauth.KeyState(strings.TrimSpace(parts[1]))
	material := strings.TrimSpace(parts[2])
	if id == "" || material == "" {
		return oauth.KeyConfig{}, errors.New("OAuth key ID and material are required")
	}
	switch state {
	case oauth.KeyStateActive, oauth.KeyStateDecryptOnly:
	default:
		return oauth.KeyConfig{}, fmt.Errorf("OAuth key state must be %q or %q", oauth.KeyStateActive, oauth.KeyStateDecryptOnly)
	}
	return oauth.KeyConfig{ID: id, State: state, MaterialBase64: material}, nil
}

func int64Default(getenv Env, key string, fallback int64) int64 {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback
	}
	var value int64
	if _, err := fmt.Sscan(raw, &value); err != nil {
		return -1
	}
	return value
}

func flagHelp(description string, envVar string) string {
	return fmt.Sprintf("%s (env: %s)", description, envVar)
}
