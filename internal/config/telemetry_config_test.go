package config

import (
	"testing"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func TestParseServeConfiguresOptInOpenTelemetryFromEnvironmentAndFlags(t *testing.T) {
	t.Parallel()
	cfg, err := ParseServeWithEnv([]string{
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
		"--otel-exporter", "otlphttp",
		"--otel-endpoint", "https://collector.flag.example.test/v1/traces",
		"--otel-header", "authorization=Bearer flag-secret",
	}, mapEnv(map[string]string{
		EnvInvenTreeToken:   "token",
		EnvOTelEnabled:      "true",
		EnvOTelServiceName:  "warehouse-mcp",
		EnvOTelExporter:     "otlpgrpc",
		EnvOTelEndpoint:     "collector.env.example.test:4317",
		EnvOTelHeaders:      "x-api-key=secret,x-tenant=warehouse",
		EnvOTelSampleRatio:  "0.25",
		EnvOTelBatchTimeout: "2s",
	}), nil)
	require.NoError(t, err)
	require.True(t, cfg.Telemetry.Enabled)
	require.Equal(t, "warehouse-mcp", cfg.Telemetry.ServiceName)
	require.Equal(t, "otlphttp", cfg.Telemetry.Exporter)
	require.Equal(t, "https://collector.flag.example.test/v1/traces", cfg.Telemetry.Endpoint)
	require.Equal(t, map[string]string{"authorization": "Bearer flag-secret"}, cfg.Telemetry.Headers)
	require.Equal(t, 0.25, cfg.Telemetry.SampleRatio)
	require.Equal(t, 2*time.Second, cfg.Telemetry.BatchTimeout)
}

func TestParseServeRejectsInvalidEnabledOpenTelemetry(t *testing.T) {
	t.Parallel()
	_, err := ParseServeWithEnv([]string{"--transport", "stdio", "--inventree-url", "https://inventory.example.test"}, mapEnv(map[string]string{
		EnvInvenTreeToken: "token",
		EnvOTelEnabled:    "true",
	}), nil)
	require.ErrorContains(t, err, "OpenTelemetry endpoint is required")
}

func TestParseServeRejectsInvalidOpenTelemetrySampleRatio(t *testing.T) {
	t.Parallel()
	_, err := ParseServeWithEnv([]string{"--transport", "stdio", "--inventree-url", "https://inventory.example.test"}, mapEnv(map[string]string{
		EnvInvenTreeToken:  "token",
		EnvOTelEnabled:     "true",
		EnvOTelEndpoint:    "collector.example.test:4317",
		EnvOTelSampleRatio: "not-a-number",
	}), nil)
	require.ErrorContains(t, err, "OpenTelemetry sample ratio")
}

func TestParseServeRejectsInvalidOpenTelemetrySettings(t *testing.T) {
	t.Parallel()
	baseArgs := []string{"--transport", "stdio", "--inventree-url", "https://inventory.example.test"}
	baseEnv := map[string]string{
		EnvInvenTreeToken: "token",
		EnvOTelEnabled:    "true",
		EnvOTelEndpoint:   "collector.example.test:4317",
	}

	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "exporter", env: map[string]string{EnvOTelExporter: "zipkin"}, want: "OpenTelemetry exporter must be"},
		{name: "batch timeout", env: map[string]string{EnvOTelBatchTimeout: "0s"}, want: "OpenTelemetry batch timeout"},
		{name: "export timeout", env: map[string]string{EnvOTelExportTimeout: "not-a-duration"}, want: "OpenTelemetry export timeout"},
		{name: "header", env: map[string]string{EnvOTelHeaders: "missing-equals"}, want: "OpenTelemetry header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := make(map[string]string, len(baseEnv)+len(tc.env))
			for key, value := range baseEnv {
				env[key] = value
			}
			for key, value := range tc.env {
				env[key] = value
			}
			_, err := ParseServeWithEnv(baseArgs, mapEnv(env), nil)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestParseServeConfiguresPrometheusMetricsForHTTP(t *testing.T) {
	t.Parallel()
	cfg, err := ParseServeWithEnv([]string{
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "http://inventory.example.test",
		"--otel-metrics-path", "/internal/metrics",
	}, mapEnv(map[string]string{
		EnvOTelMetricsEnabled: "true",
	}), nil)
	require.NoError(t, err)
	require.True(t, cfg.Telemetry.MetricsEnabled)
	require.Equal(t, "/internal/metrics", cfg.Telemetry.MetricsPath)
}

func TestValidateProductionMetricsRequiresPrivateListener(t *testing.T) {
	t.Parallel()
	base := Config{
		Transport:           TransportHTTP,
		Environment:         EnvironmentProduction,
		Listen:              "127.0.0.1:28686",
		Path:                "/mcp",
		InvenTreeURL:        "https://inventory.example.test",
		InvenTreeAuthScheme: AuthSchemeToken,
		InvenTreeTimeout:    time.Second,
		Telemetry:           telemetryConfigForTest(),
	}
	for _, tc := range []struct {
		name   string
		listen string
		valid  bool
	}{
		{name: "loopback", listen: "127.0.0.1:28686", valid: true},
		{name: "private", listen: "10.0.0.5:28686", valid: true},
		{name: "private ipv6", listen: "[fd00::5]:28686", valid: true},
		{name: "wildcard", listen: ":28686"},
		{name: "public", listen: "192.0.2.5:28686"},
		{name: "hostname", listen: "metrics.internal:28686"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Listen = tc.listen
			err := cfg.Validate()
			if tc.valid {
				require.NotContains(t, errString(err), "private, non-wildcard")
				return
			}
			require.Contains(t, errString(err), "private, non-wildcard")
		})
	}
}

func TestValidateProductionMetricsRejectsRouteCollisions(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Path:             "/mcp",
		OAuthIssuerURL:   "https://mcp.example.test",
		OAuthResourceURL: "https://mcp.example.test/mcp",
		Telemetry:        telemetryConfigForTest(),
	}
	for _, path := range []string{"/mcp", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/mcp/oauth/authorize", "/mcp/oauth/token"} {
		t.Run(path, func(t *testing.T) {
			cfg.Telemetry.MetricsPath = path
			require.ErrorContains(t, cfg.ValidateProductionRoutePaths(), "canonical paths collide")
		})
	}
}

func telemetryConfigForTest() telemetry.Config {
	return telemetry.Config{MetricsEnabled: true, MetricsPath: "/metrics", ServiceName: "inventree-mcp"}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
