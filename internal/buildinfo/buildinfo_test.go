package buildinfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsDevelopment(t *testing.T) {
	originalVersion := Version
	t.Cleanup(func() { Version = originalVersion })

	Version = "dev"
	require.True(t, IsDevelopment())

	Version = "v0.1.0"
	require.False(t, IsDevelopment())
}
