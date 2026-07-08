package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/server"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/spf13/afero"
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
	deps, err := dependenciesForConfig(cfg)
	if err != nil {
		return err
	}
	return serverRun(ctx, cfg, deps)
}

func dependenciesForConfig(cfg config.Config) (tools.Dependencies, error) {
	if cfg.Transport == config.TransportHTTP && cfg.Environment == config.EnvironmentProduction {
		return tools.Dependencies{
			EnableWriteTools:    true,
			AuthorizationMode:   tools.AuthorizationModeOAuth,
			ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
			UploadMode:          upload.ModeHTTP,
			UploadMaxBytes:      cfg.UploadMaxBytes,
			UploadTimeout:       cfg.InvenTreeTimeout,
			ClientFromContext:   server.OAuthClientFromContext(cfg.InvenTreeURL, inventreeHTTPClient(cfg)),
		}, nil
	}
	if cfg.Transport != config.TransportStdio {
		return tools.Dependencies{}, nil
	}
	client, err := inventree.NewClient(inventree.Config{
		BaseURL: cfg.InvenTreeURL,
		Credential: inventree.Credential{
			Scheme: inventree.AuthScheme(cfg.InvenTreeAuthScheme),
			Token:  cfg.InvenTreeToken,
		},
		HTTPClient: inventreeHTTPClient(cfg),
	})
	if err != nil {
		return tools.Dependencies{}, err
	}
	return tools.Dependencies{
		EnableWriteTools: true,
		UploadMode:       upload.ModeStdio,
		UploadFS:         afero.NewOsFs(),
		UploadAllowRoots: cfg.UploadAllowRoots,
		UploadMaxBytes:   cfg.UploadMaxBytes,
		UploadTimeout:    cfg.InvenTreeTimeout,
		ClientFromContext: func(context.Context) (any, error) {
			return client, nil
		},
	}, nil
}

func inventreeHTTPClient(cfg config.Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InvenTreeTLSSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Config validation rejects this in production.
	}
	return &http.Client{
		Timeout:   cfg.InvenTreeTimeout,
		Transport: transport,
	}
}

func writeLine(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}
