package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	localupdate "github.com/davidvanlaatum/inventree-mcp/internal/selfupdate"
	"github.com/davidvanlaatum/inventree-mcp/internal/server"
	"github.com/davidvanlaatum/inventree-mcp/internal/systemdnotify"
	"github.com/davidvanlaatum/inventree-mcp/internal/telemetry"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/davidvanlaatum/inventree-mcp/internal/upload"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
	"github.com/spf13/afero"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

var (
	serverRun         = server.Run
	newSystemdNotify  = systemdnotify.New
	buildDependencies = dependenciesForConfig
	shutdownTelemetry = func(runtime *telemetry.Runtime, ctx context.Context) error {
		return runtime.Shutdown(ctx)
	}
	runSelfUpdate = func(ctx context.Context, current string, options localupdate.Options) (localupdate.Result, error) {
		return localupdate.New(localupdate.Dependencies{}).Run(ctx, current, options)
	}
)

func run(args []string, stdout, stderr io.Writer, getenv config.Env) int {
	return runWithContext(context.Background(), args, stdout, stderr, getenv)
}

func runWithContext(parentCtx context.Context, args []string, stdout, stderr io.Writer, getenv config.Env) int {
	if len(args) == 0 {
		writeLine(stderr, "usage: inventree-mcp <serve|self-update|version> [flags]")
		return 2
	}

	switch args[0] {
	case "version", "--version":
		var flagOutput bytes.Buffer
		flags := flag.NewFlagSet("version", flag.ContinueOnError)
		flags.SetOutput(&flagOutput)
		porcelain := flags.String("porcelain", "", "print machine-readable porcelain output for the given porcelain format version (e.g. 1) instead of human-readable text")
		if err := flags.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				_, _ = io.Copy(stdout, &flagOutput)
				return 0
			}
			_, _ = io.Copy(stderr, &flagOutput)
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		if flags.NArg() != 0 {
			writeLine(stderr, "inventree-mcp: version accepts flags only")
			return 2
		}
		// An omitted --porcelain and an explicit --porcelain="" are indistinguishable here and
		// both fall back to human-readable output; requireVersion always passes a non-empty
		// buildinfo.PorcelainMarker, so this ambiguity is unreachable from self-update.
		if *porcelain == "" {
			writeLine(stdout, "inventree-mcp %s (commit %s, built %s)", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
			if buildinfo.PinnedInvenTreeVersion != "" {
				writeLine(stdout, "InvenTree baseline: %s (API %s)", buildinfo.PinnedInvenTreeVersion, buildinfo.PinnedInvenTreeAPIVersion)
			}
			return 0
		}
		// This only ever matches the single format version this build currently produces; it
		// does not yet serve older formats on request. The requested marker is taken as an
		// explicit argument (rather than assumed) so that a future format bump can add
		// per-version output selection here without changing this command's calling contract.
		if *porcelain != buildinfo.PorcelainMarker {
			writeLine(stderr, "inventree-mcp: unsupported porcelain marker %q; this build supports %q", *porcelain, buildinfo.PorcelainMarker)
			return 2
		}
		for _, line := range buildinfo.PorcelainLines() {
			writeLine(stdout, "%s", line)
		}
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
		ctx, err := platform.NewRootContext(parentCtx, platform.LoggerConfig{
			Level:  cfg.LogLevel,
			Output: stderr,
		})
		if err != nil {
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		if err := serve(ctx, cfg); err != nil {
			if err == context.Canceled && parentCtx.Err() != nil {
				return 0
			}
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		return 0
	case "self-update":
		var flagOutput bytes.Buffer
		flags := flag.NewFlagSet("self-update", flag.ContinueOnError)
		flags.SetOutput(&flagOutput)
		targetVersion := flags.String("version", "", "install an exact stable vX.Y.Z release instead of latest")
		adoptDirectInstall := flags.Bool("adopt-direct-install", false, "record this binary as a direct canonical GitHub archive installation")
		if err := flags.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				_, _ = io.Copy(stdout, &flagOutput)
				return 0
			}
			_, _ = io.Copy(stderr, &flagOutput)
			writeLine(stderr, "inventree-mcp: %v", err)
			return 2
		}
		if flags.NArg() != 0 {
			writeLine(stderr, "inventree-mcp: self-update accepts flags only")
			return 2
		}
		result, err := runSelfUpdate(parentCtx, buildinfo.Version, localupdate.Options{
			TargetVersion:      *targetVersion,
			GitHubToken:        getenv("GITHUB_TOKEN"),
			AdoptDirectInstall: *adoptDirectInstall,
		})
		if err != nil {
			writeLine(stderr, "inventree-mcp: self-update failed: %v", err)
			return 2
		}
		if !result.Updated {
			if result.Adopted {
				writeLine(stdout, "recorded this %s binary as a direct canonical GitHub archive installation", result.Version)
				return 0
			}
			writeLine(stdout, "inventree-mcp is already at %s", result.Version)
			return 0
		}
		writeLine(stdout, "updated inventree-mcp from %s to %s", result.PreviousVersion, result.Version)
		if result.NewInvenTreeVersion != "" {
			writeLine(stdout, "InvenTree baseline: %s (API %s) -> %s (API %s)", result.PreviousInvenTreeVersion, result.PreviousInvenTreeAPI, result.NewInvenTreeVersion, result.NewInvenTreeAPI)
		}
		writeLine(stdout, "previous binary: %s", result.BackupPath)
		return 0
	case "help", "-h", "--help":
		writeLine(stdout, "usage: inventree-mcp <serve|self-update|version> [flags]")
		return 0
	default:
		writeLine(stderr, "inventree-mcp: unknown command %q", args[0])
		return 2
	}
}

func serve(ctx context.Context, cfg config.Config) error {
	logger := logging.FromContext(ctx)
	notifier := newSystemdNotify()
	managedHTTP := cfg.Transport == config.TransportHTTP
	if managedHTTP {
		if err := notifier.Starting(); err != nil {
			return fmt.Errorf("notify systemd of service startup: %w", err)
		}
	}
	telemetryRuntime, err := telemetry.New(ctx, cfg.Telemetry)
	if err != nil {
		if managedHTTP {
			notifyFatal(logger, notifier)
		}
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Telemetry.ExportTimeout)
		defer cancel()
		if err := shutdownTelemetry(telemetryRuntime, shutdownCtx); err != nil {
			logger.Error("failed to flush OpenTelemetry traces", logging.Err(err))
		}
	}()
	deps, err := buildDependencies(cfg)
	if err != nil {
		if managedHTTP {
			notifyFatal(logger, notifier)
		}
		return err
	}
	err = serverRun(ctx, cfg, deps, notifier)
	if managedHTTP && err != nil && err != context.Canceled {
		notifyFatal(logger, notifier)
	}
	return err
}

func notifyFatal(logger *slog.Logger, notifier systemdnotify.Notifier) {
	if err := notifier.Fatal(); err != nil {
		logger.Error("failed to notify systemd of fatal service error", logging.Err(err))
	}
}

func dependenciesForConfig(cfg config.Config) (tools.Dependencies, error) {
	var webLinks *weblinks.Resolver
	if cfg.InvenTreeURL != "" || cfg.InvenTreeWebURL != "" {
		var err error
		webLinks, err = cfg.WebLinkResolver()
		if err != nil {
			return tools.Dependencies{}, err
		}
	}
	if cfg.Transport == config.TransportHTTP && cfg.Environment == config.EnvironmentProduction {
		return tools.Dependencies{
			EnableWriteTools:    true,
			AuthorizationMode:   tools.AuthorizationModeOAuth,
			ResourceMetadataURL: cfg.OAuthProtectedResourceMetadataURL(),
			UploadMode:          upload.ModeHTTP,
			UploadMaxBytes:      cfg.UploadMaxBytes,
			UploadTimeout:       cfg.InvenTreeTimeout,
			ClientFromContext:   server.OAuthClientFromContext(cfg.InvenTreeURL, inventreeHTTPClient(cfg)),
			WebLinks:            webLinks,
			BulkMaxItems:        cfg.BulkMaxItems,
			BulkConcurrency:     cfg.BulkConcurrency,
		}, nil
	}
	if cfg.Transport != config.TransportStdio {
		return tools.Dependencies{WebLinks: webLinks}, nil
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
		WebLinks:         webLinks,
		BulkMaxItems:     cfg.BulkMaxItems,
		BulkConcurrency:  cfg.BulkConcurrency,
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
	return telemetry.WrapHTTPClient(&http.Client{
		Timeout:   cfg.InvenTreeTimeout,
		Transport: transport,
	})
}

func writeLine(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}
