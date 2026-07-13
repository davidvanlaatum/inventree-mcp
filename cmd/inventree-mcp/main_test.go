package main

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRequiresServeCommand(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(nil, &stdout, &stderr, mapEnv(nil))

	r.Equal(2, code)
	r.Empty(stdout.String())
	r.Equal("usage: inventree-mcp <serve|version> [flags]\n", stderr.String())
}

func TestRunServeReportsConfigErrors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"serve", "--transport", "stdio", "--inventree-url", ""}, &stdout, &stderr, mapEnv(nil))

	r.Equal(2, code)
	a.Empty(stdout.String())
	a.Contains(stderr.String(), "InvenTree URL is required")
}

func TestRunServeHelpExitsSuccessfully(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"serve", "--help"}, &stdout, &stderr, mapEnv(nil))

	r.Equal(0, code)
	a.Contains(stdout.String(), "Usage of serve:")
	a.Empty(stderr.String())
}

func TestRunVersionReportsBuildVersion(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr, mapEnv(nil))

	r.Equal(0, code)
	a.Equal("version: dev\ncommit: unknown\ndate: unknown\n", stdout.String())
	a.Empty(stderr.String())
}

func TestRunServeStdioDoesNotWriteStdout(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	originalServerRun := serverRun
	t.Cleanup(func() {
		serverRun = originalServerRun
	})

	var gotConfig config.Config
	var gotLoggerContext bool
	var gotDependencies tools.Dependencies
	serverRun = func(ctx context.Context, cfg config.Config, deps tools.Dependencies) error {
		gotConfig = cfg
		gotLoggerContext = logging.FromContext(ctx) != nil
		gotDependencies = deps
		return nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"serve",
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
	}, &stdout, &stderr, mapEnv(map[string]string{
		config.EnvInvenTreeToken: "redacted",
	}))

	r.Equal(0, code)
	a.Empty(stdout.String())
	a.Empty(stderr.String())
	a.Equal(config.TransportStdio, gotConfig.Transport)
	a.True(gotLoggerContext)
	a.True(gotDependencies.EnableWriteTools)
	r.NotNil(gotDependencies.ClientFromContext)

	client, err := gotDependencies.ClientFromContext(context.Background())
	r.NoError(err)
	_, ok := client.(*inventree.Client)
	a.True(ok)
}

func TestDependenciesForConfigLeavesDevelopmentHTTPClientUnavailable(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	deps, err := dependenciesForConfig(config.Config{
		Transport:   config.TransportHTTP,
		Environment: config.EnvironmentDevelopment,
	})

	r.NoError(err)
	a.Nil(deps.ClientFromContext)
	a.False(deps.EnableWriteTools)
}

func TestDependenciesForConfigBuildsProductionHTTPOAuthDependencies(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	deps, err := dependenciesForConfig(config.Config{
		Transport:           config.TransportHTTP,
		Environment:         config.EnvironmentProduction,
		InvenTreeURL:        "https://inventory.example.test",
		InvenTreeTimeout:    5 * time.Second,
		OAuthIssuerURL:      "https://mcp.example.test",
		OAuthResourceURL:    "https://mcp.example.test/mcp",
		UploadMaxBytes:      1234,
		OAuthAccessLifetime: 10 * time.Minute,
	})

	r.NoError(err)
	a.True(deps.EnableWriteTools)
	a.Equal(tools.AuthorizationModeOAuth, deps.AuthorizationMode)
	a.Equal("https://mcp.example.test/.well-known/oauth-protected-resource", deps.ResourceMetadataURL)
	a.Equal(int64(1234), deps.UploadMaxBytes)
	a.Equal(5*time.Second, deps.UploadTimeout)
	r.NotNil(deps.ClientFromContext)
	_, err = deps.ClientFromContext(context.Background())
	a.ErrorContains(err, "OAuth credential unavailable")
}

func TestDependenciesForConfigBuildsStdioClient(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	deps, err := dependenciesForConfig(config.Config{
		Transport:              config.TransportStdio,
		InvenTreeURL:           "https://inventory.example.test",
		InvenTreeToken:         "redacted",
		InvenTreeAuthScheme:    config.AuthSchemeBearer,
		InvenTreeTimeout:       5 * time.Second,
		InvenTreeTLSSkipVerify: true,
		UploadAllowRoots:       []string{"/tmp/uploads"},
		UploadMaxBytes:         1234,
	})

	r.NoError(err)
	r.True(deps.EnableWriteTools)
	r.Equal([]string{"/tmp/uploads"}, deps.UploadAllowRoots)
	r.Equal(int64(1234), deps.UploadMaxBytes)
	r.Equal(5*time.Second, deps.UploadTimeout)
	r.NotNil(deps.ClientFromContext)
	client, err := deps.ClientFromContext(context.Background())
	r.NoError(err)
	r.IsType(&inventree.Client{}, client)
}

func TestInvenTreeHTTPClientUsesConfiguredTimeout(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	client := inventreeHTTPClient(config.Config{InvenTreeTimeout: 7 * time.Second})

	a.Equal(7*time.Second, client.Timeout)
	a.IsType(&http.Transport{}, client.Transport)
}

func TestRunServeReportsInvalidLogLevel(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"serve",
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
		"--log-level", "verbose",
	}, &stdout, &stderr, mapEnv(map[string]string{
		config.EnvInvenTreeToken: "redacted",
	}))

	r.Equal(2, code)
	a.Empty(stdout.String())
	a.Contains(stderr.String(), "log level must be")
}

func mapEnv(values map[string]string) config.Env {
	return func(key string) string {
		return values[key]
	}
}
