package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/inventree"
	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	DefaultInvenTreeImage = "inventree/inventree:1.4.0"
	DefaultAPIVersion     = "511"
	DefaultVersion        = "1.4.0"

	EnvSkipDocker = "INVENTREE_TEST_SKIP_DOCKER"
)

const cleanupTimeout = 30 * time.Second

const (
	defaultPostgresImage = "postgres:17"
	defaultRedisImage    = "redis:7-alpine"
	defaultWebPort       = "8000/tcp"
	defaultDBName        = "inventree"
	defaultDBUser        = "inventree"
	defaultDBPassword    = "inventree-test-password"
	defaultAdminUser     = "admin"
	defaultAdminEmail    = "admin@example.test"
	defaultAdminPassword = "inventree-test-admin-password"
	defaultTokenName     = "inventree-mcp-test"
)

type Options struct {
	Image              string
	ExpectedVersion    string
	ExpectedAPIVersion string
	StartupTimeout     time.Duration
	HTTPClient         *http.Client
	// ContainerLogf receives stdout and stderr lines from started containers.
	// Start serializes calls so callbacks do not need to be concurrency-safe.
	ContainerLogf func(container string, stream string, line string)
}

type Environment struct {
	BaseURL    string
	Token      string
	Image      string
	Version    string
	APIVersion string

	containers []testcontainers.Container
	network    *testcontainers.DockerNetwork
}

// CleanupFunc tears down a started test environment with a bounded timeout.
type CleanupFunc func() error

type versionResponse struct {
	Version struct {
		Server string `json:"server"`
		API    int    `json:"api"`
	} `json:"version"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

func DefaultOptions() Options {
	return Options{
		Image:              DefaultInvenTreeImage,
		ExpectedVersion:    DefaultVersion,
		ExpectedAPIVersion: DefaultAPIVersion,
		StartupTimeout:     3 * time.Minute,
	}
}

// DefaultTestOptions returns default options with container logs forwarded to tb.
func DefaultTestOptions(tb testing.TB) Options {
	tb.Helper()
	opts := DefaultOptions()
	opts.ContainerLogf = func(container string, stream string, line string) {
		tb.Helper()
		tb.Logf("container[%s][%s] %s", container, stream, line)
	}
	return opts
}

// CleanupForTest wraps cleanup so it can be passed directly to testing.T.Cleanup.
func CleanupForTest(tb testing.TB, cleanup CleanupFunc) func() {
	tb.Helper()
	return func() {
		tb.Helper()
		if cleanup == nil {
			return
		}
		if err := cleanup(); err != nil {
			tb.Errorf("clean up InvenTree test environment: %v", err)
		}
	}
}

func SkipDocker(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	value := strings.TrimSpace(strings.ToLower(getenv(EnvSkipDocker)))
	return value == "1" || value == "true" || value == "yes"
}

func ValidateOptions(opts Options) error {
	var errs []error

	if opts.Image == "" {
		errs = append(errs, errors.New("InvenTree image is required"))
	} else if err := validateExplicitImageTag(opts.Image); err != nil {
		errs = append(errs, err)
	}
	if opts.ExpectedVersion == "" {
		errs = append(errs, errors.New("expected InvenTree version is required"))
	}
	if opts.ExpectedAPIVersion == "" {
		errs = append(errs, errors.New("expected InvenTree API version is required"))
	}
	if opts.StartupTimeout < 0 {
		errs = append(errs, errors.New("startup timeout must not be negative"))
	}

	return errors.Join(errs...)
}

func Start(ctx context.Context, opts Options) (*Environment, CleanupFunc, error) {
	if opts.Image == "" {
		opts.Image = DefaultInvenTreeImage
	}
	if opts.ExpectedVersion == "" {
		opts.ExpectedVersion = DefaultVersion
	}
	if opts.ExpectedAPIVersion == "" {
		opts.ExpectedAPIVersion = DefaultAPIVersion
	}
	if opts.StartupTimeout == 0 {
		opts.StartupTimeout = 3 * time.Minute
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	containerLogf := synchronizedContainerLogf(opts.ContainerLogf)
	if err := ValidateOptions(opts); err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, opts.StartupTimeout)
	defer cancel()

	env := &Environment{Image: opts.Image}
	var started bool
	defer func() {
		if !started {
			_ = closeWithTimeout(env)
		}
	}()

	nw, err := tcnetwork.New(ctx, tcnetwork.WithDriver("bridge"))
	if err != nil {
		return nil, nil, fmt.Errorf("create InvenTree test network: %w", err)
	}
	env.network = nw

	postgresOpts := []testcontainers.ContainerCustomizer{
		postgres.WithDatabase(defaultDBName),
		postgres.WithUsername(defaultDBUser),
		postgres.WithPassword(defaultDBPassword),
		testcontainers.WithAdditionalWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second)),
		testcontainers.WithHostConfigModifier(loopbackPortBinding("5432/tcp")),
		tcnetwork.WithNetwork([]string{"inventree-db"}, nw),
	}
	postgresOpts = appendContainerLogConsumer(postgresOpts, "postgres", containerLogf)
	pg, err := postgres.Run(ctx, defaultPostgresImage, postgresOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("start InvenTree postgres: %w", err)
	}
	env.containers = append(env.containers, pg)
	dbHost, err := pg.ContainerIP(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve InvenTree postgres IP: %w", err)
	}

	redisOpts := []testcontainers.ContainerCustomizer{
		tcnetwork.WithNetwork([]string{"inventree-cache"}, nw),
		testcontainers.WithHostConfigModifier(loopbackPortBinding("6379/tcp")),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections").WithStartupTimeout(30 * time.Second)),
	}
	redisOpts = appendContainerLogConsumer(redisOpts, "redis", containerLogf)
	redis, err := testcontainers.Run(ctx, defaultRedisImage, redisOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("start InvenTree redis: %w", err)
	}
	env.containers = append(env.containers, redis)
	cacheHost, err := redis.ContainerIP(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve InvenTree redis IP: %w", err)
	}

	serverOpts := []testcontainers.ContainerCustomizer{
		tcnetwork.WithNetwork([]string{"inventree-server"}, nw),
		testcontainers.WithEnv(inventreeContainerEnv(dbHost, cacheHost)),
		testcontainers.WithCmd("sh", "-c", "invoke update --skip-backup --no-frontend --skip-static && exec gunicorn -c ./gunicorn.conf.py InvenTree.wsgi -b ${INVENTREE_WEB_ADDR}:${INVENTREE_WEB_PORT} --chdir ${INVENTREE_BACKEND_DIR}/InvenTree"),
		testcontainers.WithExposedPorts(defaultWebPort),
		testcontainers.WithHostConfigModifier(loopbackPortBinding(defaultWebPort)),
		testcontainers.WithWaitStrategyAndDeadline(opts.StartupTimeout, wait.ForHTTP("/api/version/").
			WithPort(defaultWebPort).
			WithStatusCodeMatcher(func(status int) bool {
				return status == http.StatusOK || status == http.StatusUnauthorized || status == http.StatusForbidden
			}).
			WithStartupTimeout(opts.StartupTimeout)),
	}
	serverOpts = appendContainerLogConsumer(serverOpts, "inventree", containerLogf)
	server, err := testcontainers.Run(ctx, opts.Image, serverOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("start InvenTree server: %w", err)
	}
	env.containers = append(env.containers, server)

	baseURL, err := server.PortEndpoint(ctx, defaultWebPort, "http")
	if err != nil {
		return nil, nil, fmt.Errorf("resolve InvenTree endpoint: %w", err)
	}
	env.BaseURL = strings.TrimRight(baseURL, "/")

	version, err := fetchVersion(ctx, opts.HTTPClient, env.BaseURL, defaultAdminUser, defaultAdminPassword)
	if err != nil {
		return nil, nil, err
	}
	apiVersion := fmt.Sprintf("%d", version.Version.API)
	if version.Version.Server != opts.ExpectedVersion || apiVersion != opts.ExpectedAPIVersion {
		return nil, nil, fmt.Errorf("InvenTree runtime version mismatch: got version %q API %q, want version %q API %q", version.Version.Server, apiVersion, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	}
	env.Version = version.Version.Server
	env.APIVersion = apiVersion

	token, err := createToken(ctx, opts.HTTPClient, env.BaseURL, defaultAdminUser, defaultAdminPassword)
	if err != nil {
		return nil, nil, err
	}
	env.Token = token

	if err := proveToken(ctx, env.BaseURL, token, opts.HTTPClient); err != nil {
		return nil, nil, err
	}

	started = true
	return env, func() error {
		return closeWithTimeout(env)
	}, nil
}

func (e *Environment) Close(ctx context.Context) error {
	var errs []error
	for i := len(e.containers) - 1; i >= 0; i-- {
		if e.containers[i] != nil {
			errs = append(errs, e.containers[i].Terminate(ctx))
		}
	}
	if e.network != nil {
		errs = append(errs, e.network.Remove(ctx))
	}
	return errors.Join(errs...)
}

func closeWithTimeout(env *Environment) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return env.Close(ctx)
}

func loopbackPortBinding(port string) func(*container.HostConfig) {
	return func(hostConfig *container.HostConfig) {
		hostConfig.PortBindings = dockernetwork.PortMap{
			dockernetwork.MustParsePort(port): []dockernetwork.PortBinding{
				{
					HostIP: netip.MustParseAddr("127.0.0.1"),
				},
			},
		}
	}
}

func appendContainerLogConsumer(opts []testcontainers.ContainerCustomizer, name string, logf func(container string, stream string, line string)) []testcontainers.ContainerCustomizer {
	if logf == nil {
		return opts
	}
	return append(opts, testcontainers.WithLogConsumers(containerLogConsumer{
		name: name,
		logf: logf,
	}))
}

func synchronizedContainerLogf(logf func(container string, stream string, line string)) func(container string, stream string, line string) {
	if logf == nil {
		return nil
	}
	var mu sync.Mutex
	return func(container string, stream string, line string) {
		mu.Lock()
		defer mu.Unlock()
		logf(container, stream, line)
	}
}

type containerLogConsumer struct {
	name string
	logf func(container string, stream string, line string)
}

func (c containerLogConsumer) Accept(log testcontainers.Log) {
	if c.logf == nil {
		return
	}
	content := strings.TrimRight(string(log.Content), "\r\n")
	if content == "" {
		return
	}
	for _, line := range strings.Split(content, "\n") {
		c.logf(c.name, strings.ToLower(log.LogType), strings.TrimRight(line, "\r"))
	}
}

func inventreeContainerEnv(dbHost string, cacheHost string) map[string]string {
	return map[string]string{
		"INVENTREE_SITE_URL":        "http://localhost:8000",
		"INVENTREE_DEBUG":           "True",
		"INVENTREE_LOG_LEVEL":       "WARNING",
		"INVENTREE_CONSOLE_LOG":     "True",
		"INVENTREE_DB_ENGINE":       "postgresql",
		"INVENTREE_DB_NAME":         defaultDBName,
		"INVENTREE_DB_HOST":         dbHost,
		"INVENTREE_DB_PORT":         "5432",
		"INVENTREE_DB_USER":         defaultDBUser,
		"INVENTREE_DB_PASSWORD":     defaultDBPassword,
		"INVENTREE_CACHE_HOST":      cacheHost,
		"INVENTREE_CACHE_PORT":      "6379",
		"INVENTREE_PLUGINS_ENABLED": "False",
		"INVENTREE_AUTO_UPDATE":     "False",
		"INVENTREE_ADMIN_USER":      defaultAdminUser,
		"INVENTREE_ADMIN_EMAIL":     defaultAdminEmail,
		"INVENTREE_ADMIN_PASSWORD":  defaultAdminPassword,
	}
}

func fetchVersion(ctx context.Context, client *http.Client, baseURL string, username string, password string) (versionResponse, error) {
	var out versionResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/version/", nil)
	if err != nil {
		return out, err
	}
	req.SetBasicAuth(username, password)
	if err := doJSON(client, req, &out); err != nil {
		return out, fmt.Errorf("fetch InvenTree runtime version: %w", err)
	}
	return out, nil
}

func createToken(ctx context.Context, client *http.Client, baseURL string, username string, password string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/user/me/token/?name="+defaultTokenName, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(username, password)
	var out tokenResponse
	if err := doJSON(client, req, &out); err != nil {
		return "", fmt.Errorf("create InvenTree test token: %w", err)
	}
	if out.Token == "" {
		return "", errors.New("create InvenTree test token: empty token")
	}
	return out.Token, nil
}

func proveToken(ctx context.Context, baseURL string, token string, client *http.Client) error {
	invClient, err := inventree.NewClient(inventree.Config{
		BaseURL: baseURL,
		Credential: inventree.Credential{
			Scheme: inventree.AuthSchemeToken,
			Token:  token,
		},
		HTTPClient: client,
	})
	if err != nil {
		return err
	}
	req, err := invClient.NewRequest(ctx, http.MethodGet, "/api/user/me/", nil, nil)
	if err != nil {
		return err
	}
	var out map[string]any
	if err := invClient.DoJSON(req, &out); err != nil {
		return fmt.Errorf("validate InvenTree test token: %w", err)
	}
	return nil
}

func doJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s %s returned %d", req.Method, req.URL.Path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func validateExplicitImageTag(image string) error {
	if strings.Contains(image, "@sha256:") {
		return errors.New("InvenTree image must use a readable version tag, not only a digest")
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash || colon == len(image)-1 {
		return fmt.Errorf("InvenTree image %q must include an explicit version tag", image)
	}
	tag := image[colon+1:]
	if tag == "latest" || tag == "stable" {
		return fmt.Errorf("InvenTree image tag %q is floating; use an explicit version tag", tag)
	}
	return nil
}
