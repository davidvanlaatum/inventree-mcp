package config

import (
	"testing"
	"time"

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
