package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseServeLoadsConfigFileWithPrecedence(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config.yml", []byte(`
transport: stdio
environment: development
inventree_url: https://yaml.example.test
inventree_token: yaml-token
inventree_timeout: 11s
inventree_tls_skip_verify: true
upload_allow_roots:
  - /yaml/upload
upload_max_bytes: 2048
oauth_client_ids:
  - https://yaml.example.test/client
trusted_proxy_cidrs:
  - 192.0.2.0/24
bulk_max_items: 15
bulk_concurrency: 6
otel_enabled: true
otel_service_name: yaml-telemetry
otel_exporter: otlphttp
otel_endpoint: https://collector.yaml.example.test/v1/traces
otel_insecure: true
otel_headers:
  - x-api-key=yaml-secret
  - x-tenant=warehouse
otel_sample_ratio: 0.5
otel_batch_timeout: 3s
otel_export_timeout: 7s
`), 0o600))

	cfg, err := parseServeWithDeps([]string{
		"--config", "/config.yml",
		"--inventree-url", "https://flag.example.test",
		"--upload-allow-root", "/flag/one",
		"--upload-allow-root", "/flag/two",
		"--oauth-client-id", "https://flag.example.test/client/a",
		"--oauth-client-id", "https://flag.example.test/client/b",
	}, mapEnv(map[string]string{
		EnvInvenTreeURL:           "https://env.example.test",
		EnvInvenTreeToken:         "env-token",
		EnvInvenTreeTimeout:       "13s",
		EnvUploadAllowRoots:       "/env/one" + string(os.PathListSeparator) + "/env/two",
		EnvUploadMaxBytes:         "4096",
		EnvOAuthClientIDs:         "https://env.example.test/client",
		EnvTrustedProxyCIDRs:      "198.51.100.0/24",
		EnvInvenTreeTLSSkipVerify: "false",
	}), nil, fs, func() (string, error) { return "/user-config", nil })
	require.NoError(t, err)
	assert.Equal(t, "https://flag.example.test", cfg.InvenTreeURL)
	assert.Equal(t, "env-token", cfg.InvenTreeToken)
	assert.Equal(t, 13*time.Second, cfg.InvenTreeTimeout)
	assert.False(t, cfg.InvenTreeTLSSkipVerify)
	assert.Equal(t, []string{"/flag/one", "/flag/two"}, cfg.UploadAllowRoots)
	assert.Equal(t, int64(4096), cfg.UploadMaxBytes)
	assert.Equal(t, []string{"https://flag.example.test/client/a", "https://flag.example.test/client/b"}, cfg.OAuthClientIDs)
	assert.Equal(t, []string{"198.51.100.0/24"}, cfg.TrustedProxyCIDRs)
	assert.Equal(t, 15, cfg.BulkMaxItems)
	assert.Equal(t, 6, cfg.BulkConcurrency)
	assert.True(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "yaml-telemetry", cfg.Telemetry.ServiceName)
	assert.Equal(t, "otlphttp", cfg.Telemetry.Exporter)
	assert.Equal(t, "https://collector.yaml.example.test/v1/traces", cfg.Telemetry.Endpoint)
	assert.True(t, cfg.Telemetry.Insecure)
	assert.Equal(t, map[string]string{"x-api-key": "yaml-secret", "x-tenant": "warehouse"}, cfg.Telemetry.Headers)
	assert.Equal(t, 0.5, cfg.Telemetry.SampleRatio)
	assert.Equal(t, 3*time.Second, cfg.Telemetry.BatchTimeout)
	assert.Equal(t, 7*time.Second, cfg.Telemetry.ExportTimeout)
}

func TestParseServeLoadsBootstrapSettingsFromConfigFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config.yml", []byte(`
inventree_url: https://yaml.example.test
inventree_token: yaml-token
bootstrap_enabled: true
bootstrap_envelope_lifetime: 48h
`), 0o600))

	cfg, err := parseServeWithDeps([]string{"--config", "/config.yml"}, mapEnv(nil), nil, fs, func() (string, error) { return "/user-config", nil })
	require.NoError(t, err)
	assert.True(t, cfg.BootstrapEnabled)
	assert.Equal(t, 48*time.Hour, cfg.BootstrapEnvelopeLifetime)
}

func TestParseServeBootstrapEnvOverridesConfigFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config.yml", []byte(`
inventree_url: https://yaml.example.test
inventree_token: yaml-token
bootstrap_enabled: true
bootstrap_envelope_lifetime: 48h
`), 0o600))

	cfg, err := parseServeWithDeps([]string{"--config", "/config.yml"}, mapEnv(map[string]string{
		EnvBootstrapEnabled:          "false",
		EnvBootstrapEnvelopeLifetime: "72h",
	}), nil, fs, func() (string, error) { return "/user-config", nil })
	require.NoError(t, err)
	assert.False(t, cfg.BootstrapEnabled)
	assert.Equal(t, 72*time.Hour, cfg.BootstrapEnvelopeLifetime)
}

func TestDiscoverConfigPathUsesFirstExistingFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	for _, filename := range []string{"./inventree-mcp.yml", "/user-config/inventree-mcp/config.yml", "/etc/inventree-mcp/config.yml"} {
		require.NoError(t, fs.MkdirAll(filepath.Dir(filename), 0o700))
		require.NoError(t, afero.WriteFile(fs, filename, []byte("inventree_url: https://example.test\n"), 0o600))
	}

	path, err := discoverConfigPath(fs, func() (string, error) { return "/user-config", nil })
	require.NoError(t, err)
	assert.Equal(t, "./inventree-mcp.yml", path)
}

func TestDiscoverConfigPathChecksUserConfigAfterWorkingDirectory(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := "/user-config/inventree-mcp/config.yaml"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, afero.WriteFile(fs, path, []byte("inventree_url: https://example.test\n"), 0o600))

	got, err := discoverConfigPath(fs, func() (string, error) { return "/user-config", nil })
	require.NoError(t, err)
	assert.Equal(t, path, got)
}

func TestParseServeExplicitConfigPathDoesNotUseDefaultDiscovery(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/explicit.yml", []byte("inventree_url: https://explicit.example.test\n"), 0o600))

	cfg, err := parseServeWithDeps([]string{"--config=/explicit.yml"}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil, fs, func() (string, error) {
		return "", errors.New("default discovery must not run")
	})
	require.NoError(t, err)
	assert.Equal(t, "https://explicit.example.test", cfg.InvenTreeURL)
}

func TestParseServeRejectsInvalidTelemetryConfigFileValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "header", body: "otel_headers:\n  - missing-equals\n", want: "otel_headers[0] is invalid"},
		{name: "batch timeout", body: "otel_batch_timeout: not-a-duration\n", want: "otel_batch_timeout must be a valid duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/config.yml", []byte(tc.body), 0o600))
			_, err := parseServeWithDeps([]string{"--config", "/config.yml"}, mapEnv(map[string]string{
				EnvInvenTreeToken: "token",
			}), nil, fs, func() (string, error) { return "/unused", nil })
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestParseServeRejectsUnknownOrMalformedConfigWithoutSecretLeak(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown field", body: "inventree_url: https://example.test\nunknown: super-secret-value\n", want: "field unknown not found"},
		{name: "malformed yaml", body: "inventree_url: [\n inventree_token: super-secret-value\n", want: "decode YAML"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/config.yml", []byte(tc.body), 0o600))
			_, err := parseServeWithDeps([]string{"--config", "/config.yml"}, mapEnv(map[string]string{
				EnvInvenTreeToken: "token",
			}), nil, fs, func() (string, error) { return "/unused", nil })
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "super-secret-value")
		})
	}
}

func TestParseServeRejectsUnsafeConfigPermissionsAndChecksSymlinkTarget(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix mode checks are not used on this platform")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yml")
	link := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(target, []byte("inventree_url: https://example.test\n"), 0o644))
	require.NoError(t, os.Symlink(target, link))

	_, err := parseServeWithDeps([]string{"--config", link}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil, afero.NewOsFs(), func() (string, error) { return dir, nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be group- or world-readable")

	require.NoError(t, os.Chmod(target, 0o600))
	_, err = parseServeWithDeps([]string{"--config", link}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil, afero.NewOsFs(), func() (string, error) { return dir, nil })
	require.NoError(t, err)
}

func TestDiscoverConfigPathReportsUserConfigDirError(t *testing.T) {
	t.Parallel()
	_, err := discoverConfigPath(afero.NewMemMapFs(), func() (string, error) {
		return "", errors.New("XDG_CONFIG_HOME must be absolute")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "determine user config directory")
}

func TestParseServeConfigFlagErrorsAreActionable(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	_, err := parseServeWithDeps([]string{"--config"}, mapEnv(nil), nil, fs, func() (string, error) { return "/user", nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--config requires a file path")

	_, err = parseServeWithDeps([]string{"--config", "/missing.yml"}, mapEnv(nil), nil, fs, func() (string, error) { return "/user", nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open config file")
}

func TestParseServeConfigFileSupportsExplicitEmptyLists(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config.yml", []byte("inventree_url: https://example.test\nupload_allow_roots: []\n"), 0o600))
	cfg, err := parseServeWithDeps([]string{"--config", "/config.yml"}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
	}), nil, fs, func() (string, error) { return "/user", nil })
	require.NoError(t, err)
	assert.Empty(t, cfg.UploadAllowRoots)
}

func TestApplyFileConfigCoversTypedFields(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()
	fileCfg := fileConfig{
		Transport:              stringPtr("http"),
		Environment:            stringPtr("development"),
		Listen:                 stringPtr("127.0.0.1:9999"),
		Path:                   stringPtr("/custom"),
		InvenTreeURL:           stringPtr("https://inventory.example.test"),
		InvenTreeWebURL:        stringPtr("https://inventory.example.test/web"),
		InvenTreeToken:         stringPtr("token"),
		InvenTreeAuthScheme:    stringPtr("Bearer"),
		InvenTreeTimeout:       stringPtr("7s"),
		InvenTreeTLSSkipVerify: boolPtr(true),
		UploadAllowRoots:       []string{"/one", "/two"},
		UploadMaxBytes:         int64Ptr(100),
		MCPMaxRequestBodyBytes: int64Ptr(200),
		LogLevel:               stringPtr("debug"),
		DebugTrafficLog:        stringPtr("/tmp/traffic.jsonl"),
		DevIncompleteOAuth:     boolPtr(true),
		OAuthIssuerURL:         stringPtr("https://issuer.example.test"),
		OAuthResourceURL:       stringPtr("https://resource.example.test/mcp"),
		OAuthKeys:              []string{"current:active:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"},
		OAuthClientIDs:         []string{"https://client.example.test"},
		TrustedProxyCIDRs:      []string{"192.0.2.0/24"},
		OAuthAccessLifetime:    stringPtr("1m"),
		OAuthRefreshLifetime:   stringPtr("2m"),
		OAuthSessionLifetime:   stringPtr("3m"),
		BulkMaxItems:           intPtr(10),
		BulkConcurrency:        intPtr(2),
	}
	require.NoError(t, applyFileConfig(&cfg, fileCfg))
	assert.Equal(t, TransportHTTP, cfg.Transport)
	assert.Equal(t, EnvironmentDevelopment, cfg.Environment)
	assert.Equal(t, "127.0.0.1:9999", cfg.Listen)
	assert.Equal(t, "/custom", cfg.Path)
	assert.Equal(t, "https://inventory.example.test", cfg.InvenTreeURL)
	assert.Equal(t, "https://inventory.example.test/web", cfg.InvenTreeWebURL)
	assert.Equal(t, "token", cfg.InvenTreeToken)
	assert.Equal(t, AuthSchemeBearer, cfg.InvenTreeAuthScheme)
	assert.Equal(t, 7*time.Second, cfg.InvenTreeTimeout)
	assert.True(t, cfg.InvenTreeTLSSkipVerify)
	assert.Equal(t, []string{"/one", "/two"}, cfg.UploadAllowRoots)
	assert.Equal(t, int64(100), cfg.UploadMaxBytes)
	assert.Equal(t, int64(200), cfg.MCPMaxRequestBodyBytes)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "/tmp/traffic.jsonl", cfg.DebugTrafficLog)
	assert.True(t, cfg.DevIncompleteOAuth)
	assert.Equal(t, "https://issuer.example.test", cfg.OAuthIssuerURL)
	assert.Equal(t, "https://resource.example.test/mcp", cfg.OAuthResourceURL)
	assert.Len(t, cfg.OAuthKeyring.Keys, 1)
	assert.Equal(t, []string{"https://client.example.test"}, cfg.OAuthClientIDs)
	assert.Equal(t, []string{"192.0.2.0/24"}, cfg.TrustedProxyCIDRs)
	assert.Equal(t, time.Minute, cfg.OAuthAccessLifetime)
	assert.Equal(t, 2*time.Minute, cfg.OAuthRefreshLifetime)
	assert.Equal(t, 3*time.Minute, cfg.OAuthSessionLifetime)
	assert.Equal(t, 10, cfg.BulkMaxItems)
	assert.Equal(t, 2, cfg.BulkConcurrency)
}

func TestApplyEnvironmentCoversTypedFields(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()
	applyEnvironment(&cfg, mapEnv(map[string]string{
		EnvTransport:              "http",
		EnvEnvironment:            "development",
		EnvListen:                 "127.0.0.1:9999",
		EnvPath:                   "/custom",
		EnvInvenTreeURL:           "https://inventory.example.test",
		EnvInvenTreeWebURL:        "https://inventory.example.test/web",
		EnvInvenTreeToken:         "token",
		EnvInvenTreeAuthScheme:    "Bearer",
		EnvInvenTreeTimeout:       "7s",
		EnvInvenTreeTLSSkipVerify: "true",
		EnvUploadAllowRoots:       "/one" + string(os.PathListSeparator) + "/two",
		EnvUploadMaxBytes:         "100",
		EnvMCPMaxRequestBodyBytes: "200",
		EnvLogLevel:               "debug",
		EnvDebugTrafficLog:        "/tmp/traffic.jsonl",
		EnvDevIncompleteOAuth:     "true",
		EnvOAuthIssuerURL:         "https://issuer.example.test",
		EnvOAuthResourceURL:       "https://resource.example.test/mcp",
		EnvOAuthKeys:              "current:active:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",
		EnvOAuthClientIDs:         "https://client.example.test",
		EnvTrustedProxyCIDRs:      "192.0.2.0/24",
		EnvOAuthAccessLifetime:    "1m",
		EnvOAuthRefreshLifetime:   "2m",
		EnvOAuthSessionLifetime:   "3m",
		EnvBulkMaxItems:           "10",
		EnvBulkConcurrency:        "2",
	}))
	assert.Equal(t, TransportHTTP, cfg.Transport)
	assert.Equal(t, EnvironmentDevelopment, cfg.Environment)
	assert.Equal(t, "127.0.0.1:9999", cfg.Listen)
	assert.Equal(t, "/custom", cfg.Path)
	assert.Equal(t, "https://inventory.example.test", cfg.InvenTreeURL)
	assert.Equal(t, "https://inventory.example.test/web", cfg.InvenTreeWebURL)
	assert.Equal(t, "token", cfg.InvenTreeToken)
	assert.Equal(t, AuthSchemeBearer, cfg.InvenTreeAuthScheme)
	assert.Equal(t, 7*time.Second, cfg.InvenTreeTimeout)
	assert.True(t, cfg.InvenTreeTLSSkipVerify)
	assert.Equal(t, []string{"/one", "/two"}, cfg.UploadAllowRoots)
	assert.Equal(t, int64(100), cfg.UploadMaxBytes)
	assert.Equal(t, int64(200), cfg.MCPMaxRequestBodyBytes)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "/tmp/traffic.jsonl", cfg.DebugTrafficLog)
	assert.True(t, cfg.DevIncompleteOAuth)
	assert.Len(t, cfg.OAuthKeyring.Keys, 1)
	assert.Equal(t, []string{"https://client.example.test"}, cfg.OAuthClientIDs)
	assert.Equal(t, []string{"192.0.2.0/24"}, cfg.TrustedProxyCIDRs)
	assert.Equal(t, time.Minute, cfg.OAuthAccessLifetime)
	assert.Equal(t, 2*time.Minute, cfg.OAuthRefreshLifetime)
	assert.Equal(t, 3*time.Minute, cfg.OAuthSessionLifetime)
	assert.Equal(t, 10, cfg.BulkMaxItems)
	assert.Equal(t, 2, cfg.BulkConcurrency)
}

func TestConfigPathFromArgsRejectsDuplicateAndEmptyValues(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--config", "/one", "--config", "/two"}, {"--config="}} {
		_, _, err := configPathFromArgs(args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--config")
	}
}

func TestApplyFileConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, fileCfg := range []fileConfig{
		{InvenTreeTimeout: stringPtr("not-a-duration")},
		{OAuthAccessLifetime: stringPtr("not-a-duration")},
		{OAuthKeys: []string{"invalid-key"}},
	} {
		err := applyFileConfig(&Config{}, fileCfg)
		require.Error(t, err)
	}
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func int64Ptr(value int64) *int64 { return &value }

func intPtr(value int) *int { return &value }
