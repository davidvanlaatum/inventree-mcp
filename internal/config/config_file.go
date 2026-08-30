package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/oauth"
	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Transport                 *string  `yaml:"transport"`
	Environment               *string  `yaml:"environment"`
	Listen                    *string  `yaml:"listen"`
	Path                      *string  `yaml:"path"`
	InvenTreeURL              *string  `yaml:"inventree_url"`
	InvenTreeWebURL           *string  `yaml:"inventree_web_url"`
	InvenTreeToken            *string  `yaml:"inventree_token"`
	InvenTreeAuthScheme       *string  `yaml:"inventree_auth_scheme"`
	InvenTreeTimeout          *string  `yaml:"inventree_timeout"`
	InvenTreeTLSSkipVerify    *bool    `yaml:"inventree_tls_skip_verify"`
	UploadAllowRoots          []string `yaml:"upload_allow_roots"`
	UploadMaxBytes            *int64   `yaml:"upload_max_bytes"`
	MCPMaxRequestBodyBytes    *int64   `yaml:"mcp_max_request_body_bytes"`
	LogLevel                  *string  `yaml:"log_level"`
	DebugTrafficLog           *string  `yaml:"debug_traffic_log"`
	DevIncompleteOAuth        *bool    `yaml:"dev_incomplete_oauth"`
	OAuthIssuerURL            *string  `yaml:"oauth_issuer_url"`
	OAuthResourceURL          *string  `yaml:"oauth_resource_url"`
	OAuthKeys                 []string `yaml:"oauth_keys"`
	OAuthClientIDs            []string `yaml:"oauth_client_ids"`
	TrustedProxyCIDRs         []string `yaml:"trusted_proxy_cidrs"`
	OAuthAccessLifetime       *string  `yaml:"oauth_access_lifetime"`
	OAuthRefreshLifetime      *string  `yaml:"oauth_refresh_lifetime"`
	OAuthSessionLifetime      *string  `yaml:"oauth_session_lifetime"`
	BootstrapEnabled          *bool    `yaml:"bootstrap_enabled"`
	BootstrapEnvelopeLifetime *string  `yaml:"bootstrap_envelope_lifetime"`
	TelemetryEnabled          *bool    `yaml:"otel_enabled"`
	TelemetryServiceName      *string  `yaml:"otel_service_name"`
	TelemetryExporter         *string  `yaml:"otel_exporter"`
	TelemetryEndpoint         *string  `yaml:"otel_endpoint"`
	TelemetryInsecure         *bool    `yaml:"otel_insecure"`
	TelemetryHeaders          []string `yaml:"otel_headers"`
	TelemetrySampleRatio      *float64 `yaml:"otel_sample_ratio"`
	TelemetryBatchTimeout     *string  `yaml:"otel_batch_timeout"`
	TelemetryExportTimeout    *string  `yaml:"otel_export_timeout"`
	TelemetryMetricsEnabled   *bool    `yaml:"otel_metrics_enabled"`
	TelemetryMetricsPath      *string  `yaml:"otel_metrics_path"`
	BulkMaxItems              *int     `yaml:"bulk_max_items"`
	BulkConcurrency           *int     `yaml:"bulk_concurrency"`
}

func defaultConfig() Config {
	return Config{
		Transport:                 TransportStdio,
		Environment:               EnvironmentProduction,
		Listen:                    DefaultListen,
		Path:                      "/mcp",
		InvenTreeAuthScheme:       AuthSchemeToken,
		InvenTreeTimeout:          30 * time.Second,
		UploadMaxBytes:            5 * 1024 * 1024,
		MCPMaxRequestBodyBytes:    DefaultMCPMaxRequestBodyBytes,
		LogLevel:                  "info",
		OAuthKeyring:              oauth.KeyringConfig{},
		OAuthAccessLifetime:       oauth.DefaultAccessTokenLifetime,
		OAuthRefreshLifetime:      oauth.DefaultRefreshTokenLifetime,
		OAuthSessionLifetime:      oauth.DefaultSessionLifetime,
		BootstrapEnvelopeLifetime: oauth.DefaultBootstrapEnvelopeLifetime,
		Telemetry:                 telemetry.DefaultConfig(),
		BulkMaxItems:              DefaultBulkMaxItems,
		BulkConcurrency:           DefaultBulkConcurrency,
	}
}

func applyEnvironment(cfg *Config, getenv Env) {
	if raw := strings.TrimSpace(getenv(EnvTransport)); raw != "" {
		cfg.Transport = Transport(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvEnvironment)); raw != "" {
		cfg.Environment = Environment(raw)
	}
	if raw := getenv(EnvListen); raw != "" {
		cfg.Listen = raw
	}
	if raw := getenv(EnvPath); raw != "" {
		cfg.Path = raw
	}
	if raw := getenv(EnvInvenTreeURL); raw != "" {
		cfg.InvenTreeURL = raw
	}
	if raw := getenv(EnvInvenTreeWebURL); raw != "" {
		cfg.InvenTreeWebURL = raw
	}
	if raw := getenv(EnvInvenTreeToken); raw != "" {
		cfg.InvenTreeToken = raw
	}
	if raw := strings.TrimSpace(getenv(EnvInvenTreeAuthScheme)); raw != "" {
		cfg.InvenTreeAuthScheme = AuthScheme(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvInvenTreeTimeout)); raw != "" {
		cfg.InvenTreeTimeout = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvInvenTreeTLSSkipVerify)); raw != "" {
		cfg.InvenTreeTLSSkipVerify = boolEnv(getenv, EnvInvenTreeTLSSkipVerify)
	}
	if raw := getenv(EnvUploadAllowRoots); raw != "" {
		cfg.UploadAllowRoots = listEnv(getenv, EnvUploadAllowRoots)
	}
	if raw := strings.TrimSpace(getenv(EnvUploadMaxBytes)); raw != "" {
		cfg.UploadMaxBytes = int64Default(getenv, EnvUploadMaxBytes, cfg.UploadMaxBytes)
	}
	if raw := strings.TrimSpace(getenv(EnvMCPMaxRequestBodyBytes)); raw != "" {
		cfg.MCPMaxRequestBodyBytes = int64Default(getenv, EnvMCPMaxRequestBodyBytes, cfg.MCPMaxRequestBodyBytes)
	}
	if raw := getenv(EnvLogLevel); raw != "" {
		cfg.LogLevel = raw
	}
	if raw := getenv(EnvDebugTrafficLog); raw != "" {
		cfg.DebugTrafficLog = strings.TrimSpace(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvDevIncompleteOAuth)); raw != "" {
		cfg.DevIncompleteOAuth = boolEnv(getenv, EnvDevIncompleteOAuth)
	}
	if raw := getenv(EnvOAuthIssuerURL); raw != "" {
		cfg.OAuthIssuerURL = raw
	}
	if raw := getenv(EnvOAuthResourceURL); raw != "" {
		cfg.OAuthResourceURL = raw
	}
	if raw := getenv(EnvOAuthKeys); raw != "" {
		cfg.OAuthKeyring = oauth.KeyringConfig{Keys: keyListEnv(getenv, EnvOAuthKeys)}
	}
	if raw := getenv(EnvOAuthClientIDs); raw != "" {
		cfg.OAuthClientIDs = commaListEnv(getenv, EnvOAuthClientIDs)
	}
	if raw := getenv(EnvTrustedProxyCIDRs); raw != "" {
		cfg.TrustedProxyCIDRs = commaListEnv(getenv, EnvTrustedProxyCIDRs)
	}
	if raw := strings.TrimSpace(getenv(EnvOAuthAccessLifetime)); raw != "" {
		cfg.OAuthAccessLifetime = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvOAuthRefreshLifetime)); raw != "" {
		cfg.OAuthRefreshLifetime = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvOAuthSessionLifetime)); raw != "" {
		cfg.OAuthSessionLifetime = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvBootstrapEnabled)); raw != "" {
		cfg.BootstrapEnabled = boolEnv(getenv, EnvBootstrapEnabled)
	}
	if raw := strings.TrimSpace(getenv(EnvBootstrapEnvelopeLifetime)); raw != "" {
		cfg.BootstrapEnvelopeLifetime = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelEnabled)); raw != "" {
		cfg.Telemetry.Enabled = boolEnv(getenv, EnvOTelEnabled)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelServiceName)); raw != "" {
		cfg.Telemetry.ServiceName = raw
	}
	if raw := strings.TrimSpace(getenv(EnvOTelExporter)); raw != "" {
		cfg.Telemetry.Exporter = strings.ToLower(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelEndpoint)); raw != "" {
		cfg.Telemetry.Endpoint = raw
	}
	if raw := strings.TrimSpace(getenv(EnvOTelInsecure)); raw != "" {
		cfg.Telemetry.Insecure = boolEnv(getenv, EnvOTelInsecure)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelHeaders)); raw != "" {
		cfg.Telemetry.Headers = headerEnv(getenv, EnvOTelHeaders)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelSampleRatio)); raw != "" {
		cfg.Telemetry.SampleRatio = float64Default(getenv, EnvOTelSampleRatio, cfg.Telemetry.SampleRatio)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelBatchTimeout)); raw != "" {
		cfg.Telemetry.BatchTimeout = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelExportTimeout)); raw != "" {
		cfg.Telemetry.ExportTimeout = parseDurationEnv(raw)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelMetricsEnabled)); raw != "" {
		cfg.Telemetry.MetricsEnabled = boolEnv(getenv, EnvOTelMetricsEnabled)
	}
	if raw := strings.TrimSpace(getenv(EnvOTelMetricsPath)); raw != "" {
		cfg.Telemetry.MetricsPath = raw
	}
	if raw := strings.TrimSpace(getenv(EnvBulkMaxItems)); raw != "" {
		cfg.BulkMaxItems = intDefault(getenv, EnvBulkMaxItems, cfg.BulkMaxItems)
	}
	if raw := strings.TrimSpace(getenv(EnvBulkConcurrency)); raw != "" {
		cfg.BulkConcurrency = intDefault(getenv, EnvBulkConcurrency, cfg.BulkConcurrency)
	}
}

func parseDurationEnv(raw string) time.Duration {
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return invalidDuration
	}
	return parsed
}

func applyFileConfig(cfg *Config, fileCfg fileConfig) error {
	if fileCfg.Transport != nil {
		cfg.Transport = Transport(*fileCfg.Transport)
	}
	if fileCfg.Environment != nil {
		cfg.Environment = Environment(*fileCfg.Environment)
	}
	if fileCfg.Listen != nil {
		cfg.Listen = *fileCfg.Listen
	}
	if fileCfg.Path != nil {
		cfg.Path = *fileCfg.Path
	}
	if fileCfg.InvenTreeURL != nil {
		cfg.InvenTreeURL = *fileCfg.InvenTreeURL
	}
	if fileCfg.InvenTreeWebURL != nil {
		cfg.InvenTreeWebURL = *fileCfg.InvenTreeWebURL
	}
	if fileCfg.InvenTreeToken != nil {
		cfg.InvenTreeToken = *fileCfg.InvenTreeToken
	}
	if fileCfg.InvenTreeAuthScheme != nil {
		cfg.InvenTreeAuthScheme = AuthScheme(*fileCfg.InvenTreeAuthScheme)
	}
	if fileCfg.InvenTreeTimeout != nil {
		parsed, err := time.ParseDuration(*fileCfg.InvenTreeTimeout)
		if err != nil {
			return errors.New("inventree_timeout must be a valid duration")
		}
		cfg.InvenTreeTimeout = parsed
	}
	if fileCfg.InvenTreeTLSSkipVerify != nil {
		cfg.InvenTreeTLSSkipVerify = *fileCfg.InvenTreeTLSSkipVerify
	}
	if fileCfg.UploadAllowRoots != nil {
		cfg.UploadAllowRoots = append([]string(nil), fileCfg.UploadAllowRoots...)
	}
	if fileCfg.UploadMaxBytes != nil {
		cfg.UploadMaxBytes = *fileCfg.UploadMaxBytes
	}
	if fileCfg.MCPMaxRequestBodyBytes != nil {
		cfg.MCPMaxRequestBodyBytes = *fileCfg.MCPMaxRequestBodyBytes
	}
	if fileCfg.LogLevel != nil {
		cfg.LogLevel = *fileCfg.LogLevel
	}
	if fileCfg.DebugTrafficLog != nil {
		cfg.DebugTrafficLog = *fileCfg.DebugTrafficLog
	}
	if fileCfg.DevIncompleteOAuth != nil {
		cfg.DevIncompleteOAuth = *fileCfg.DevIncompleteOAuth
	}
	if fileCfg.OAuthIssuerURL != nil {
		cfg.OAuthIssuerURL = *fileCfg.OAuthIssuerURL
	}
	if fileCfg.OAuthResourceURL != nil {
		cfg.OAuthResourceURL = *fileCfg.OAuthResourceURL
	}
	if fileCfg.OAuthKeys != nil {
		keys := make([]oauth.KeyConfig, 0, len(fileCfg.OAuthKeys))
		for index, raw := range fileCfg.OAuthKeys {
			key, err := parseKeyConfig(raw)
			if err != nil {
				return fmt.Errorf("oauth_keys[%d] is invalid: %w", index, err)
			}
			keys = append(keys, key)
		}
		cfg.OAuthKeyring = oauth.KeyringConfig{Keys: keys}
	}
	if fileCfg.OAuthClientIDs != nil {
		cfg.OAuthClientIDs = append([]string(nil), fileCfg.OAuthClientIDs...)
	}
	if fileCfg.TrustedProxyCIDRs != nil {
		cfg.TrustedProxyCIDRs = append([]string(nil), fileCfg.TrustedProxyCIDRs...)
	}
	for _, field := range []struct {
		name   string
		value  *string
		target *time.Duration
	}{{"oauth_access_lifetime", fileCfg.OAuthAccessLifetime, &cfg.OAuthAccessLifetime}, {"oauth_refresh_lifetime", fileCfg.OAuthRefreshLifetime, &cfg.OAuthRefreshLifetime}, {"oauth_session_lifetime", fileCfg.OAuthSessionLifetime, &cfg.OAuthSessionLifetime}, {"bootstrap_envelope_lifetime", fileCfg.BootstrapEnvelopeLifetime, &cfg.BootstrapEnvelopeLifetime}} {
		if field.value == nil {
			continue
		}
		parsed, err := time.ParseDuration(*field.value)
		if err != nil {
			return fmt.Errorf("%s must be a valid duration", field.name)
		}
		*field.target = parsed
	}
	if fileCfg.BootstrapEnabled != nil {
		cfg.BootstrapEnabled = *fileCfg.BootstrapEnabled
	}
	if fileCfg.TelemetryMetricsEnabled != nil {
		cfg.Telemetry.MetricsEnabled = *fileCfg.TelemetryMetricsEnabled
	}
	if fileCfg.TelemetryMetricsPath != nil {
		cfg.Telemetry.MetricsPath = *fileCfg.TelemetryMetricsPath
	}
	if fileCfg.TelemetryEnabled != nil {
		cfg.Telemetry.Enabled = *fileCfg.TelemetryEnabled
	}
	if fileCfg.TelemetryServiceName != nil {
		cfg.Telemetry.ServiceName = *fileCfg.TelemetryServiceName
	}
	if fileCfg.TelemetryExporter != nil {
		cfg.Telemetry.Exporter = strings.ToLower(*fileCfg.TelemetryExporter)
	}
	if fileCfg.TelemetryEndpoint != nil {
		cfg.Telemetry.Endpoint = *fileCfg.TelemetryEndpoint
	}
	if fileCfg.TelemetryInsecure != nil {
		cfg.Telemetry.Insecure = *fileCfg.TelemetryInsecure
	}
	if fileCfg.TelemetryHeaders != nil {
		cfg.Telemetry.Headers = make(map[string]string, len(fileCfg.TelemetryHeaders))
		for index, raw := range fileCfg.TelemetryHeaders {
			key, value, err := parseHeader(raw)
			if err != nil {
				return fmt.Errorf("otel_headers[%d] is invalid: %w", index, err)
			}
			cfg.Telemetry.Headers[key] = value
		}
	}
	if fileCfg.TelemetrySampleRatio != nil {
		cfg.Telemetry.SampleRatio = *fileCfg.TelemetrySampleRatio
	}
	for _, field := range []struct {
		name   string
		value  *string
		target *time.Duration
	}{{"otel_batch_timeout", fileCfg.TelemetryBatchTimeout, &cfg.Telemetry.BatchTimeout}, {"otel_export_timeout", fileCfg.TelemetryExportTimeout, &cfg.Telemetry.ExportTimeout}} {
		if field.value == nil {
			continue
		}
		parsed, err := time.ParseDuration(*field.value)
		if err != nil {
			return fmt.Errorf("%s must be a valid duration", field.name)
		}
		*field.target = parsed
	}
	if fileCfg.BulkMaxItems != nil {
		cfg.BulkMaxItems = *fileCfg.BulkMaxItems
	}
	if fileCfg.BulkConcurrency != nil {
		cfg.BulkConcurrency = *fileCfg.BulkConcurrency
	}
	return nil
}

func configPathFromArgs(args []string) (string, bool, error) {
	var value string
	var found bool
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--config" {
			if found {
				return "", false, errors.New("--config may be specified only once")
			}
			if index+1 >= len(args) || strings.TrimSpace(args[index+1]) == "" {
				return "", false, errors.New("--config requires a file path")
			}
			index++
			value = args[index]
			found = true
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			if found {
				return "", false, errors.New("--config may be specified only once")
			}
			value = strings.TrimPrefix(arg, "--config=")
			if strings.TrimSpace(value) == "" {
				return "", false, errors.New("--config requires a file path")
			}
			found = true
		}
	}
	return value, found, nil
}

func discoverConfigPath(filesystem afero.Fs, userConfigDir func() (string, error)) (string, error) {
	paths := []string{
		"./inventree-mcp.yml",
		"./inventree-mcp.yaml",
	}
	for _, candidate := range paths {
		_, err := filesystem.Stat(candidate)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect config file %q: %w", candidate, err)
		}
	}
	userDir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine user config directory: %w", err)
	}
	paths = []string{
		filepath.Join(userDir, "inventree-mcp", "config.yml"),
		filepath.Join(userDir, "inventree-mcp", "config.yaml"),
	}
	if runtime.GOOS != "windows" {
		paths = append(paths, "/etc/inventree-mcp/config.yml", "/etc/inventree-mcp/config.yaml")
	}
	for _, candidate := range paths {
		_, err := filesystem.Stat(candidate)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect config file %q: %w", candidate, err)
		}
	}
	return "", nil
}

func loadConfigFile(filesystem afero.Fs, filename string) (fileConfig, error) {
	file, err := filesystem.Open(filename)
	if err != nil {
		return fileConfig{}, fmt.Errorf("open config file: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fileConfig{}, fmt.Errorf("stat config file: %w", err)
	}
	if requiresPrivateConfigMode() && info.Mode().Perm()&0o077 != 0 {
		return fileConfig{}, errors.New("config file must not be group- or world-readable")
	}

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var cfg fileConfig
	if err := decoder.Decode(&cfg); err != nil {
		return fileConfig{}, fmt.Errorf("decode YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fileConfig{}, errors.New("multiple YAML documents are not supported")
		}
		return fileConfig{}, fmt.Errorf("decode YAML: %w", err)
	}
	return cfg, nil
}

func requiresPrivateConfigMode() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}
