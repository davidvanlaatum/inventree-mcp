package testenv

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestDefaultOptionsPinInvenTreeVersion(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	opts := DefaultOptions()

	a.Equal("inventree/inventree:1.4.0", opts.Image)
	a.Equal("1.4.0", opts.ExpectedVersion)
	a.Equal("511", opts.ExpectedAPIVersion)
	a.NoError(ValidateOptions(opts))
}

func TestValidateOptionsRejectsFloatingOrAmbiguousImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
	}{
		{name: "missing tag", image: "inventree/inventree"},
		{name: "stable", image: "inventree/inventree:stable"},
		{name: "latest", image: "inventree/inventree:latest"},
		{name: "digest", image: "inventree/inventree@sha256:abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			opts := DefaultOptions()
			opts.Image = tt.image
			err := ValidateOptions(opts)

			r.Error(err)
		})
	}
}

func TestValidateOptionsRejectsMissingVersionAPIAndNegativeTimeout(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	opts := DefaultOptions()
	opts.ExpectedVersion = ""
	opts.ExpectedAPIVersion = ""
	opts.StartupTimeout = -1

	err := ValidateOptions(opts)

	r.Error(err)
	r.ErrorContains(err, "expected InvenTree version is required")
	r.ErrorContains(err, "expected InvenTree API version is required")
	r.ErrorContains(err, "startup timeout must not be negative")
}

func TestSkipDockerParsesExplicitExclusion(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.True(SkipDocker(func(string) string { return "1" }))
	a.True(SkipDocker(func(string) string { return "true" }))
	a.True(SkipDocker(func(string) string { return "yes" }))
	a.False(SkipDocker(func(string) string { return "" }))
	a.False(SkipDocker(func(string) string { return "0" }))
	a.False(SkipDocker(nil))
}

func TestIntegrationDockerSkipEnvironmentName(t *testing.T) {
	a := assert.New(t)

	t.Setenv(EnvSkipDocker, "true")

	a.True(SkipDocker(os.Getenv))
}

func TestLoopbackPortBinding(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	hostConfig := &container.HostConfig{}
	loopbackPortBinding(defaultWebPort)(hostConfig)

	bindings := hostConfig.PortBindings[dockernetwork.MustParsePort(defaultWebPort)]
	r.Len(bindings, 1)
	r.Equal(netip.MustParseAddr("127.0.0.1"), bindings[0].HostIP)
	r.Empty(bindings[0].HostPort)
}

func TestContainerLogConsumerForwardsNamedLines(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var got []string
	consumer := containerLogConsumer{
		name: "inventree",
		logf: func(container string, stream string, line string) {
			got = append(got, container+" "+stream+" "+line)
		},
	}

	consumer.Accept(testcontainers.Log{
		LogType: testcontainers.StderrLog,
		Content: []byte("first line\nsecond line\n"),
	})
	consumer.Accept(testcontainers.Log{
		LogType: testcontainers.StdoutLog,
		Content: []byte("\n"),
	})

	r.Equal([]string{
		"inventree stderr first line",
		"inventree stderr second line",
	}, got)
}

func TestSynchronizedContainerLogfSerializesCalls(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var got []string
	logf := synchronizedContainerLogf(func(container string, stream string, line string) {
		got = append(got, container+" "+stream+" "+line)
	})

	r.NotNil(logf)
	logf("postgres", "stdout", "ready")
	logf("redis", "stderr", "warning")

	r.Equal([]string{
		"postgres stdout ready",
		"redis stderr warning",
	}, got)
	r.Nil(synchronizedContainerLogf(nil))
}

func TestHTTPHelpersFetchVersionCreateAndProveToken(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/api/version/":
			assertBasicAuth(t, req)
			return http.StatusOK, `{"version":{"server":"1.4.0","api":511}}`
		case "/api/user/me/token/":
			assertBasicAuth(t, req)
			r.Equal(defaultTokenName, req.URL.Query().Get("name"))
			return http.StatusOK, `{"token":"test-token"}`
		case "/api/user/me/":
			r.Equal("Token test-token", req.Header.Get("Authorization"))
			return http.StatusOK, `{"username":"admin"}`
		default:
			return http.StatusNotFound, `{}`
		}
	})

	version, err := fetchVersion(ctx, client, "http://inventree.test", defaultAdminUser, defaultAdminPassword)
	r.NoError(err)
	r.Equal(DefaultVersion, version.Version.Server)
	r.Equal(511, version.Version.API)

	token, err := createToken(ctx, client, "http://inventree.test", defaultAdminUser, defaultAdminPassword)
	r.NoError(err)
	r.Equal("test-token", token)

	r.NoError(proveToken(ctx, "http://inventree.test", token, client))
}

func TestDoJSONRejectsNonSuccessAndInvalidJSON(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()

	client := fakeHTTPClient(t, func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/status":
			return http.StatusTeapot, "nope"
		case "/invalid":
			return http.StatusOK, "{"
		default:
			return http.StatusNotFound, "{}"
		}
	})

	statusReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://inventree.test/status", nil)
	r.NoError(err)
	r.ErrorContains(doJSON(client, statusReq, &map[string]any{}), "GET /status returned 418")

	invalidReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://inventree.test/invalid", nil)
	r.NoError(err)
	r.ErrorContains(doJSON(client, invalidReq, &map[string]any{}), "decode response")
}

func assertBasicAuth(t *testing.T, req *http.Request) {
	t.Helper()
	r := require.New(t)

	username, password, ok := req.BasicAuth()
	r.True(ok)
	r.Equal(defaultAdminUser, username)
	r.Equal(defaultAdminPassword, password)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func fakeHTTPClient(t *testing.T, handler func(*http.Request) (int, string)) *http.Client {
	t.Helper()

	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := handler(req)
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
}
