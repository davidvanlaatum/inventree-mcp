package scripts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var maintainerScripts = []string{
	"preinstall.sh",
	"postinstall.sh",
	"preremove.sh",
	"postremove.sh",
}

func TestMaintainerScriptsAreValidPOSIXShell(t *testing.T) {
	t.Parallel()

	for _, name := range maintainerScripts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			cmd := exec.Command("sh", "-n", name)
			out, err := cmd.CombinedOutput()
			r.NoError(err, "sh -n %s: %s", name, out)
		})
	}
}

func TestPreinstallCreatesStaticSystemIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	content, err := os.ReadFile("preinstall.sh")
	r.NoError(err)
	text := string(content)

	a.Contains(text, "groupadd --system inventree-mcp")
	a.Contains(text, "addgroup -S inventree-mcp")
	a.Contains(text, "useradd --system")
	a.Contains(text, "adduser -S -D -H")
	a.Contains(text, "getent group inventree-mcp")
	a.Contains(text, "getent passwd inventree-mcp")
}

func TestPostinstallFixesConfigOwnership(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	content, err := os.ReadFile("postinstall.sh")
	r.NoError(err)
	text := string(content)

	a.Contains(text, "chown inventree-mcp:inventree-mcp /etc/inventree-mcp/config.yml")
	a.Contains(text, "chmod 0600 /etc/inventree-mcp/config.yml")
}

func TestPostremoveDeletesIdentityOnlyOnFinalRemoval(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	content, err := os.ReadFile("postremove.sh")
	r.NoError(err)
	text := string(content)

	a.Contains(text, `[ "${1:-}" = "purge" ]`, "deb postrm must only remove the identity on purge")
	a.Contains(text, `[ "${1:-}" = "0" ]`, "rpm postun must only remove the identity on final erase")
	a.Contains(text, "userdel inventree-mcp")
	a.Contains(text, "deluser inventree-mcp")
	a.Contains(text, "groupdel inventree-mcp")
	a.Contains(text, "delgroup inventree-mcp")
}

// writeStub creates an executable shell stub named `name` in `dir` that appends its own
// invocation (name plus arguments) as one line to the file named by the LOGFILE env var, then
// exits with exitCode. Tests use these stubs on a sandboxed PATH to observe, without touching the
// real system, which commands a maintainer script actually invokes and with what arguments.
func writeStub(t *testing.T, dir, name string, exitCode int) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho \"%s $*\" >>\"$LOGFILE\"\nexit %d\n", name, exitCode)
	r := require.New(t)
	r.NoError(os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700))
}

// runMaintainerScript runs the given maintainer script with only the named stub commands on
// PATH, plus the given positional argument, and returns the stub invocation log.
func runMaintainerScript(t *testing.T, script string, arg string, stubs map[string]int) string {
	t.Helper()
	r := require.New(t)

	binDir := t.TempDir()
	for name, exitCode := range stubs {
		writeStub(t, binDir, name, exitCode)
	}

	logFile := filepath.Join(t.TempDir(), "log")
	r.NoError(os.WriteFile(logFile, nil, 0o600))

	args := []string{script}
	if arg != "" {
		args = append(args, arg)
	}
	cmd := exec.Command("sh", args...)
	cmd.Env = []string{"PATH=" + binDir, "LOGFILE=" + logFile}
	out, err := cmd.CombinedOutput()
	r.NoError(err, "%s %s: %s", script, arg, out)

	logged, err := os.ReadFile(logFile)
	r.NoError(err)
	return string(logged)
}

func TestPreinstallIsIdempotent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		getentExit    int
		wantIdentity  bool
		wantNoCreated bool
	}{
		{name: "fresh install creates the identity", getentExit: 2, wantIdentity: true},
		{name: "existing identity is left untouched", getentExit: 0, wantNoCreated: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)

			log := runMaintainerScript(t, "preinstall.sh", "", map[string]int{
				"getent":   tc.getentExit,
				"groupadd": 0,
				"useradd":  0,
			})

			if tc.wantIdentity {
				a.Contains(log, "groupadd --system inventree-mcp")
				a.Contains(log, "useradd --system")
			}
			if tc.wantNoCreated {
				a.NotContains(log, "groupadd")
				a.NotContains(log, "useradd")
			}
		})
	}
}

func TestPreinstallFallsBackToAlpineTools(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	log := runMaintainerScript(t, "preinstall.sh", "", map[string]int{
		"getent":   2,
		"addgroup": 0,
		"adduser":  0,
	})

	a.Contains(log, "addgroup -S inventree-mcp")
	a.Contains(log, "adduser -S -D -H")
}

func TestPostremoveDeletesIdentityOnlyOnFinalRemovalExecution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		pkgManager  string
		arg         string
		wantDeleted bool
	}{
		{name: "dpkg upgrade keeps the identity", pkgManager: "dpkg", arg: "upgrade"},
		{name: "dpkg remove keeps the identity", pkgManager: "dpkg", arg: "remove"},
		{name: "dpkg purge deletes the identity", pkgManager: "dpkg", arg: "purge", wantDeleted: true},
		{name: "rpm upgrade erase keeps the identity", pkgManager: "rpm", arg: "1"},
		{name: "rpm final erase deletes the identity", pkgManager: "rpm", arg: "0", wantDeleted: true},
		{name: "apk removal deletes the identity", pkgManager: "apk", arg: "", wantDeleted: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)

			log := runMaintainerScript(t, "postremove.sh", tc.arg, map[string]int{
				tc.pkgManager: 0,
				"userdel":     0,
				"groupdel":    0,
			})

			if tc.wantDeleted {
				a.Contains(log, "userdel inventree-mcp")
				a.Contains(log, "groupdel inventree-mcp")
			} else {
				a.False(strings.Contains(log, "userdel inventree-mcp") || strings.Contains(log, "groupdel inventree-mcp"),
					"expected the identity not to be deleted, log:\n%s", log)
			}
		})
	}
}

func TestPostremoveFallsBackToAlpineTools(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	log := runMaintainerScript(t, "postremove.sh", "", map[string]int{
		"apk":      0,
		"deluser":  0,
		"delgroup": 0,
	})

	a.Contains(log, "deluser inventree-mcp")
	a.Contains(log, "delgroup inventree-mcp")
}
