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
)

const invalidDuration = time.Duration(-1)

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
	LogLevel               string
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
		Transport:           Transport(envDefault(getenv, "INVENTREE_MCP_TRANSPORT", string(TransportStdio))),
		Environment:         Environment(envDefault(getenv, "INVENTREE_MCP_ENVIRONMENT", string(EnvironmentProduction))),
		Listen:              envDefault(getenv, "INVENTREE_MCP_LISTEN", ":8080"),
		Path:                envDefault(getenv, "INVENTREE_MCP_PATH", "/mcp"),
		InvenTreeURL:        getenv("INVENTREE_URL"),
		InvenTreeToken:      getenv("INVENTREE_TOKEN"),
		InvenTreeAuthScheme: AuthScheme(envDefault(getenv, "INVENTREE_AUTH_SCHEME", string(AuthSchemeToken))),
		InvenTreeTimeout:    durationDefault(getenv, "INVENTREE_TIMEOUT", 30*time.Second),
		LogLevel:            envDefault(getenv, "INVENTREE_MCP_LOG_LEVEL", "info"),
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar((*string)(&cfg.Transport), "transport", string(cfg.Transport), "transport to serve: stdio or http")
	fs.StringVar((*string)(&cfg.Environment), "environment", string(cfg.Environment), "runtime environment: development or production")
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	fs.StringVar(&cfg.Path, "path", cfg.Path, "HTTP MCP path")
	fs.StringVar(&cfg.InvenTreeURL, "inventree-url", cfg.InvenTreeURL, "InvenTree base URL")
	fs.StringVar(&cfg.InvenTreeToken, "inventree-token", cfg.InvenTreeToken, "InvenTree API token for STDIO mode")
	fs.StringVar((*string)(&cfg.InvenTreeAuthScheme), "inventree-auth-scheme", string(cfg.InvenTreeAuthScheme), "InvenTree auth scheme: Token or Bearer")
	fs.DurationVar(&cfg.InvenTreeTimeout, "inventree-timeout", cfg.InvenTreeTimeout, "InvenTree request timeout")
	fs.BoolVar(&cfg.InvenTreeTLSSkipVerify, "inventree-tls-skip-verify", boolEnv(getenv, "INVENTREE_TLS_SKIP_VERIFY"), "skip upstream InvenTree TLS verification")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	fs.BoolVar(&cfg.DevIncompleteOAuth, "dev-incomplete-oauth", boolEnv(getenv, "INVENTREE_MCP_DEV_INCOMPLETE_OAUTH"), "allow development-only HTTP parsing before OAuth is implemented")

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
	}

	switch c.InvenTreeAuthScheme {
	case AuthSchemeToken, AuthSchemeBearer:
	default:
		validationErrors = append(validationErrors, fmt.Errorf("InvenTree auth scheme must be %q or %q", AuthSchemeToken, AuthSchemeBearer))
	}

	if c.InvenTreeTimeout <= 0 {
		validationErrors = append(validationErrors, errors.New("InvenTree timeout must be greater than zero"))
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
			validationErrors = append(validationErrors, errors.New("configured InvenTree tokens are STDIO-only until HTTP OAuth is implemented"))
		}
		if c.InvenTreeAuthScheme != AuthSchemeToken {
			validationErrors = append(validationErrors, errors.New("configured InvenTree auth schemes are STDIO-only until HTTP OAuth is implemented"))
		}
		if c.InvenTreeTLSSkipVerify && c.Environment == EnvironmentProduction {
			validationErrors = append(validationErrors, errors.New("production HTTP mode rejects InvenTree TLS skip verify"))
		}
		if c.Environment == EnvironmentProduction {
			validationErrors = append(validationErrors, errors.New("production HTTP mode is disabled until OAuth is implemented"))
		}
		if c.Environment == EnvironmentDevelopment && !c.DevIncompleteOAuth {
			validationErrors = append(validationErrors, errors.New("development HTTP mode requires --dev-incomplete-oauth until OAuth is implemented"))
		}
	}

	return errors.Join(validationErrors...)
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
