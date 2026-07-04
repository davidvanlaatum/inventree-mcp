package main

import (
	"bytes"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/internal/config"
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
	r.Equal("usage: inventree-mcp serve [flags]\n", stderr.String())
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

func TestRunServeStdioDoesNotWriteStdout(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

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
}

func mapEnv(values map[string]string) config.Env {
	return func(key string) string {
		return values[key]
	}
}
