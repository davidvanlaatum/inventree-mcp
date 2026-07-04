package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseServeUsesEnvAndFlagPrecedence(t *testing.T) {
	t.Parallel()

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
	if err != nil {
		t.Fatalf("ParseServeWithEnv returned error: %v", err)
	}

	if cfg.InvenTreeURL != "https://flag.example.test" {
		t.Fatalf("InvenTreeURL = %q, want flag value", cfg.InvenTreeURL)
	}
	if cfg.InvenTreeToken != "env-token" {
		t.Fatalf("InvenTreeToken = %q, want env value", cfg.InvenTreeToken)
	}
	if cfg.InvenTreeAuthScheme != AuthSchemeBearer {
		t.Fatalf("InvenTreeAuthScheme = %q, want %q", cfg.InvenTreeAuthScheme, AuthSchemeBearer)
	}
	if cfg.InvenTreeTimeout != 5*time.Second {
		t.Fatalf("InvenTreeTimeout = %s, want 5s", cfg.InvenTreeTimeout)
	}
}

func TestParseServeRejectsMissingStdioRequiredValues(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{"--transport", "stdio"}, mapEnv(nil), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}

	assertErrorContains(t, err, "InvenTree URL is required")
	assertErrorContains(t, err, "InvenTree token is required for STDIO transport")
}

func TestParseServeRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "websocket",
		"--environment", "stage",
		"--inventree-url", "not-a-url",
		"--inventree-auth-scheme", "Basic",
		"--inventree-timeout", "0s",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}

	assertErrorContains(t, err, "transport must be")
	assertErrorContains(t, err, "environment must be")
	assertErrorContains(t, err, "InvenTree URL must be an absolute URL")
	assertErrorContains(t, err, "InvenTree auth scheme must be")
	assertErrorContains(t, err, "InvenTree timeout must be greater than zero")
}

func TestParseServeRejectsInvalidEnvDuration(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken:   "token",
		EnvInvenTreeTimeout: "not-a-duration",
	}), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}
	assertErrorContains(t, err, "InvenTree timeout must be greater than zero")
}

func TestParseServeRejectsProductionHTTPBeforeOAuth(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-tls-skip-verify",
	}, mapEnv(nil), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}

	assertErrorContains(t, err, "production HTTP mode rejects InvenTree TLS skip verify")
	assertErrorContains(t, err, "production HTTP mode is disabled until OAuth is implemented")
}

func TestParseServeAllowsDevelopmentHTTPOnlyWithExplicitIncompleteOAuthFlag(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(nil), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}
	assertErrorContains(t, err, "development HTTP mode requires --dev-incomplete-oauth")

	cfg, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(nil), nil)
	if err != nil {
		t.Fatalf("ParseServeWithEnv returned error: %v", err)
	}
	if cfg.Transport != TransportHTTP {
		t.Fatalf("Transport = %q, want %q", cfg.Transport, TransportHTTP)
	}
}

func TestParseServeRejectsHTTPConfiguredToken(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken: "raw-token",
	}), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}
	assertErrorContains(t, err, "configured InvenTree tokens are STDIO-only")
}

func TestParseServeRejectsTokenFlag(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-token", "raw-token",
	}, mapEnv(nil), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}
	assertErrorContains(t, err, "flag provided but not defined: -inventree-token")
}

func TestParseServeRejectsHTTPConfiguredAuthScheme(t *testing.T) {
	t.Parallel()

	_, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
		"--inventree-auth-scheme", "Bearer",
	}, mapEnv(nil), nil)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}
	assertErrorContains(t, err, "configured InvenTree auth schemes are STDIO-only")
}

func TestParseServeHelpMentionsEnvVars(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	_, err := ParseServeWithEnv([]string{"--help"}, mapEnv(nil), &output)
	if err == nil {
		t.Fatal("ParseServeWithEnv returned nil error")
	}

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
	} {
		if !strings.Contains(help, envVar) {
			t.Fatalf("help output does not mention %s:\n%s", envVar, help)
		}
	}
	if strings.Contains(help, EnvInvenTreeToken) {
		t.Fatalf("help output mentions sensitive %s:\n%s", EnvInvenTreeToken, help)
	}
}

func mapEnv(values map[string]string) Env {
	return func(key string) string {
		return values[key]
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()

	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}
