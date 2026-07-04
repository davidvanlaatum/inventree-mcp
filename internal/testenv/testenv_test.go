package testenv

import (
	"net/netip"
	"os"
	"testing"

	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
