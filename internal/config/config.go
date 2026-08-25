package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
	"github.com/spf13/afero"
)

const (
	EnvTransport              = "INVENTREE_MCP_TRANSPORT"
	EnvEnvironment            = "INVENTREE_MCP_ENVIRONMENT"
	EnvListen                 = "INVENTREE_MCP_LISTEN"
	EnvPath                   = "INVENTREE_MCP_PATH"
	EnvInvenTreeURL           = "INVENTREE_URL"
	EnvInvenTreeWebURL        = "INVENTREE_WEB_URL"
	EnvInvenTreeToken         = "INVENTREE_TOKEN"
	EnvInvenTreeAuthScheme    = "INVENTREE_AUTH_SCHEME"
	EnvInvenTreeTimeout       = "INVENTREE_TIMEOUT"
	EnvInvenTreeTLSSkipVerify = "INVENTREE_TLS_SKIP_VERIFY"
	EnvUploadAllowRoots       = "INVENTREE_UPLOAD_ALLOW_ROOTS"
	EnvUploadMaxBytes         = "INVENTREE_UPLOAD_MAX_BYTES"
	EnvMCPMaxRequestBodyBytes = "INVENTREE_MCP_MAX_REQUEST_BODY_BYTES"
	EnvLogLevel               = "INVENTREE_MCP_LOG_LEVEL"
	EnvDebugTrafficLog        = "INVENTREE_MCP_DEBUG_TRAFFIC_LOG"
	EnvDevIncompleteOAuth     = "INVENTREE_MCP_DEV_INCOMPLETE_OAUTH"
	EnvOAuthIssuerURL         = "INVENTREE_MCP_OAUTH_ISSUER_URL"
	EnvOAuthResourceURL       = "INVENTREE_MCP_OAUTH_RESOURCE_URL"
	EnvOAuthKeys              = "INVENTREE_MCP_OAUTH_KEYS"
	EnvOAuthClientIDs         = "INVENTREE_MCP_OAUTH_CLIENT_IDS"
	EnvTrustedProxyCIDRs      = "INVENTREE_MCP_TRUSTED_PROXY_CIDRS"
	EnvOAuthAccessLifetime    = "INVENTREE_MCP_OAUTH_ACCESS_LIFETIME"
	EnvOAuthRefreshLifetime   = "INVENTREE_MCP_OAUTH_REFRESH_LIFETIME"
	EnvOAuthSessionLifetime   = "INVENTREE_MCP_OAUTH_SESSION_LIFETIME"
	EnvOTelEnabled            = "INVENTREE_MCP_OTEL_ENABLED"
	EnvOTelServiceName        = "INVENTREE_MCP_OTEL_SERVICE_NAME"
	EnvOTelExporter           = "INVENTREE_MCP_OTEL_EXPORTER"
	EnvOTelEndpoint           = "INVENTREE_MCP_OTEL_ENDPOINT"
	EnvOTelInsecure           = "INVENTREE_MCP_OTEL_INSECURE"
	EnvOTelHeaders            = "INVENTREE_MCP_OTEL_HEADERS"
	EnvOTelSampleRatio        = "INVENTREE_MCP_OTEL_SAMPLE_RATIO"
	EnvOTelBatchTimeout       = "INVENTREE_MCP_OTEL_BATCH_TIMEOUT"
	EnvOTelExportTimeout      = "INVENTREE_MCP_OTEL_EXPORT_TIMEOUT"

	invalidDuration               = time.Duration(-1)
	DefaultListen                 = "127.0.0.1:28686"
	DefaultMCPMaxRequestBodyBytes = int64(8 * 1024 * 1024)
	mcpRequestBodyOverheadBytes   = int64(1024 * 1024)
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
	InvenTreeWebURL        string
	InvenTreeToken         string
	InvenTreeAuthScheme    AuthScheme
	InvenTreeTimeout       time.Duration
	InvenTreeTLSSkipVerify bool
	UploadAllowRoots       []string
	UploadMaxBytes         int64
	MCPMaxRequestBodyBytes int64
	LogLevel               string
	DebugTrafficLog        string
	DevIncompleteOAuth     bool
	OAuthIssuerURL         string
	OAuthResourceURL       string
	OAuthKeyring           oauth.KeyringConfig
	OAuthClientIDs         []string
	TrustedProxyCIDRs      []string
	OAuthAccessLifetime    time.Duration
	OAuthRefreshLifetime   time.Duration
	OAuthSessionLifetime   time.Duration
	Telemetry              telemetry.Config
}

type Env func(string) string

func ParseServe(args []string) (Config, error) {
	return ParseServeWithEnv(args, os.Getenv, io.Discard)
}

func ParseServeWithEnv(args []string, getenv Env, output io.Writer) (Config, error) {
	return parseServeWithDeps(args, getenv, output, afero.NewOsFs(), os.UserConfigDir)
}

func parseServeWithDeps(args []string, getenv Env, output io.Writer, filesystem afero.Fs, userConfigDir func() (string, error)) (Config, error) {
	if output == nil {
		output = io.Discard
	}

	cfg := defaultConfig()
	configPath, configPathExplicit, err := configPathFromArgs(args)
	if err != nil {
		return Config{}, err
	}
	if !configPathExplicit {
		configPath, err = discoverConfigPath(filesystem, userConfigDir)
		if err != nil {
			return Config{}, err
		}
	}
	if configPath != "" {
		fileCfg, err := loadConfigFile(filesystem, configPath)
		if err != nil {
			return Config{}, err
		}
		if err := applyFileConfig(&cfg, fileCfg); err != nil {
			return Config{}, fmt.Errorf("config file %q: %w", configPath, err)
		}
	}
	applyEnvironment(&cfg, getenv)

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&configPath, "config", configPath, "YAML configuration file; default discovery uses the first existing file")
	fs.StringVar((*string)(&cfg.Transport), "transport", string(cfg.Transport), flagHelp("transport to serve: stdio or http", EnvTransport))
	fs.StringVar((*string)(&cfg.Environment), "environment", string(cfg.Environment), flagHelp("runtime environment: development or production", EnvEnvironment))
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, flagHelp("HTTP listen address", EnvListen))
	fs.StringVar(&cfg.Path, "path", cfg.Path, flagHelp("HTTP MCP path", EnvPath))
	fs.StringVar(&cfg.InvenTreeURL, "inventree-url", cfg.InvenTreeURL, flagHelp("InvenTree base URL", EnvInvenTreeURL))
	fs.StringVar(&cfg.InvenTreeWebURL, "inventree-web-url", cfg.InvenTreeWebURL, flagHelp("optional exact user-facing InvenTree frontend mount URL", EnvInvenTreeWebURL))
	fs.StringVar((*string)(&cfg.InvenTreeAuthScheme), "inventree-auth-scheme", string(cfg.InvenTreeAuthScheme), flagHelp("InvenTree auth scheme: Token or Bearer", EnvInvenTreeAuthScheme))
	fs.DurationVar(&cfg.InvenTreeTimeout, "inventree-timeout", cfg.InvenTreeTimeout, flagHelp("InvenTree request timeout", EnvInvenTreeTimeout))
	fs.BoolVar(&cfg.InvenTreeTLSSkipVerify, "inventree-tls-skip-verify", cfg.InvenTreeTLSSkipVerify, flagHelp("skip upstream InvenTree TLS verification", EnvInvenTreeTLSSkipVerify))
	var uploadFlagSeen bool
	fs.Func("upload-allow-root", flagHelp("trusted STDIO local upload root; repeatable", EnvUploadAllowRoots), func(value string) error {
		if !uploadFlagSeen {
			cfg.UploadAllowRoots = nil
			uploadFlagSeen = true
		}
		value = strings.TrimSpace(value)
		if value != "" {
			cfg.UploadAllowRoots = append(cfg.UploadAllowRoots, value)
		}
		return nil
	})
	fs.Int64Var(&cfg.UploadMaxBytes, "upload-max-bytes", cfg.UploadMaxBytes, flagHelp("maximum bytes accepted from one upload source", EnvUploadMaxBytes))
	fs.Int64Var(&cfg.MCPMaxRequestBodyBytes, "mcp-max-request-body-bytes", cfg.MCPMaxRequestBodyBytes, flagHelp("maximum bytes accepted in one MCP HTTP request body", EnvMCPMaxRequestBodyBytes))
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, flagHelp("log level", EnvLogLevel))
	fs.StringVar(&cfg.DebugTrafficLog, "debug-traffic-log", cfg.DebugTrafficLog, flagHelp("append full MCP request/response JSON to this debug log file", EnvDebugTrafficLog))
	fs.BoolVar(&cfg.DevIncompleteOAuth, "dev-incomplete-oauth", cfg.DevIncompleteOAuth, flagHelp("allow development-only HTTP parsing without OAuth setup wiring", EnvDevIncompleteOAuth))
	fs.StringVar(&cfg.OAuthIssuerURL, "oauth-issuer-url", cfg.OAuthIssuerURL, flagHelp("public HTTPS OAuth issuer URL", EnvOAuthIssuerURL))
	fs.StringVar(&cfg.OAuthResourceURL, "oauth-resource-url", cfg.OAuthResourceURL, flagHelp("public HTTPS MCP resource URL", EnvOAuthResourceURL))
	var oauthClientIDFlagSeen bool
	fs.Func("oauth-client-id", flagHelp("allowed OAuth client_id metadata URL; repeatable", EnvOAuthClientIDs), func(value string) error {
		if !oauthClientIDFlagSeen {
			cfg.OAuthClientIDs = nil
			oauthClientIDFlagSeen = true
		}
		value = strings.TrimSpace(value)
		if value != "" {
			cfg.OAuthClientIDs = append(cfg.OAuthClientIDs, value)
		}
		return nil
	})
	var trustedProxyCIDRFlagSeen bool
	fs.Func("trusted-proxy-cidr", flagHelp("trusted reverse-proxy CIDR; repeatable", EnvTrustedProxyCIDRs), func(value string) error {
		if !trustedProxyCIDRFlagSeen {
			cfg.TrustedProxyCIDRs = nil
			trustedProxyCIDRFlagSeen = true
		}
		value = strings.TrimSpace(value)
		if value != "" {
			cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, value)
		}
		return nil
	})
	fs.DurationVar(&cfg.OAuthAccessLifetime, "oauth-access-lifetime", cfg.OAuthAccessLifetime, flagHelp("OAuth access token lifetime", EnvOAuthAccessLifetime))
	fs.DurationVar(&cfg.OAuthRefreshLifetime, "oauth-refresh-lifetime", cfg.OAuthRefreshLifetime, flagHelp("OAuth refresh token lifetime", EnvOAuthRefreshLifetime))
	fs.DurationVar(&cfg.OAuthSessionLifetime, "oauth-session-lifetime", cfg.OAuthSessionLifetime, flagHelp("OAuth maximum connector session lifetime", EnvOAuthSessionLifetime))
	fs.BoolVar(&cfg.Telemetry.Enabled, "otel-enabled", cfg.Telemetry.Enabled, flagHelp("enable OpenTelemetry trace export", EnvOTelEnabled))
	fs.StringVar(&cfg.Telemetry.ServiceName, "otel-service-name", cfg.Telemetry.ServiceName, flagHelp("OpenTelemetry service name", EnvOTelServiceName))
	fs.StringVar(&cfg.Telemetry.Exporter, "otel-exporter", cfg.Telemetry.Exporter, flagHelp("OpenTelemetry trace exporter: otlpgrpc or otlphttp", EnvOTelExporter))
	fs.StringVar(&cfg.Telemetry.Endpoint, "otel-endpoint", cfg.Telemetry.Endpoint, flagHelp("OpenTelemetry exporter endpoint", EnvOTelEndpoint))
	fs.BoolVar(&cfg.Telemetry.Insecure, "otel-insecure", cfg.Telemetry.Insecure, flagHelp("disable TLS for the OpenTelemetry exporter", EnvOTelInsecure))
	fs.Float64Var(&cfg.Telemetry.SampleRatio, "otel-sample-ratio", cfg.Telemetry.SampleRatio, flagHelp("OpenTelemetry trace sampling ratio from 0 to 1", EnvOTelSampleRatio))
	fs.DurationVar(&cfg.Telemetry.BatchTimeout, "otel-batch-timeout", cfg.Telemetry.BatchTimeout, flagHelp("OpenTelemetry trace batch timeout", EnvOTelBatchTimeout))
	fs.DurationVar(&cfg.Telemetry.ExportTimeout, "otel-export-timeout", cfg.Telemetry.ExportTimeout, flagHelp("OpenTelemetry trace export timeout", EnvOTelExportTimeout))
	var otelHeaderFlagSeen bool
	fs.Func("otel-header", flagHelp("OpenTelemetry exporter header key=value; repeatable", EnvOTelHeaders), func(value string) error {
		if !otelHeaderFlagSeen {
			cfg.Telemetry.Headers = nil
			otelHeaderFlagSeen = true
		}
		key, headerValue, err := parseHeader(value)
		if err != nil {
			return err
		}
		if cfg.Telemetry.Headers == nil {
			cfg.Telemetry.Headers = make(map[string]string)
		}
		cfg.Telemetry.Headers[key] = headerValue
		return nil
	})

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

	if _, err := c.WebLinkResolver(); err != nil {
		validationErrors = append(validationErrors, err)
	}

	switch c.InvenTreeAuthScheme {
	case AuthSchemeToken, AuthSchemeBearer:
	default:
		validationErrors = append(validationErrors, fmt.Errorf("InvenTree auth scheme must be %q or %q", AuthSchemeToken, AuthSchemeBearer))
	}

	if c.InvenTreeTimeout <= 0 {
		validationErrors = append(validationErrors, errors.New("InvenTree timeout must be greater than zero"))
	}
	if err := c.Telemetry.Validate(); err != nil {
		validationErrors = append(validationErrors, err)
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
		if c.Environment == EnvironmentProduction {
			if parsed, err := url.ParseRequestURI(c.InvenTreeURL); err == nil && parsed.Scheme != "https" {
				validationErrors = append(validationErrors, errors.New("production HTTP mode requires an HTTPS InvenTree URL"))
			}
		}
		if c.MCPMaxRequestBodyBytes <= 0 {
			validationErrors = append(validationErrors, errors.New("MCP max request body bytes must be greater than zero"))
		} else if minimum := MinimumMCPRequestBodyBytes(c.UploadMaxBytes); minimum > 0 && c.MCPMaxRequestBodyBytes < minimum {
			validationErrors = append(validationErrors, fmt.Errorf(
				"MCP max request body bytes must be at least %d for upload max bytes %d",
				minimum,
				c.UploadMaxBytes,
			))
		}
		if c.Path == "" || !strings.HasPrefix(c.Path, "/") {
			validationErrors = append(validationErrors, errors.New("HTTP path must start with /"))
		} else if path.Clean(c.Path) != c.Path || (c.Path != "/" && strings.HasSuffix(c.Path, "/")) {
			validationErrors = append(validationErrors, errors.New("HTTP path must be a canonical path without a trailing slash"))
		}
		if c.Listen == "" {
			validationErrors = append(validationErrors, errors.New("HTTP listen address is required"))
		}
		if c.InvenTreeToken != "" {
			validationErrors = append(validationErrors, errors.New("configured InvenTree tokens are STDIO-only; HTTP runtime credentials come from MCP OAuth access tokens"))
		}
		if c.InvenTreeAuthScheme != AuthSchemeToken {
			validationErrors = append(validationErrors, errors.New("configured InvenTree auth schemes are STDIO-only; HTTP upstream credentials come from MCP OAuth access tokens"))
		}
		if c.Environment == EnvironmentProduction {
			validationErrors = append(validationErrors, c.validateProductionHTTP()...)
		}
		if c.Environment == EnvironmentDevelopment && !c.DevIncompleteOAuth {
			validationErrors = append(validationErrors, errors.New("development HTTP mode requires --dev-incomplete-oauth until OAuth setup wiring is available"))
		}
	}
	for _, rawCIDR := range c.TrustedProxyCIDRs {
		if prefix, err := netip.ParsePrefix(rawCIDR); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("trusted proxy CIDR %q is invalid", rawCIDR))
		} else if prefix.Bits() == 0 {
			validationErrors = append(validationErrors, fmt.Errorf("trusted proxy CIDR %q must not trust every address", rawCIDR))
		}
	}

	return errors.Join(validationErrors...)
}

// EffectiveInvenTreeWebURL returns the explicit user-facing frontend mount or
// the site base used for the approved all-mode INVENTREE_URL fallback.
func (c Config) EffectiveInvenTreeWebURL() (string, string) {
	if strings.TrimSpace(c.InvenTreeWebURL) != "" {
		return c.InvenTreeWebURL, EnvInvenTreeWebURL
	}
	return c.InvenTreeURL, EnvInvenTreeURL
}

// WebLinkResolver validates the effective process-scoped user-facing base.
// An explicit INVENTREE_WEB_URL is the exact frontend mount. The
// INVENTREE_URL fallback is the InvenTree site/API base, so stock deployments
// mount their frontend beneath its version-pinned default /web path.
func (c Config) WebLinkResolver() (*weblinks.Resolver, error) {
	raw, key := c.EffectiveInvenTreeWebURL()
	if key == EnvInvenTreeURL {
		return weblinks.NewAtDefaultMount(raw, key, c.Environment == EnvironmentProduction)
	}
	return weblinks.New(raw, key, c.Environment == EnvironmentProduction)
}

func (c Config) validateProductionHTTP() []error {
	var validationErrors []error
	if c.DevIncompleteOAuth {
		validationErrors = append(validationErrors, errors.New("production HTTP mode rejects --dev-incomplete-oauth"))
	}
	if err := validateHTTPSURL(c.OAuthIssuerURL, "OAuth issuer URL"); err != nil {
		validationErrors = append(validationErrors, err)
	} else if parsed, err := url.Parse(c.OAuthIssuerURL); err == nil && !canonicalOptionalURLPath(parsed) {
		validationErrors = append(validationErrors, errors.New("OAuth issuer URL path must be canonical and must not end with /"))
	}
	if err := validateHTTPSURL(c.OAuthResourceURL, "OAuth resource URL"); err != nil {
		validationErrors = append(validationErrors, err)
	} else if parsed, err := url.Parse(c.OAuthResourceURL); err == nil && !canonicalRequiredURLPath(parsed) {
		validationErrors = append(validationErrors, errors.New("OAuth resource URL path must be canonical and must not end with /"))
	} else if parsed, err := url.Parse(c.OAuthResourceURL); err == nil && parsed.Path != c.Path {
		validationErrors = append(validationErrors, errors.New("OAuth resource URL path must exactly match the HTTP path for path-preserving proxy routing"))
	}
	validationErrors = append(validationErrors, c.validateProductionRoutePaths()...)
	if len(c.TrustedProxyCIDRs) == 0 {
		validationErrors = append(validationErrors, errors.New("at least one trusted proxy CIDR is required for production HTTP"))
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

func (c Config) validateProductionRoutePaths() []error {
	issuer, issuerErr := url.Parse(c.OAuthIssuerURL)
	resourceMetadata, metadataErr := url.Parse(c.OAuthProtectedResourceMetadataURL())
	if issuerErr != nil || metadataErr != nil || issuer.Host == "" || resourceMetadata.Path == "" {
		return nil
	}
	issuerBase := strings.TrimSuffix(issuer.Path, "/")
	routes := []string{
		c.Path,
		resourceMetadata.Path,
		"/.well-known/oauth-authorization-server" + issuerBase,
		issuerBase + "/authorize",
		issuerBase + "/token",
	}
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if _, exists := seen[route]; exists {
			return []error{fmt.Errorf("production HTTP canonical paths collide at %q", route)}
		}
		seen[route] = struct{}{}
	}
	return nil
}

// ValidateProductionRoutePaths rejects canonical route collisions before the
// HTTP mux registers OAuth and MCP handlers.
func (c Config) ValidateProductionRoutePaths() error {
	return errors.Join(c.validateProductionRoutePaths()...)
}

func canonicalOptionalURLPath(parsed *url.URL) bool {
	if parsed.RawPath != "" {
		return false
	}
	return parsed.Path == "" || (parsed.Path != "/" && path.Clean(parsed.Path) == parsed.Path && !strings.HasSuffix(parsed.Path, "/"))
}

func canonicalRequiredURLPath(parsed *url.URL) bool {
	return parsed.RawPath == "" && parsed.Path != "" && parsed.Path != "/" && path.Clean(parsed.Path) == parsed.Path && !strings.HasSuffix(parsed.Path, "/")
}

func (c Config) OAuthProtectedResourceMetadataURL() string {
	if c.OAuthResourceURL == "" {
		return ""
	}
	parsed, err := url.Parse(c.OAuthResourceURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	metadataPath := "/.well-known/oauth-protected-resource"
	if resourcePath := strings.TrimSuffix(parsed.Path, "/"); resourcePath != "" {
		metadataPath += resourcePath
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: metadataPath}).String()
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

// MinimumMCPRequestBodyBytes returns a request-body limit that can carry a
// maximum-sized inline upload after base64 expansion plus bounded JSON and
// tool-argument overhead.
func MinimumMCPRequestBodyBytes(uploadMaxBytes int64) int64 {
	if uploadMaxBytes <= 0 {
		return 0
	}
	if uploadMaxBytes > ((math.MaxInt64-mcpRequestBodyOverheadBytes)/4)*3-2 {
		return math.MaxInt64
	}
	return ((uploadMaxBytes+2)/3)*4 + mcpRequestBodyOverheadBytes
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

func float64Default(getenv Env, key string, fallback float64) float64 {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return math.NaN()
	}
	return value
}

func headerEnv(getenv Env, key string) map[string]string {
	raw := getenv(key)
	if raw == "" {
		return nil
	}
	headers := make(map[string]string)
	for _, item := range strings.Split(raw, ",") {
		name, value, err := parseHeader(item)
		if err != nil {
			headers[""] = "invalid"
			continue
		}
		headers[name] = value
	}
	return headers
}

func parseHeader(raw string) (string, string, error) {
	name, value, ok := strings.Cut(raw, "=")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return "", "", errors.New("OpenTelemetry header must use key=value")
	}
	return name, value, nil
}

func flagHelp(description string, envVar string) string {
	return fmt.Sprintf("%s (env: %s)", description, envVar)
}
