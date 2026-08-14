package buildinfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserAgentUsesBuildVersion(t *testing.T) {
	previousVersion := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = previousVersion })

	require.New(t).Equal("inventree-mcp/1.2.3", UserAgent())
}
