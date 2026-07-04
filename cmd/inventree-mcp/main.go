package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/server"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

var serverRun = server.Run

func run(args []string, stdout, stderr io.Writer, getenv config.Env) int {
	if len(args) == 0 {
		writeLine(stderr, "usage: inventree-mcp <serve|version> [flags]")
		return 2
	}

	switch args[0] {
	case "version", "--version":
		writeLine(stdout, "version: %s", buildinfo.Version)
		writeLine(stdout, "commit: %s", buildinfo.Commit)
		writeLine(stdout, "date: %s", buildinfo.Date)
		return 0
	case "serve":
		var flagOutput bytes.Buffer
		cfg, err := config.ParseServeWithEnv(args[1:], getenv, &flagOutput)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				_, _ = io.Copy(stdout, &flagOutput)
				return 0
			}
			_, _ = io.Copy(stderr, &flagOutput)
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		ctx, err := platform.NewRootContext(context.Background(), platform.LoggerConfig{
			Level:  cfg.LogLevel,
			Output: stderr,
		})
		if err != nil {
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		if err := serve(ctx, cfg); err != nil {
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		return 0
	case "help", "-h", "--help":
		writeLine(stdout, "usage: inventree-mcp <serve|version> [flags]")
		return 0
	default:
		writeLine(stderr, "inventree-mcp: unknown command %q", args[0])
		return 2
	}
}

func serve(ctx context.Context, cfg config.Config) error {
	_ = logging.FromContext(ctx)
	return serverRun(ctx, cfg, tools.Dependencies{})
}

func writeLine(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}
