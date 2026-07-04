package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/davidvanlaatum/inventree-mcp/internal/config"
)

func TestRunRequiresServeCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(nil, &stdout, &stderr, mapEnv(nil))

	if code != 2 {
		t.Fatalf("run exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.String() != "usage: inventree-mcp serve [flags]\n" {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunServeReportsConfigErrors(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"serve", "--transport", "stdio", "--inventree-url", ""}, &stdout, &stderr, mapEnv(nil))

	if code != 2 {
		t.Fatalf("run exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("InvenTree URL is required")) {
		t.Fatalf("stderr = %q, want missing URL error", stderr.String())
	}
}

func TestRunServeStdioDoesNotWriteStdout(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"serve",
		"--transport", "stdio",
		"--inventree-url", "https://inventory.example.test",
	}, &stdout, &stderr, mapEnv(map[string]string{
		config.EnvInvenTreeToken: "redacted",
	}))

	if code != 0 {
		t.Fatalf("run exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty for STDIO transport", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunHelpReportsOutputWriteFailure(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	code := run([]string{"help"}, errWriter{}, &stderr, mapEnv(nil))

	if code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func mapEnv(values map[string]string) config.Env {
	return func(key string) string {
		return values[key]
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
