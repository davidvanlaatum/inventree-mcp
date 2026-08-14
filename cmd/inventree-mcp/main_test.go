package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/davidvanlaatum/inventree-mcp/internal/config"
	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/davidvanlaatum/inventree-mcp/internal/selfupdate"
	"github.com/davidvanlaatum/inventree-mcp/internal/systemdnotify"
	"github.com/davidvanlaatum/inventree-mcp/internal/tools"
	"github.com/davidvanlaatum/inventree-mcp/internal/weblinks"
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
	r.Equal("usage: inventree-mcp <serve|self-update|version> [flags]\n", stderr.String())
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

func TestRunServeTreatsRootCancellationAsGracefulShutdown(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	originalServerRun := serverRun
	t.Cleanup(func() {
		serverRun = originalServerRun
	})
	serverRun = func(ctx context.Context, _ config.Config, _ tools.Dependencies, _ systemdnotify.Notifier) error {
		return ctx.Err()
	}
	parentCtx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithContext(parentCtx, []string{
		"serve",
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
	}, &stdout, &stderr, mapEnv(nil))

	r.Equal(0, code)
	a.Empty(stdout.String())
	a.Empty(stderr.String())
}

func TestRunServeReportsShutdownFailureAfterRootCancellation(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	originalServerRun := serverRun
	t.Cleanup(func() {
		serverRun = originalServerRun
	})
	shutdownErr := errors.New("shutdown failed")
	serverRun = func(ctx context.Context, _ config.Config, _ tools.Dependencies, _ systemdnotify.Notifier) error {
		return errors.Join(ctx.Err(), shutdownErr)
	}
	parentCtx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runWithContext(parentCtx, []string{
		"serve",
		"--transport", "http",
		"--environment", "development",
		"--dev-incomplete-oauth",
		"--inventree-url", "https://inventory.example.test",
	}, &stdout, &stderr, mapEnv(nil))

	r.Equal(2, code)
	a.Empty(stdout.String())
	a.Contains(stderr.String(), shutdownErr.Error())
}

func TestRunServeRedactsMalformedOAuthKeyFlag(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	const sensitiveValue = "broken:active:super-secret-key-material:extra"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"serve", "--oauth-key", sensitiveValue}, &stdout, &stderr, mapEnv(nil))

	r.Equal(2, code)
	a.Empty(stdout.String())
	a.Contains(stderr.String(), "flag provided but not defined: -oauth-key")
	a.NotContains(stderr.String(), sensitiveValue)
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
	code := run([]string{"version"}, &stdout, &stderr, func(key string) string {
		a.Fail("version command accessed configuration", key)
		return "secret"
	})

	r.Equal(0, code)
	a.Equal("version: dev\ncommit: unknown\ndate: unknown\n", stdout.String())
	a.Empty(stderr.String())
}

func TestRunSelfUpdateReportsNoOpAndPassesTargetAndToken(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	original := runSelfUpdate
	t.Cleanup(func() { runSelfUpdate = original })
	runSelfUpdate = func(_ context.Context, current string, options selfupdate.Options) (selfupdate.Result, error) {
		a.Equal("dev", current)
		a.Equal("v1.2.3", options.TargetVersion)
		a.Equal("secret-token", options.GitHubToken)
		a.False(options.AdoptDirectInstall)
		return selfupdate.Result{PreviousVersion: "v1.2.3", Version: "v1.2.3"}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"self-update", "--version", "v1.2.3"}, &stdout, &stderr, mapEnv(map[string]string{"GITHUB_TOKEN": "secret-token"}))

	r.Equal(0, code)
	a.Equal("inventree-mcp is already at v1.2.3\n", stdout.String())
	a.Empty(stderr.String())
}

func TestRunSelfUpdateAdoptsDirectInstall(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)
	original := runSelfUpdate
	t.Cleanup(func() { runSelfUpdate = original })
	runSelfUpdate = func(_ context.Context, current string, options selfupdate.Options) (selfupdate.Result, error) {
		a.Equal("dev", current)
		a.True(options.AdoptDirectInstall)
		return selfupdate.Result{Version: "v1.2.3", Adopted: true}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"self-update", "--adopt-direct-install"}, &stdout, &stderr, mapEnv(nil))

	r.Equal(0, code)
	a.Contains(stdout.String(), "recorded this v1.2.3 binary")
	a.Empty(stderr.String())
}

func TestRunSelfUpdateReportsSuccess(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	original := runSelfUpdate
	t.Cleanup(func() { runSelfUpdate = original })
	runSelfUpdate = func(context.Context, string, selfupdate.Options) (selfupdate.Result, error) {
		return selfupdate.Result{PreviousVersion: "v1.2.2", Version: "v1.2.3", BackupPath: "/safe/inventree-mcp.previous", Updated: true}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"self-update"}, &stdout, &stderr, mapEnv(nil))

	r.Equal(0, code)
	a.Equal("updated inventree-mcp from v1.2.2 to v1.2.3\nprevious binary: /safe/inventree-mcp.previous\n", stdout.String())
	a.Empty(stderr.String())
}

func TestRunSelfUpdateDoesNotLeakGitHubTokenOnFailure(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	original := runSelfUpdate
	t.Cleanup(func() { runSelfUpdate = original })
	runSelfUpdate = func(context.Context, string, selfupdate.Options) (selfupdate.Result, error) {
		return selfupdate.Result{}, errors.New("release lookup failed")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"self-update"}, &stdout, &stderr, mapEnv(map[string]string{"GITHUB_TOKEN": "secret-token"}))

	r.Equal(2, code)
	a.Empty(stdout.String())
	a.Contains(stderr.String(), "release lookup failed")
	a.NotContains(stderr.String(), "secret-token")
}

func TestRunSelfUpdateHelp(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"self-update", "--help"}, &stdout, &stderr, mapEnv(nil))

	r.Equal(0, code)
	a.Contains(stdout.String(), "Usage of self-update:")
	a.Contains(stdout.String(), "-version string")
	a.Empty(stderr.String())
}

func TestRunSelfUpdateRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"self-update", "--unknown"}, want: "flag provided but not defined"},
		{name: "positional argument", args: []string{"self-update", "v1.2.3"}, want: "self-update accepts flags only"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := run(test.args, &stdout, &stderr, mapEnv(nil))

			r.Equal(2, code)
			a.Empty(stdout.String())
			a.Contains(stderr.String(), test.want)
		})
	}
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
	serverRun = func(ctx context.Context, cfg config.Config, deps tools.Dependencies, _ systemdnotify.Notifier) error {
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
	a.Equal("https://mcp.example.test/.well-known/oauth-protected-resource/mcp", deps.ResourceMetadataURL)
	a.Equal(int64(1234), deps.UploadMaxBytes)
	a.Equal(5*time.Second, deps.UploadTimeout)
	r.NotNil(deps.ClientFromContext)
	r.NotNil(deps.WebLinks)
	a.Equal("https://inventory.example.test/web/part/7/", deps.WebLinks.URL(weblinks.Part, 7))
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
	r.NotNil(deps.WebLinks)
}

func TestDependenciesForConfigRejectsUnsafeEffectiveWebBase(t *testing.T) {
	t.Parallel()
	_, err := dependenciesForConfig(config.Config{
		Transport:        config.TransportHTTP,
		Environment:      config.EnvironmentProduction,
		InvenTreeURL:     "https://inventory.example.test",
		InvenTreeWebURL:  "https://secret:password@browser.example.test",
		InvenTreeTimeout: time.Second,
	})
	require.ErrorContains(t, err, "INVENTREE_WEB_URL must not include userinfo")
	assert.NotContains(t, err.Error(), "secret")
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

func TestServePublishesSystemdStartupAndFatalStatus(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	originalServerRun := serverRun
	originalNewSystemdNotify := newSystemdNotify
	t.Cleanup(func() {
		serverRun = originalServerRun
		newSystemdNotify = originalNewSystemdNotify
	})

	notifier := &recordingNotifier{}
	newSystemdNotify = func() systemdnotify.Notifier {
		return notifier
	}
	wantErr := errors.New("serve failed")
	serverRun = func(context.Context, config.Config, tools.Dependencies, systemdnotify.Notifier) error {
		return wantErr
	}
	ctx := loggingTestContext(t)

	err := serve(ctx, config.Config{
		Transport:   config.TransportHTTP,
		Environment: config.EnvironmentDevelopment,
	})

	r.ErrorIs(err, wantErr)
	a.Equal([]string{"starting", "fatal"}, notifier.events)
}

func TestServeFailsWhenSystemdStartupStatusCannotBeSent(t *testing.T) {
	r := require.New(t)

	originalServerRun := serverRun
	originalNewSystemdNotify := newSystemdNotify
	t.Cleanup(func() {
		serverRun = originalServerRun
		newSystemdNotify = originalNewSystemdNotify
	})

	wantErr := errors.New("notify failed")
	notifier := &recordingNotifier{startingErr: wantErr}
	newSystemdNotify = func() systemdnotify.Notifier {
		return notifier
	}
	serverRun = func(context.Context, config.Config, tools.Dependencies, systemdnotify.Notifier) error {
		r.Fail("server started after systemd startup notification failed")
		return nil
	}
	ctx := loggingTestContext(t)

	err := serve(ctx, config.Config{Transport: config.TransportHTTP})

	r.ErrorIs(err, wantErr)
	r.Equal([]string{"starting"}, notifier.events)
}

func TestServePublishesFatalStatusWhenManagedDependenciesFail(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	originalServerRun := serverRun
	originalNewSystemdNotify := newSystemdNotify
	originalBuildDependencies := buildDependencies
	t.Cleanup(func() {
		serverRun = originalServerRun
		newSystemdNotify = originalNewSystemdNotify
		buildDependencies = originalBuildDependencies
	})

	notifier := &recordingNotifier{}
	newSystemdNotify = func() systemdnotify.Notifier {
		return notifier
	}
	wantErr := errors.New("dependencies failed")
	buildDependencies = func(config.Config) (tools.Dependencies, error) {
		return tools.Dependencies{}, wantErr
	}
	serverRun = func(context.Context, config.Config, tools.Dependencies, systemdnotify.Notifier) error {
		r.Fail("server started after managed dependency initialization failed")
		return nil
	}
	ctx := loggingTestContext(t)

	err := serve(ctx, config.Config{Transport: config.TransportHTTP})

	r.ErrorIs(err, wantErr)
	a.Equal([]string{"starting", "fatal"}, notifier.events)
}

func TestServeLogsFatalNotificationFailureWithoutReplacingServerError(t *testing.T) {
	r := require.New(t)
	a := assert.New(t)

	originalServerRun := serverRun
	originalNewSystemdNotify := newSystemdNotify
	t.Cleanup(func() {
		serverRun = originalServerRun
		newSystemdNotify = originalNewSystemdNotify
	})

	notifyErr := errors.New("fatal notification failed")
	notifier := &recordingNotifier{fatalErr: notifyErr}
	newSystemdNotify = func() systemdnotify.Notifier {
		return notifier
	}
	serveErr := errors.New("serve failed")
	serverRun = func(context.Context, config.Config, tools.Dependencies, systemdnotify.Notifier) error {
		return serveErr
	}
	var stderr bytes.Buffer
	ctx, err := platform.NewRootContext(t.Context(), platform.LoggerConfig{Output: &stderr})
	r.NoError(err)

	err = serve(ctx, config.Config{
		Transport:   config.TransportHTTP,
		Environment: config.EnvironmentDevelopment,
	})

	r.ErrorIs(err, serveErr)
	a.Contains(stderr.String(), "failed to notify systemd of fatal service error")
	a.Contains(stderr.String(), notifyErr.Error())
	a.Equal([]string{"starting", "fatal"}, notifier.events)
}

func loggingTestContext(t *testing.T) context.Context {
	t.Helper()
	var stderr bytes.Buffer
	ctx, err := platform.NewRootContext(t.Context(), platform.LoggerConfig{Output: &stderr})
	require.New(t).NoError(err)
	return ctx
}

type recordingNotifier struct {
	events      []string
	startingErr error
	fatalErr    error
}

func (n *recordingNotifier) Starting() error {
	n.events = append(n.events, "starting")
	return n.startingErr
}

func (n *recordingNotifier) Ready() error {
	n.events = append(n.events, "ready")
	return nil
}

func (n *recordingNotifier) RunWatchdog(context.Context, func(error)) {}

func (n *recordingNotifier) Degraded() error {
	n.events = append(n.events, "degraded")
	return nil
}

func (n *recordingNotifier) Stopping() error {
	n.events = append(n.events, "stopping")
	return nil
}

func (n *recordingNotifier) Fatal() error {
	n.events = append(n.events, "fatal")
	return n.fatalErr
}

func mapEnv(values map[string]string) config.Env {
	return func(key string) string {
		return values[key]
	}
}
