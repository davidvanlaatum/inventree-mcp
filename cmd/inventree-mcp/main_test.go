package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
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
	a.Equal(tools.Dependencies{}, gotDependencies)
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
