package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"strings"
	"time"
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
	EnvMCPMaxRequestBodyBytes = "INVENTREE_MCP_MAX_REQUEST_BODY_BYTES"
	EnvLogLevel               = "INVENTREE_MCP_LOG_LEVEL"
	EnvDebugTrafficLog        = "INVENTREE_MCP_DEBUG_TRAFFIC_LOG"
	EnvDevIncompleteOAuth     = "INVENTREE_MCP_DEV_INCOMPLETE_OAUTH"

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
		MCPMaxRequestBodyBytes: int64Default(
			getenv,
			EnvMCPMaxRequestBodyBytes,
			DefaultMCPMaxRequestBodyBytes,
		),
		LogLevel:        envDefault(getenv, EnvLogLevel, "info"),
		DebugTrafficLog: strings.TrimSpace(getenv(EnvDebugTrafficLog)),
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
	fs.Int64Var(&cfg.MCPMaxRequestBodyBytes, "mcp-max-request-body-bytes", cfg.MCPMaxRequestBodyBytes, flagHelp("maximum bytes accepted in one MCP HTTP request body", EnvMCPMaxRequestBodyBytes))
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, flagHelp("log level", EnvLogLevel))
	fs.StringVar(&cfg.DebugTrafficLog, "debug-traffic-log", cfg.DebugTrafficLog, flagHelp("append full MCP request/response JSON to this debug log file", EnvDebugTrafficLog))
	fs.BoolVar(&cfg.DevIncompleteOAuth, "dev-incomplete-oauth", boolEnv(getenv, EnvDevIncompleteOAuth), flagHelp("allow development-only HTTP parsing before OAuth startup wiring is available", EnvDevIncompleteOAuth))

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
			validationErrors = append(validationErrors, errors.New("production HTTP mode is disabled until OAuth startup and setup wiring is available"))
		}
		if c.Environment == EnvironmentDevelopment && !c.DevIncompleteOAuth {
			validationErrors = append(validationErrors, errors.New("development HTTP mode requires --dev-incomplete-oauth until OAuth startup and setup wiring is available"))
		}
	}

	return errors.Join(validationErrors...)
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
