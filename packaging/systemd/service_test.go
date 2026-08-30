package systemd_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackagedServiceUsesNativeNotifyAndWatchdog(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	unit, err := os.ReadFile("inventree-mcp.service")
	r.NoError(err)
	text := string(unit)

	a.Contains(text, "Type=notify\n")
	a.Contains(text, "NotifyAccess=main\n")
	a.Contains(text, "WatchdogSec=30s\n")
	a.Contains(text, "Restart=on-failure\n")
	a.NotContains(text, "Type=simple")
}

func TestPackagedServiceUsesConfigFile(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	unit, err := os.ReadFile("inventree-mcp.service")
	r.NoError(err)
	text := string(unit)

	a.Contains(text, "ExecStart=/usr/bin/inventree-mcp serve --config /etc/inventree-mcp/config.yml\n")
	a.NotContains(text, "EnvironmentFile=")
}

func TestPackagedServiceRoutesApplicationLogsToJournal(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	unit, err := os.ReadFile("inventree-mcp.service")
	r.NoError(err)
	text := string(unit)

	a.Contains(text, "StandardOutput=journal\n")
	a.Contains(text, "StandardError=journal\n")
	a.Contains(text, "SyslogIdentifier=inventree-mcp\n")
}

func TestPackagedServiceRunsAsNonRootIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	unit, err := os.ReadFile("inventree-mcp.service")
	r.NoError(err)
	text := string(unit)

	a.Contains(text, "User=inventree-mcp\n")
	a.Contains(text, "Group=inventree-mcp\n")
	a.NotContains(text, "User=root")
	a.NotContains(text, "DynamicUser=")
}
