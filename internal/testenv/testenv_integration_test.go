//go:build !no_integration_tests
// +build !no_integration_tests

package testenv

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartInvenTreeStack(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	if SkipDocker(os.Getenv) || testing.Short() {
		t.Skipf("Docker-backed InvenTree integration test excluded by %s or -short", EnvSkipDocker)
	}

	opts := DefaultOptions()
	t.Logf("starting InvenTree integration stack with image %s, expected version %s, expected API %s", opts.Image, opts.ExpectedVersion, opts.ExpectedAPIVersion)
	env, err := Start(ctx, opts)
	r.NoError(err)
	r.NotNil(env)
	t.Cleanup(func() {
		r.NoError(closeWithTimeout(env))
	})

	r.Equal(DefaultInvenTreeImage, env.Image)
	r.Equal(DefaultVersion, env.Version)
	r.Equal(DefaultAPIVersion, env.APIVersion)
	r.NotEmpty(env.BaseURL)
	r.NotEmpty(env.Token)
	assertPublishedPortsLoopback(t, env)
}

func assertPublishedPortsLoopback(t *testing.T, env *Environment) {
	t.Helper()
	r := require.New(t)
	ctx := context.Background()

	for _, ctr := range env.containers {
		inspect, err := ctr.Inspect(ctx)
		r.NoError(err)
		for port, bindings := range inspect.NetworkSettings.Ports {
			if len(bindings) == 0 {
				continue
			}
			for _, binding := range bindings {
				r.Equal(netip.MustParseAddr("127.0.0.1"), binding.HostIP, "container %s published %s on a non-loopback address", inspect.Name, port)
			}
		}
	}
}
