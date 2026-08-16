package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalStableVersion(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	got, err := canonicalStableVersion("1.2.3")
	a.NoError(err)
	a.Equal("v1.2.3", got)
	for _, value := range []string{"", "dev", "v1", "v1.2.3-rc.1", "v1.2.3+build"} {
		_, err := canonicalStableVersion(value)
		a.ErrorIs(err, ErrVersionPolicy, value)
	}
}

func TestSupportedPlatform(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	a.NoError(supportedPlatform("linux", "amd64"))
	a.NoError(supportedPlatform("darwin", "arm64"))
	a.ErrorIs(supportedPlatform("windows", "amd64"), ErrUnsupportedPlatform)
	a.ErrorIs(supportedPlatform("linux", "386"), ErrUnsupportedPlatform)
}

func TestUpdaterNoOpValidatesDirectInstallationWithoutMutation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	old := []byte("old")
	prior := []byte("prior")
	r.NoError(os.WriteFile(executable, old, 0o755))
	r.NoError(os.WriteFile(executable+".previous", prior, 0o700))
	r.NoError(adoptDirectInstall(executable))
	before := captureInstallationState(t, executable)

	client := &mapHTTPClient{responses: map[string][]byte{
		"https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/latest": releaseJSON("v1.2.3", false, false, runtime.GOOS, runtime.GOARCH),
	}}
	updater := New(Dependencies{
		Client:         client,
		ExecutablePath: func() (string, error) { return executable, nil },
		GOOS:           runtime.GOOS, GOARCH: runtime.GOARCH,
	})

	result, err := updater.Run(t.Context(), "v1.2.3", Options{})
	r.NoError(err)
	a.False(result.Updated)
	a.Equal("v1.2.3", result.Version)
	assertInstallationState(t, executable, before, true)
}

func TestUpdaterRejectsDowngradeBeforeNetwork(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	client := &mapHTTPClient{}
	updater := New(Dependencies{Client: client, GOOS: "linux", GOARCH: "amd64"})
	_, err := updater.Run(t.Context(), "v2.0.0", Options{TargetVersion: "v1.9.9"})
	r.ErrorIs(err, ErrVersionPolicy)
	a.Empty(client.requests)
}

func TestUpdaterInstallsVerifiedReleaseAndPreservesPrevious(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	oldBinary := []byte("binary-version=v1.0.0")
	newBinary := []byte("binary-version=v1.1.0")
	r.NoError(os.WriteFile(executable, oldBinary, 0o755))
	r.NoError(adoptDirectInstall(executable))
	archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: newBinary}})
	client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
	commands := 0
	runner := func(_ context.Context, command Command) (string, error) {
		commands++
		data, err := os.ReadFile(command.Path)
		r.NoError(err)
		probeInfo, err := os.Stat(command.Path)
		r.NoError(err)
		if commands == 1 {
			a.Equal(fs.FileMode(0o700), probeInfo.Mode().Perm())
		}
		a.Equal(dir, command.Dir)
		a.Equal([]string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin"}, command.Env)
		a.NotContains(strings.Join(command.Env, "\x00"), "TOKEN")
		version := "v1.0.0"
		if bytes.Contains(data, []byte("v1.1.0")) {
			version = "v1.1.0"
		}
		return versionOutput(version), nil
	}
	updater := New(Dependencies{
		Client: client, ExecutablePath: func() (string, error) { return executable, nil },
		RunCommand: runner, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})

	result, err := updater.Run(t.Context(), "v1.0.0", Options{GitHubToken: "github-secret"})
	r.NoError(err)
	a.True(result.Updated)
	a.Equal("v1.1.0", result.Version)
	a.Equal(2, commands)
	installed, err := os.ReadFile(executable)
	r.NoError(err)
	a.Equal(newBinary, installed)
	previous, err := os.ReadFile(executable + ".previous")
	r.NoError(err)
	a.Equal(oldBinary, previous)
	info, err := os.Stat(executable)
	r.NoError(err)
	a.Equal(fs.FileMode(0o755), info.Mode().Perm())
	a.FileExists(executable + ".self-update.lock")
	a.NoFileExists(executable + ".self-update.transaction")
	for _, request := range client.requests {
		a.Empty(request.Header.Get("INVENTREE_TOKEN"))
		a.Empty(request.Header.Get("MCP_AUTHORIZATION"))
		if request.URL.Host == "api.github.com" {
			a.Equal("Bearer github-secret", request.Header.Get("Authorization"))
		} else {
			a.Empty(request.Header.Get("Authorization"))
		}
	}
}

func TestUpdaterInstallsRequestedVersionFromTagEndpoint(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old-v1.0.0"), 0o755))
	r.NoError(adoptDirectInstall(executable))
	archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("new-v1.2.0")}})
	client := releaseClient("v1.2.0", archive, runtime.GOOS, runtime.GOARCH)
	latest := "https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/latest"
	tag := "https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/tags/v1.2.0"
	client.responses[tag] = client.responses[latest]
	delete(client.responses, latest)
	updater := New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.2.0"), nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})

	result, err := updater.Run(t.Context(), "v1.0.0", Options{TargetVersion: "v1.2.0"})
	r.NoError(err)
	a.True(result.Updated)
	a.Equal("v1.2.0", result.Version)
	a.Equal(tag, client.requests[0].URL.String())
}

func TestUpdaterRollsBackInstalledVersionFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	oldBinary := []byte("binary-version=v1.0.0")
	newBinary := []byte("binary-version=v1.1.0")
	r.NoError(os.WriteFile(executable, oldBinary, 0o751))
	r.NoError(adoptDirectInstall(executable))
	existingPrevious := []byte("older")
	r.NoError(os.WriteFile(executable+".previous", existingPrevious, 0o700))
	archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: newBinary}})
	client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
	commands := 0
	runner := func(_ context.Context, command Command) (string, error) {
		commands++
		if commands == 2 {
			return "", errors.New("installed probe failed")
		}
		return versionOutput("v1.1.0"), nil
	}
	updater := New(Dependencies{
		Client: client, ExecutablePath: func() (string, error) { return executable, nil },
		RunCommand: runner, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})

	_, err := updater.Run(t.Context(), "v1.0.0", Options{})
	r.ErrorContains(err, "installed probe failed")
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal(oldBinary, installed)
	previous, readErr := os.ReadFile(executable + ".previous")
	r.NoError(readErr)
	a.Equal(existingPrevious, previous)
	info, statErr := os.Stat(executable)
	r.NoError(statErr)
	a.Equal(fs.FileMode(0o751), info.Mode().Perm())
	a.NoFileExists(executable + ".self-update.transaction")
}

func TestValidateInstallTargetRejectsPackageAndLinkBoundaries(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	a.ErrorIs(validateInstallTarget("/usr/bin/inventree-mcp", "linux"), ErrPackageManaged)
	a.ErrorIs(validateInstallTarget("/opt/homebrew/bin/inventree-mcp", "darwin"), ErrPackageManaged)

	dir := t.TempDir()
	target := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(target, []byte("old"), 0o755))
	symlink := filepath.Join(dir, "linked")
	r.NoError(os.Symlink(target, symlink))
	a.ErrorIs(validateInstallTarget(symlink, runtime.GOOS), ErrUnsafeTarget)
	hardlink := filepath.Join(dir, "hardlink")
	r.NoError(os.Link(target, hardlink))
	a.ErrorIs(validateInstallTarget(target, runtime.GOOS), ErrUnsafeTarget)
}

func TestRecoverInterruptedUpdateRestoresBackup(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	dir := t.TempDir()
	executable := filepath.Join(dir, binaryName)
	backup := filepath.Join(dir, ".inventree-mcp-rollback-test")
	candidate := filepath.Join(dir, ".inventree-mcp-update-test")
	r.NoError(os.WriteFile(executable, []byte("new"), 0o755))
	r.NoError(os.WriteFile(backup, []byte("old"), 0o600))
	r.NoError(os.WriteFile(candidate, []byte("staged"), 0o700))
	previousCandidate := filepath.Join(dir, ".inventree-mcp-previous-candidate-test")
	r.NoError(os.WriteFile(previousCandidate, []byte("old"), 0o751))
	r.NoError(writeTransaction(executable, transaction{BackupPath: backup, CandidatePath: candidate, PreviousCandidate: previousCandidate, Mode: 0o751, TargetVersion: "v1.1.0"}))

	recovered, err := recoverInterruptedUpdate(executable)
	r.NoError(err)
	a.True(recovered)
	data, err := os.ReadFile(executable)
	r.NoError(err)
	a.Equal([]byte("old"), data)
	info, err := os.Stat(executable)
	r.NoError(err)
	a.Equal(fs.FileMode(0o751), info.Mode().Perm())
	a.NoFileExists(candidate)
	a.NoFileExists(executable + ".self-update.transaction")
}

func TestRecoverInterruptedRollbackIsRestartable(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	backup := filepath.Join(dir, ".inventree-mcp-rollback-source")
	previousBackup := filepath.Join(dir, ".inventree-mcp-previous-source")
	previousCandidate := filepath.Join(dir, ".inventree-mcp-previous-candidate")
	r.NoError(os.WriteFile(executable, []byte("old"), 0o751))
	r.NoError(os.WriteFile(backup, []byte("old"), 0o600))
	r.NoError(os.WriteFile(executable+".previous", []byte("new-previous"), 0o751))
	r.NoError(os.WriteFile(previousBackup, []byte("older-previous"), 0o700))
	r.NoError(os.WriteFile(previousCandidate, []byte("old"), 0o751))
	txn := transaction{BackupPath: backup, CandidatePath: filepath.Join(dir, ".missing-candidate"), PreviousCandidate: previousCandidate, PreviousBackupPath: previousBackup, PreviousMode: 0o700, Mode: 0o751, TargetVersion: "v1.1.0"}
	r.NoError(writeTransaction(executable, txn))

	recovered, err := recoverInterruptedUpdate(executable)
	r.NoError(err)
	a.True(recovered)
	installed, err := os.ReadFile(executable)
	r.NoError(err)
	a.Equal([]byte("old"), installed)
	previous, err := os.ReadFile(executable + ".previous")
	r.NoError(err)
	a.Equal([]byte("older-previous"), previous)
	a.NoFileExists(executable + ".self-update.transaction")
	recovered, err = recoverInterruptedUpdate(executable)
	r.NoError(err)
	a.False(recovered)
}

func TestUpdaterRecoversInterruptedTransactionBeforeNoOp(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	backup := filepath.Join(dir, ".inventree-mcp-rollback-source")
	previousCandidate := filepath.Join(dir, ".inventree-mcp-previous-candidate")
	r.NoError(os.WriteFile(executable, []byte("new"), 0o755))
	r.NoError(adoptDirectInstall(executable))
	r.NoError(os.WriteFile(backup, []byte("old"), 0o600))
	r.NoError(os.WriteFile(previousCandidate, []byte("old"), 0o755))
	r.NoError(writeTransaction(executable, transaction{BackupPath: backup, CandidatePath: filepath.Join(dir, ".missing-candidate"), PreviousCandidate: previousCandidate, Mode: 0o755, TargetVersion: "v1.1.0"}))
	client := releaseClient("v1.1.0", []byte("unused"), runtime.GOOS, runtime.GOARCH)
	updater := New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})

	_, err := updater.Run(t.Context(), "v1.1.0", Options{})
	a.ErrorContains(err, "rerun self-update")
	a.Empty(client.requests)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("old"), installed)
	a.NoFileExists(executable + ".self-update.transaction")
}

func TestAcquireLockRejectsActiveAndRecoversAfterRelease(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	dir := t.TempDir()
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o755))
	lock, err := acquireLock(executable)
	r.NoError(err)
	_, err = acquireLock(executable)
	a.ErrorIs(err, ErrUpdateLocked)
	lock.Release()

	lockPath := executable + ".self-update.lock"
	recovered, err := acquireLock(executable)
	r.NoError(err)
	recovered.Release()
	a.FileExists(lockPath)
	info, err := os.Stat(lockPath)
	r.NoError(err)
	a.Equal(fs.FileMode(0o600), info.Mode().Perm())
}

func TestDirectInstallAdoptionAndMarkerValidation(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := t.TempDir()
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("binary"), 0o755))

	a.Error(validateDirectInstallMarker(executable))
	r.NoError(adoptDirectInstall(executable))
	a.NoError(validateDirectInstallMarker(executable))
	marker := directInstallMarkerPath(executable)
	info, err := os.Stat(marker)
	r.NoError(err)
	a.Equal(fs.FileMode(0o600), info.Mode().Perm())
	r.NoError(os.WriteFile(marker, []byte("wrong\n"), 0o600))
	a.Error(validateDirectInstallMarker(executable))
}

func TestUpdaterAdoptsDirectInstallWithoutNetwork(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("released-binary"), 0o755))
	client := &mapHTTPClient{}
	updater := New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})

	result, err := updater.Run(t.Context(), "v1.2.3", Options{AdoptDirectInstall: true})
	r.NoError(err)
	a.True(result.Adopted)
	a.Empty(client.requests)
	a.NoError(validateDirectInstallMarker(executable))
	_, err = updater.Run(t.Context(), "v1.2.3", Options{AdoptDirectInstall: true, TargetVersion: "v1.2.4"})
	a.Error(err)
}

func TestExtractExecutableRejectsUnsafeArchiveShapes(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	valid := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("binary")}})
	got, mode, err := extractExecutable(valid, 100)
	a.NoError(err)
	a.Equal([]byte("binary"), got)
	a.Equal(fs.FileMode(0o755), mode)

	cases := map[string][]archiveEntry{
		"unexpected": {{name: "README.md", mode: 0o644, body: []byte("readme")}, {name: binaryName, mode: 0o755, body: []byte("binary")}},
		"duplicate":  {{name: binaryName, mode: 0o755, body: []byte("one")}, {name: binaryName, mode: 0o755, body: []byte("two")}},
		"traversal":  {{name: "../inventree-mcp", mode: 0o755, body: []byte("binary")}},
		"symlink":    {{name: binaryName, mode: 0o755, typeFlag: tar.TypeSymlink, linkName: "/tmp/other"}},
		"hardlink":   {{name: binaryName, mode: 0o755, typeFlag: tar.TypeLink, linkName: "/tmp/other"}},
		"device":     {{name: binaryName, mode: 0o755, typeFlag: tar.TypeChar}},
		"directory":  {{name: binaryName, mode: 0o755, typeFlag: tar.TypeDir}},
		"nonexec":    {{name: binaryName, mode: 0o644, body: []byte("binary")}},
	}
	for name, entries := range cases {
		name, entries := name, entries
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := extractExecutable(makeArchive(t, entries), 100)
			assert.Error(t, err)
		})
	}
	_, _, err = extractExecutable(append(valid, []byte("trailing")...), 100)
	a.Error(err)
	_, _, err = extractExecutable(append(append([]byte(nil), valid...), valid...), 100)
	a.Error(err)
	_, _, err = extractExecutable(valid[:len(valid)-4], 100)
	a.Error(err)
	_, _, err = extractExecutable(valid, 2)
	a.Error(err)
}

func TestVerifyChecksum(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	archive := []byte("archive")
	hash := sha256.Sum256(archive)
	line := hex.EncodeToString(hash[:]) + "  file.tar.gz\n"
	a.NoError(verifyChecksum("file.tar.gz", archive, []byte(line)))
	a.Error(verifyChecksum("missing.tar.gz", archive, []byte(line)))
	a.Error(verifyChecksum("file.tar.gz", []byte("changed"), []byte(line)))
	a.Error(verifyChecksum("file.tar.gz", archive, []byte(line+line)))
}

func TestHTTPRedirectPolicyStripsCredentialsAndRejectsOrigins(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	client := newHTTPClient(time.Second, nil)
	via, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/example", nil)
	r.NoError(err)
	via.Header.Set("Authorization", "Bearer secret")
	redirect, err := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/object", nil)
	r.NoError(err)
	redirect.Header.Set("Authorization", "Bearer secret")
	a.NoError(client.CheckRedirect(redirect, []*http.Request{via}))
	a.Empty(redirect.Header.Get("Authorization"))
	bad, err := http.NewRequest(http.MethodGet, "https://example.com/object", nil)
	r.NoError(err)
	a.Error(client.CheckRedirect(bad, []*http.Request{via}))
}

func TestHTTPGetPreservesVersionedUserAgentAcrossAllowedRedirect(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	userAgents := make(chan string, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		userAgents <- req.Header.Get("User-Agent")
		if req.URL.Path == "/start" {
			http.Redirect(w, req, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("redirected"))
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	r.NoError(err)
	client := server.Client()
	client.CheckRedirect = newHTTPClient(time.Second, []string{serverURL.Host}).CheckRedirect

	updater := New(Dependencies{Client: client})
	data, err := updater.get(t.Context(), server.URL+"/start", "", 16)
	r.NoError(err)
	r.Equal([]byte("redirected"), data)
	r.Equal(buildinfo.UserAgent(), <-userAgents)
	r.Equal(buildinfo.UserAgent(), <-userAgents)
}

func TestResolveReleaseRequiresExactRequestedAssetURLs(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	name := releaseArchiveName("linux", "amd64")
	endpoint := "https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/tags/v1.2.3"
	valid := string(releaseJSON("v1.2.3", false, false, "linux", "amd64"))
	tests := map[string]string{
		"cross release":  strings.Replace(valid, "/download/v1.2.3/", "/download/v9.9.9/", 1),
		"wrong basename": strings.Replace(valid, "/"+name+`"`, "/other.tar.gz\"", 1),
		"query":          strings.Replace(valid, "/"+name+`"`, "/"+name+`?download=1"`, 1),
		"trailing JSON":  valid + `{}`,
	}
	for testName, body := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			client := &mapHTTPClient{responses: map[string][]byte{endpoint: []byte(body)}}
			updater := New(Dependencies{Client: client, GOOS: "linux", GOARCH: "amd64"})
			_, err := updater.resolveRelease(t.Context(), "v1.2.3", "")
			a.Error(err)
		})
	}
	client := &mapHTTPClient{responses: map[string][]byte{endpoint: releaseJSON("v1.2.3", false, false, "linux", "amd64")}}
	updater := New(Dependencies{Client: client, GOOS: "linux", GOARCH: "amd64"})
	release, err := updater.resolveRelease(t.Context(), "v1.2.3", "")
	r.NoError(err)
	a.Equal("v1.2.3", release.Version)
}

func TestResolveReleaseRejectsAmbiguousOrUnstableMetadata(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	endpoint := "https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/latest"
	base := string(releaseJSON("v1.2.3", false, false, "linux", "amd64"))
	name := releaseArchiveName("linux", "amd64")
	tests := map[string]string{
		"draft":        string(releaseJSON("v1.2.3", true, false, "linux", "amd64")),
		"prerelease":   string(releaseJSON("v1.2.3", false, true, "linux", "amd64")),
		"missing":      strings.Replace(base, fmt.Sprintf(`,{"name":"checksums.txt","browser_download_url":%q}`, "https://github.com/davidvanlaatum/inventree-mcp/releases/download/v1.2.3/checksums.txt"), "", 1),
		"duplicate":    strings.Replace(base, `"assets":[`, `"assets":[{"name":"`+name+`","browser_download_url":"https://github.com/davidvanlaatum/inventree-mcp/releases/download/v1.2.3/`+name+`"},`, 1),
		"tag mismatch": strings.Replace(base, `"tag_name":"v1.2.3"`, `"tag_name":"v1.2.3-rc.1"`, 1),
	}
	for testName, body := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			updater := New(Dependencies{Client: &mapHTTPClient{responses: map[string][]byte{endpoint: []byte(body)}}, GOOS: "linux", GOARCH: "amd64"})
			_, err := updater.resolveRelease(t.Context(), "", "")
			a.Error(err)
		})
	}
}

func TestHTTPGetBoundsErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	a := assert.New(t)
	tests := []struct {
		name   string
		client HTTPDoer
		ctx    func(*testing.T) context.Context
	}{
		{name: "oversized", client: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("12345"))}, nil
		}), ctx: func(t *testing.T) context.Context { return t.Context() }},
		{name: "truncated", client: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&failingReader{})}, nil
		}), ctx: func(t *testing.T) context.Context { return t.Context() }},
		{name: "canceled", client: doerFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}), ctx: func(t *testing.T) context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			updater := New(Dependencies{Client: test.client})
			_, err := updater.get(test.ctx(t), "https://api.github.com/test", "secret", 4)
			a.Error(err)
		})
	}
}

func TestHTTPGetUsesVersionedUserAgent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	updater := New(Dependencies{Client: doerFunc(func(request *http.Request) (*http.Response, error) {
		r.Equal(buildinfo.UserAgent(), request.Header.Get("User-Agent"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})})

	data, err := updater.get(t.Context(), "https://api.github.com/test", "", 8)
	r.NoError(err)
	r.Equal([]byte("ok"), data)
}

func TestRunCommandUsesIsolatedBoundedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-only")
	}
	r := require.New(t)
	a := assert.New(t)
	ordinaryCommandTimeout := 5 * time.Second
	sensitive := map[string]string{
		"INVENTREE_TOKEN":   "inventree-secret",
		"MCP_AUTHORIZATION": "mcp-secret",
		"HTTP_PROXY":        "http://proxy.invalid",
		"HTTPS_PROXY":       "http://proxy.invalid",
		"ALL_PROXY":         "socks5://proxy.invalid",
		"COOKIE":            "cookie-secret",
		"NETRC":             "/secret/netrc",
	}
	for name, value := range sensitive {
		t.Setenv(name, value)
	}
	dir := resolvedTempDir(t)
	script := filepath.Join(dir, "probe.sh")
	r.NoError(os.WriteFile(script, []byte("#!/bin/sh\nif IFS= read -r line; then exit 8; fi\npwd\nenv\n"), 0o700))
	output, err := runCommand(t.Context(), Command{Path: script, Dir: dir, Env: []string{"LANG=C", "PATH=/usr/bin:/bin"}, Timeout: ordinaryCommandTimeout, OutputLimit: 4096})
	r.NoError(err)
	a.Contains(output, dir)
	a.Contains(output, "LANG=C")
	for name, value := range sensitive {
		a.NotContains(output, name)
		a.NotContains(output, value)
	}

	noisy := filepath.Join(dir, "noisy.sh")
	r.NoError(os.WriteFile(noisy, []byte("#!/bin/sh\nwhile :; do echo 1234567890; done\n"), 0o700))
	_, err = runCommand(t.Context(), Command{Path: noisy, Dir: dir, Env: []string{"PATH=/usr/bin:/bin"}, Timeout: ordinaryCommandTimeout, OutputLimit: 64})
	a.ErrorContains(err, "output exceeds")

	slow := filepath.Join(dir, "slow.sh")
	r.NoError(os.WriteFile(slow, []byte("#!/bin/sh\n(sleep 30) &\nwait\n"), 0o700))
	started := time.Now()
	_, err = runCommand(t.Context(), Command{Path: slow, Dir: dir, Env: []string{"PATH=/usr/bin:/bin"}, Timeout: 50 * time.Millisecond, OutputLimit: 64})
	a.ErrorContains(err, "timed out")
	a.Less(time.Since(started), 3*time.Second)
}

func TestUpdaterRejectsStagedProbeFailuresWithoutMutation(t *testing.T) {
	t.Parallel()
	tests := map[string]CommandRunner{
		"mismatch":  func(context.Context, Command) (string, error) { return versionOutput("v9.9.9"), nil },
		"malformed": func(context.Context, Command) (string, error) { return "version: v1.1.0\n", nil },
		"nonzero":   func(context.Context, Command) (string, error) { return "", errors.New("exit status 2") },
	}
	for testName, runner := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			dir := resolvedTempDir(t)
			executable := filepath.Join(dir, binaryName)
			old := []byte("old-v1.0.0")
			r.NoError(os.WriteFile(executable, old, 0o755))
			r.NoError(adoptDirectInstall(executable))
			archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("new-v1.1.0")}})
			updater := New(Dependencies{Client: releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH), ExecutablePath: func() (string, error) { return executable, nil }, RunCommand: runner, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})
			_, err := updater.Run(t.Context(), "v1.0.0", Options{})
			a.Error(err)
			got, readErr := os.ReadFile(executable)
			r.NoError(readErr)
			a.Equal(old, got)
			a.NoFileExists(executable + ".self-update.transaction")
		})
	}
}

func TestRequireVersionAcceptsWithAndWithoutOptionalInvenTreeFields(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		output string
		want   buildinfo.PorcelainInfo
	}{
		"with optional fields": {
			output: "porcelain: 1\nversion: v1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\ninventree_version: 1.5.0\ninventree_api: 530\n",
			want:   buildinfo.PorcelainInfo{Version: "v1.2.3", Commit: "abc", Date: "2026-08-16T10:00:00Z", InvenTreeVersion: "1.5.0", InvenTreeAPI: "530"},
		},
		"without optional fields": {
			output: "porcelain: 1\nversion: v1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
			want:   buildinfo.PorcelainInfo{Version: "v1.2.3", Commit: "abc", Date: "2026-08-16T10:00:00Z"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			runner := func(context.Context, Command) (string, error) { return test.output, nil }
			info, err := requireVersion(t.Context(), runner, "/bin/example", "/bin", "v1.2.3", time.Second, 1024)
			r.NoError(err)
			a.Equal(test.want, info)
		})
	}
}

func TestRequireVersionRejectsMalformedOrUnsupportedPorcelain(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing porcelain marker":           "version: v1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
		"unsupported porcelain version":      "porcelain: 2\nversion: v1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
		"line without colon-space separator": "porcelain: 1\nversion:v1.2.3\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
		"version mismatch":                   "porcelain: 1\nversion: v9.9.9\ncommit: abc\ndate: 2026-08-16T10:00:00Z\n",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := assert.New(t)
			runner := func(context.Context, Command) (string, error) { return output, nil }
			_, err := requireVersion(t.Context(), runner, "/bin/example", "/bin", "v1.2.3", time.Second, 1024)
			a.Error(err, name)
		})
	}
}

func TestUpdaterPopulatesInvenTreeBaselineFromPreRenameCandidateWithoutSecondExec(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o755))
	r.NoError(adoptDirectInstall(executable))
	archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("new")}})
	client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
	calls := 0
	runner := func(context.Context, Command) (string, error) {
		calls++
		if calls == 1 {
			return "porcelain: 1\nversion: v1.1.0\ncommit: test\ndate: 2026-08-16T10:00:00Z\ninventree_version: 1.6.0\ninventree_api: 540\n", nil
		}
		return versionOutput("v1.1.0"), nil
	}
	updater := New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, RunCommand: runner, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})

	result, err := updater.Run(t.Context(), "v1.0.0", Options{})
	r.NoError(err)
	a.True(result.Updated)
	a.Equal(buildinfo.PinnedInvenTreeVersion, result.PreviousInvenTreeVersion)
	a.Equal(buildinfo.PinnedInvenTreeAPIVersion, result.PreviousInvenTreeAPI)
	a.Equal("1.6.0", result.NewInvenTreeVersion)
	a.Equal("540", result.NewInvenTreeAPI)
	a.Equal(2, calls, "only the pre-rename staged probe and the post-rename installed probe should run; the summary must reuse the pre-rename parse rather than executing the candidate a third time")
}

func TestUpdaterOmitsInvenTreeBaselineWhenCandidateLacksOptionalFields(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)

	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o755))
	r.NoError(adoptDirectInstall(executable))
	archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("new")}})
	client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
	runner := func(context.Context, Command) (string, error) {
		return "porcelain: 1\nversion: v1.1.0\ncommit: test\ndate: 2026-08-16T10:00:00Z\n", nil
	}
	updater := New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, RunCommand: runner, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})

	result, err := updater.Run(t.Context(), "v1.0.0", Options{})
	r.NoError(err)
	a.True(result.Updated)
	a.Empty(result.NewInvenTreeVersion)
	a.Empty(result.NewInvenTreeAPI)
}

// TestOldFixedThreeLineParserRejectsNewPorcelainFormat documents, rather than silently
// drops, F-S72's accepted one-time self-update break: every binary released before this
// story shipped has a compiled-in requireVersion that expects the exact old
// "version:\ncommit:\ndate:" 3-line shape and rejects the new porcelain output as
// malformed, refusing to self-update into it. Since the production old parser was
// replaced rather than kept, this test re-implements a minimal copy of it inline solely
// to prove the migration break is real and intentional, not accidental.
//
// TODO: revisit or remove this test at the v1.0.0 boundary (AGENTS.md's Release Workflow
// section already treats v1.0.0 as a compatibility milestone), once no supported binary
// predates the porcelain format.
func TestOldFixedThreeLineParserRejectsNewPorcelainFormat(t *testing.T) {
	t.Parallel()
	a := assert.New(t)

	oldParserRequiresVersion := func(output string) error {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) != 3 || !strings.HasPrefix(lines[0], "version: ") || !strings.HasPrefix(lines[1], "commit: ") || !strings.HasPrefix(lines[2], "date: ") {
			return fmt.Errorf("malformed version output")
		}
		return nil
	}

	newOutput := strings.Join(buildinfo.PorcelainLines(), "\n") + "\n"
	a.ErrorContains(oldParserRequiresVersion(newOutput), "malformed version output")
}

func TestUpdaterVerificationFailuresLeaveInstallationUnchanged(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*testing.T, []byte) *mapHTTPClient{
		"missing download": func(t *testing.T, archive []byte) *mapHTTPClient {
			client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
			delete(client.responses, "https://github.com/davidvanlaatum/inventree-mcp/releases/download/v1.1.0/checksums.txt")
			return client
		},
		"checksum mismatch": func(t *testing.T, archive []byte) *mapHTTPClient {
			client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
			name := releaseArchiveName(runtime.GOOS, runtime.GOARCH)
			client.responses["https://github.com/davidvanlaatum/inventree-mcp/releases/download/v1.1.0/checksums.txt"] = []byte(strings.Repeat("0", 64) + "  " + name + "\n")
			return client
		},
		"archive rejection": func(t *testing.T, _ []byte) *mapHTTPClient {
			bad := makeArchive(t, []archiveEntry{{name: "README.md", mode: 0o644, body: []byte("bad")}})
			return releaseClient("v1.1.0", bad, runtime.GOOS, runtime.GOARCH)
		},
	}
	for testName, clientFactory := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			dir := resolvedTempDir(t)
			executable := filepath.Join(dir, binaryName)
			old := []byte("old")
			prior := []byte("prior")
			r.NoError(os.WriteFile(executable, old, 0o755))
			r.NoError(os.WriteFile(executable+".previous", prior, 0o700))
			r.NoError(adoptDirectInstall(executable))
			valid := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("new")}})
			updater := New(Dependencies{Client: clientFactory(t, valid), ExecutablePath: func() (string, error) { return executable, nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, RunCommand: func(context.Context, Command) (string, error) {
				a.Fail("verification failure executed candidate")
				return "", errors.New("unexpected")
			}})
			_, err := updater.Run(t.Context(), "v1.0.0", Options{})
			a.Error(err)
			got, readErr := os.ReadFile(executable)
			r.NoError(readErr)
			a.Equal(old, got)
			gotPrior, readErr := os.ReadFile(executable + ".previous")
			r.NoError(readErr)
			a.Equal(prior, gotPrior)
			a.NoFileExists(executable + ".self-update.transaction")
			matches, globErr := filepath.Glob(filepath.Join(dir, ".inventree-mcp-update-*"))
			r.NoError(globErr)
			a.Empty(matches)
		})
	}
}

func TestUpdaterRequiresDirectInstallMarkerAndRejectsTargetSubstitution(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	old := []byte("old")
	r.NoError(os.WriteFile(executable, old, 0o755))
	archive := makeArchive(t, []archiveEntry{{name: binaryName, mode: 0o755, body: []byte("new")}})
	client := releaseClient("v1.1.0", archive, runtime.GOOS, runtime.GOARCH)
	updater := New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})
	_, err := updater.Run(t.Context(), "v1.0.0", Options{})
	a.ErrorContains(err, "adopt-direct-install")
	got, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal(old, got)

	r.NoError(adoptDirectInstall(executable))
	runner := func(_ context.Context, command Command) (string, error) {
		if strings.Contains(command.Path, "-update-") {
			replacement := filepath.Join(dir, "attacker-replacement")
			r.NoError(os.WriteFile(replacement, []byte("changed"), 0o755))
			r.NoError(os.Rename(replacement, executable))
		}
		return versionOutput("v1.1.0"), nil
	}
	updater = New(Dependencies{Client: client, ExecutablePath: func() (string, error) { return executable, nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, RunCommand: runner})
	_, err = updater.Run(t.Context(), "v1.0.0", Options{})
	a.ErrorIs(err, ErrUnsafeTarget)
	got, readErr = os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("changed"), got)
	a.NoFileExists(executable + ".self-update.transaction")
}

func TestValidateInstallTargetRejectsUnsafePermissionsAndAncestors(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o755))
	r.NoError(os.Chmod(executable, fs.ModeSetuid|0o755))
	a.ErrorIs(validateInstallTarget(executable, runtime.GOOS), ErrUnsafeTarget)
	r.NoError(os.Chmod(executable, 0o755))
	r.NoError(os.Chmod(dir, 0o777))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	a.ErrorIs(validateInstallTarget(executable, runtime.GOOS), ErrUnsafeTarget)
}

func TestUpdaterRefusalsPreserveExactInstallationState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		configure func(*testing.T, string, *Dependencies) context.Context
		wantLock  bool
	}{
		{name: "unsupported platform", configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.GOOS = "windows"
			return t.Context()
		}},
		{name: "package managed", configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.ValidateInstallTarget = func(string, string) error { return fmt.Errorf("alternate package prefix: %w", ErrPackageManaged) }
			return t.Context()
		}},
		{name: "ownership unavailable", configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.ValidateInstallTarget = func(string, string) error { return fmt.Errorf("ownership metadata unavailable: %w", ErrUnsafeTarget) }
			return t.Context()
		}},
		{name: "missing adoption marker", configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			r := require.New(t)
			r.NoError(os.Remove(directInstallMarkerPath(executable)))
			return t.Context()
		}},
		{name: "malformed metadata", wantLock: true, configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.Client = &mapHTTPClient{responses: map[string][]byte{"https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/latest": []byte(`{"tag_name":`)}}
			return t.Context()
		}},
		{name: "oversized metadata", wantLock: true, configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.Limits.MetadataBytes = 4
			deps.Client = doerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("12345"))}, nil
			})
			return t.Context()
		}},
		{name: "truncated metadata", wantLock: true, configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.Client = doerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&failingReader{})}, nil
			})
			return t.Context()
		}},
		{name: "canceled metadata", wantLock: true, configure: func(t *testing.T, executable string, deps *Dependencies) context.Context {
			deps.Client = doerFunc(func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()
				return nil, request.Context().Err()
			})
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			dir := resolvedTempDir(t)
			executable := filepath.Join(dir, binaryName)
			r.NoError(os.WriteFile(executable, []byte("old"), 0o751))
			r.NoError(os.WriteFile(executable+".previous", []byte("prior"), 0o700))
			r.NoError(adoptDirectInstall(executable))
			deps := Dependencies{Client: &mapHTTPClient{}, ExecutablePath: func() (string, error) { return executable, nil }, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
			ctx := test.configure(t, executable, &deps)
			before := captureInstallationState(t, executable)
			_, err := New(deps).Run(ctx, "v1.0.0", Options{})
			a.Error(err)
			assertInstallationState(t, executable, before, test.wantLock)
		})
	}
}

func TestInstallUsesOwnerOnlyExclusiveTemporaryArtifacts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o755))
	r.NoError(os.WriteFile(executable+".previous", []byte("prior"), 0o755))
	collisions := []string{
		filepath.Join(dir, ".inventree-mcp-update-collision"),
		filepath.Join(dir, ".inventree-mcp-rollback-collision"),
		filepath.Join(dir, ".inventree-mcp-previous-backup-collision"),
	}
	for _, collision := range collisions {
		r.NoError(os.WriteFile(collision, []byte("collision"), 0o600))
	}
	checked := false
	_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.1.0"), nil }, Timeout: time.Second, OutputLimit: 1024, SyncDirectory: func(path string) error {
		if !checked {
			checked = true
			for _, pattern := range []string{".inventree-mcp-rollback-*", ".inventree-mcp-previous-backup-*", ".inventree-mcp-previous-candidate-*"} {
				matches, globErr := filepath.Glob(filepath.Join(dir, pattern))
				r.NoError(globErr)
				for _, match := range matches {
					if strings.HasSuffix(match, "-collision") {
						continue
					}
					info, statErr := os.Stat(match)
					r.NoError(statErr)
					a.Equal(fs.FileMode(0o600), info.Mode().Perm(), match)
				}
			}
			transactionInfo, statErr := os.Stat(executable + ".self-update.transaction")
			r.NoError(statErr)
			a.Equal(fs.FileMode(0o600), transactionInfo.Mode().Perm())
		}
		return syncDirectory(path)
	}})
	r.NoError(err)
	a.True(checked)
	for _, collision := range collisions {
		data, readErr := os.ReadFile(collision)
		r.NoError(readErr)
		a.Equal([]byte("collision"), data)
	}
}

func TestInstallRetainsCommittedRecoveryRecordOnCleanupSyncFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	old := []byte("old")
	r.NoError(os.WriteFile(executable, old, 0o751))
	priorPrevious := []byte("older")
	r.NoError(os.WriteFile(executable+".previous", priorPrevious, 0o700))
	calls := 0
	result, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.1.0"), nil }, Timeout: time.Second, OutputLimit: 1024, SyncDirectory: func(path string) error {
		calls++
		if calls == 3 {
			return errors.New("injected commit sync failure")
		}
		return syncDirectory(path)
	}})
	r.NoError(err)
	a.Equal(executable+".previous", result.PreviousPath)
	a.Equal("v1.1.0", result.Candidate.Version)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("new"), installed)
	previous, readErr := os.ReadFile(executable + ".previous")
	r.NoError(readErr)
	a.Equal(old, previous)
	a.FileExists(executable + ".self-update.transaction")
	recovered, recoverErr := recoverInterruptedUpdate(executable)
	r.NoError(recoverErr)
	a.False(recovered)
	a.NoFileExists(executable + ".self-update.transaction")
}

func TestInstallRepublishesCommittedRecordOnFinalCleanupSyncFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	old := []byte("old")
	r.NoError(os.WriteFile(executable, old, 0o751))
	calls := 0
	_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.1.0"), nil }, Timeout: time.Second, OutputLimit: 1024, SyncDirectory: func(path string) error {
		calls++
		if calls == 4 {
			return errors.New("injected final cleanup sync failure")
		}
		return syncDirectory(path)
	}})
	r.NoError(err)
	transactionPath := executable + ".self-update.transaction"
	data, readErr := os.ReadFile(transactionPath)
	r.NoError(readErr)
	var txn transaction
	r.NoError(json.Unmarshal(data, &txn))
	a.True(txn.Committed)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("new"), installed)
	previous, readErr := os.ReadFile(executable + ".previous")
	r.NoError(readErr)
	a.Equal(old, previous)
	recovered, recoverErr := recoverInterruptedUpdate(executable)
	r.NoError(recoverErr)
	a.False(recovered)
	a.NoFileExists(transactionPath)
}

func TestAmbiguousCommittedRecordPublicationNeverStartsRollback(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	old := []byte("old")
	r.NoError(os.WriteFile(executable, old, 0o751))
	_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.1.0"), nil }, Timeout: time.Second, OutputLimit: 1024, ReplaceTransaction: func(path string, txn transaction) error {
		r.NoError(replaceTransaction(path, txn))
		return &transactionPublishError{err: errors.New("injected ambiguous directory sync failure")}
	}})
	r.NoError(err)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("new"), installed)
	previous, readErr := os.ReadFile(executable + ".previous")
	r.NoError(readErr)
	a.Equal(old, previous)
	transactionPath := executable + ".self-update.transaction"
	data, readErr := os.ReadFile(transactionPath)
	r.NoError(readErr)
	var txn transaction
	r.NoError(json.Unmarshal(data, &txn))
	a.True(txn.Committed)
	recovered, recoverErr := recoverInterruptedUpdate(executable)
	r.NoError(recoverErr)
	a.False(recovered)
	installed, readErr = os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("new"), installed)
}

func TestInstallRetainsCommittedRecoveryRecordOnTransactionRemovalFailure(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	old := []byte("old")
	prior := []byte("older")
	r.NoError(os.WriteFile(executable, old, 0o751))
	r.NoError(os.WriteFile(executable+".previous", prior, 0o700))
	_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.1.0"), nil }, Timeout: time.Second, OutputLimit: 1024, RemoveTransaction: func(string) error { return errors.New("injected transaction removal failure") }})
	r.NoError(err)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("new"), installed)
	previous, readErr := os.ReadFile(executable + ".previous")
	r.NoError(readErr)
	a.Equal(old, previous)
	a.FileExists(executable + ".self-update.transaction")
	recovered, recoverErr := recoverInterruptedUpdate(executable)
	r.NoError(recoverErr)
	a.False(recovered)
	a.NoFileExists(executable + ".self-update.transaction")
}

func TestInstallRollsBackInstalledProbeFailureMatrix(t *testing.T) {
	t.Parallel()
	tests := map[string]func() (string, error){
		"mismatch":  func() (string, error) { return versionOutput("v9.9.9"), nil },
		"malformed": func() (string, error) { return "version: v1.1.0\n", nil },
		"nonzero":   func() (string, error) { return "", errors.New("exit status 9") },
		"timeout":   func() (string, error) { return "", context.DeadlineExceeded },
	}
	for testName, failure := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			dir := resolvedTempDir(t)
			executable := filepath.Join(dir, binaryName)
			old := []byte("old")
			prior := []byte("older")
			r.NoError(os.WriteFile(executable, old, 0o751))
			r.NoError(os.WriteFile(executable+".previous", prior, 0o700))
			calls := 0
			runner := func(context.Context, Command) (string, error) {
				calls++
				if calls == 1 {
					return versionOutput("v1.1.0"), nil
				}
				return failure()
			}
			_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: runner, Timeout: 50 * time.Millisecond, OutputLimit: 1024})
			a.Error(err)
			installed, readErr := os.ReadFile(executable)
			r.NoError(readErr)
			a.Equal(old, installed)
			info, statErr := os.Stat(executable)
			r.NoError(statErr)
			a.Equal(fs.FileMode(0o751), info.Mode().Perm())
			previous, readErr := os.ReadFile(executable + ".previous")
			r.NoError(readErr)
			a.Equal(prior, previous)
			previousInfo, statErr := os.Stat(executable + ".previous")
			r.NoError(statErr)
			a.Equal(fs.FileMode(0o700), previousInfo.Mode().Perm())
			a.NoFileExists(executable + ".self-update.transaction")
		})
	}
}

func TestInstallRollsBackRealInstalledSubprocessFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update is unsupported on Windows")
	}
	tests := map[string]string{
		"nonzero": "exit 9",
		"timeout": "(sleep 30) &\nwait",
	}
	for testName, installedAction := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			a := assert.New(t)
			dir := resolvedTempDir(t)
			executable := filepath.Join(dir, binaryName)
			old := []byte("old")
			prior := []byte("older")
			r.NoError(os.WriteFile(executable, old, 0o751))
			r.NoError(os.WriteFile(executable+".previous", prior, 0o700))
			seen := filepath.Join(dir, "probe-seen")
			script := fmt.Sprintf("#!/bin/sh\nif [ ! -e '%s' ]; then\n  : > '%s'\n  printf 'porcelain: 1\\nversion: v1.1.0\\ncommit: test\\ndate: 2026-08-16T10:00:00Z\\n'\n  exit 0\nfi\n%s\n", seen, seen, installedAction)
			_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte(script), TargetVersion: "v1.1.0", RunCommand: runCommand, Timeout: 50 * time.Millisecond, OutputLimit: 1024})
			a.Error(err)
			installed, readErr := os.ReadFile(executable)
			r.NoError(readErr)
			a.Equal(old, installed)
			previous, readErr := os.ReadFile(executable + ".previous")
			r.NoError(readErr)
			a.Equal(prior, previous)
			a.NoFileExists(executable + ".self-update.transaction")
		})
	}
}

func TestFailedRollbackPreservesRestartSources(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o751))
	calls := 0
	runner := func(context.Context, Command) (string, error) {
		calls++
		if calls == 1 {
			return versionOutput("v1.1.0"), nil
		}
		previousDir := executable + ".previous"
		r.NoError(os.Mkdir(previousDir, 0o700))
		r.NoError(os.WriteFile(filepath.Join(previousDir, "blocker"), []byte("block"), 0o600))
		return "", errors.New("installed probe failed")
	}
	_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: runner, Timeout: time.Second, OutputLimit: 1024})
	a.Error(err)
	transactionPath := executable + ".self-update.transaction"
	data, readErr := os.ReadFile(transactionPath)
	r.NoError(readErr)
	var txn transaction
	r.NoError(json.Unmarshal(data, &txn))
	a.FileExists(txn.BackupPath)
	a.FileExists(transactionPath)
	r.NoError(os.Remove(filepath.Join(executable+".previous", "blocker")))
	r.NoError(os.Remove(executable + ".previous"))
	recovered, recoverErr := recoverInterruptedUpdate(executable)
	r.NoError(recoverErr)
	a.True(recovered)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("old"), installed)
	a.NoFileExists(transactionPath)
}

func TestInitialTransactionPublicationFailurePreservesRestartSources(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o751))
	_, err := installVerified(t.Context(), installRequest{Executable: executable, Binary: []byte("new"), TargetVersion: "v1.1.0", RunCommand: func(context.Context, Command) (string, error) { return versionOutput("v1.1.0"), nil }, Timeout: time.Second, OutputLimit: 1024, WriteTransaction: func(path string, txn transaction) error {
		r.NoError(writeTransaction(path, txn))
		return errors.New("injected post-publication sync failure")
	}})
	a.ErrorContains(err, "injected post-publication sync failure")
	transactionPath := executable + ".self-update.transaction"
	data, readErr := os.ReadFile(transactionPath)
	r.NoError(readErr)
	var txn transaction
	r.NoError(json.Unmarshal(data, &txn))
	a.FileExists(txn.BackupPath)
	a.FileExists(txn.CandidatePath)
	a.FileExists(txn.PreviousCandidate)
	recovered, recoverErr := recoverInterruptedUpdate(executable)
	r.NoError(recoverErr)
	a.True(recovered)
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("old"), installed)
	a.NoFileExists(transactionPath)
}

func TestCommittedCleanupRecoveryToleratesPartiallyMissingArtifacts(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("new"), 0o751))
	candidate := filepath.Join(dir, ".missing-candidate")
	previousCandidate := filepath.Join(dir, ".previous-candidate")
	previousBackup := filepath.Join(dir, ".previous-backup")
	r.NoError(os.WriteFile(previousCandidate, []byte("old"), 0o600))
	r.NoError(os.WriteFile(previousBackup, []byte("older"), 0o600))
	txn := transaction{BackupPath: filepath.Join(dir, ".missing-backup"), CandidatePath: candidate, PreviousCandidate: previousCandidate, PreviousBackupPath: previousBackup, PreviousMode: 0o700, Mode: 0o751, TargetVersion: "v1.1.0", Committed: true}
	r.NoError(writeTransaction(executable, txn))

	recovered, err := recoverInterruptedUpdate(executable)
	r.NoError(err)
	a.False(recovered)
	a.NoFileExists(previousCandidate)
	a.NoFileExists(previousBackup)
	a.NoFileExists(executable + ".self-update.transaction")
	installed, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("new"), installed)
}

func TestCrossProcessLockContentionAndKilledOwnerRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update is unsupported on Windows")
	}
	r := require.New(t)
	a := assert.New(t)
	dir := resolvedTempDir(t)
	executable := filepath.Join(dir, binaryName)
	r.NoError(os.WriteFile(executable, []byte("old"), 0o755))
	lock, err := acquireLock(executable)
	r.NoError(err)
	previous := executable + ".previous"
	r.NoError(os.WriteFile(previous, []byte("prior"), 0o700))
	executableInfo, err := os.Stat(executable)
	r.NoError(err)
	previousInfo, err := os.Stat(previous)
	r.NoError(err)
	contender := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	contender.Env = append(os.Environ(), "INVENTREE_LOCK_HELPER=1", "INVENTREE_LOCK_PATH="+executable, "INVENTREE_LOCK_MUTATE=1")
	err = contender.Run()
	var exitErr *exec.ExitError
	r.ErrorAs(err, &exitErr)
	a.Equal(3, exitErr.ExitCode())
	got, readErr := os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("old"), got)
	gotPrevious, readErr := os.ReadFile(previous)
	r.NoError(readErr)
	a.Equal([]byte("prior"), gotPrevious)
	currentInfo, err := os.Stat(executable)
	r.NoError(err)
	currentPreviousInfo, err := os.Stat(previous)
	r.NoError(err)
	a.Equal(executableInfo.Mode(), currentInfo.Mode())
	a.Equal(previousInfo.Mode(), currentPreviousInfo.Mode())
	a.NoFileExists(executable + ".self-update.transaction")
	lock.Release()

	ready := filepath.Join(dir, "ready")
	owner := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	owner.Env = append(os.Environ(), "INVENTREE_LOCK_HELPER=1", "INVENTREE_LOCK_PATH="+executable, "INVENTREE_LOCK_READY="+ready)
	r.NoError(owner.Start())
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, statErr := os.Stat(ready); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			r.FailNow("helper did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.NoError(owner.Process.Kill())
	_ = owner.Wait()
	got, readErr = os.ReadFile(executable)
	r.NoError(readErr)
	a.Equal([]byte("old"), got)
	gotPrevious, readErr = os.ReadFile(previous)
	r.NoError(readErr)
	a.Equal([]byte("prior"), gotPrevious)
	recovered, err := acquireLock(executable)
	r.NoError(err)
	recovered.Release()
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("INVENTREE_LOCK_HELPER") != "1" {
		return
	}
	lock, err := acquireLock(os.Getenv("INVENTREE_LOCK_PATH"))
	if err != nil {
		os.Exit(3)
	}
	if os.Getenv("INVENTREE_LOCK_MUTATE") == "1" {
		if err := os.WriteFile(os.Getenv("INVENTREE_LOCK_PATH"), []byte("mutated"), 0o755); err != nil {
			os.Exit(5)
		}
		if err := os.WriteFile(os.Getenv("INVENTREE_LOCK_PATH")+".previous", []byte("mutated"), 0o700); err != nil {
			os.Exit(6)
		}
	}
	if ready := os.Getenv("INVENTREE_LOCK_READY"); ready != "" {
		if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
			os.Exit(4)
		}
	}
	time.Sleep(30 * time.Second)
	lock.Release()
}

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

type failingReader struct{ read bool }

func (reader *failingReader) Read(buffer []byte) (int, error) {
	if !reader.read {
		reader.read = true
		copy(buffer, "12")
		return 2, nil
	}
	return 0, io.ErrUnexpectedEOF
}

type fileState struct {
	exists bool
	data   []byte
	mode   fs.FileMode
}

type installationState map[string]fileState

func captureInstallationState(t *testing.T, executable string) installationState {
	t.Helper()
	state := installationState{}
	for _, path := range []string{executable, executable + ".previous", directInstallMarkerPath(executable)} {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			state[path] = fileState{}
			continue
		}
		require.NoError(t, err)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		state[path] = fileState{exists: true, data: data, mode: info.Mode()}
	}
	return state
}

func assertInstallationState(t *testing.T, executable string, want installationState, wantLock bool) {
	t.Helper()
	r := require.New(t)
	a := assert.New(t)
	for path, expected := range want {
		info, err := os.Stat(path)
		if !expected.exists {
			a.ErrorIs(err, os.ErrNotExist, path)
			continue
		}
		r.NoError(err, path)
		data, readErr := os.ReadFile(path)
		r.NoError(readErr, path)
		a.Equal(expected.data, data, path)
		a.Equal(expected.mode, info.Mode(), path)
	}
	lockPath := executable + ".self-update.lock"
	if wantLock {
		info, err := os.Stat(lockPath)
		r.NoError(err)
		a.Equal(fs.FileMode(0o600), info.Mode().Perm())
	} else {
		a.NoFileExists(lockPath)
	}
	a.NoFileExists(executable + ".self-update.transaction")
	for _, pattern := range []string{
		".inventree-mcp-update-*",
		".inventree-mcp-rollback-*",
		".inventree-mcp-previous-backup-*",
		".inventree-mcp-previous-candidate-*",
		".inventree-mcp-restore-*",
		".inventree-mcp-previous-restore-*",
		".inventree-mcp-transaction-*",
	} {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(executable), pattern))
		r.NoError(err)
		a.Empty(matches, pattern)
	}
}

type mapHTTPClient struct {
	responses map[string][]byte
	requests  []*http.Request
}

func (client *mapHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.requests = append(client.requests, request.Clone(request.Context()))
	data, ok := client.responses[request.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("missing"))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func releaseClient(version string, archive []byte, goos, goarch string) *mapHTTPClient {
	name := releaseArchiveName(goos, goarch)
	hash := sha256.Sum256(archive)
	return &mapHTTPClient{responses: map[string][]byte{
		"https://api.github.com/repos/davidvanlaatum/inventree-mcp/releases/latest":                       releaseJSON(version, false, false, goos, goarch),
		"https://github.com/davidvanlaatum/inventree-mcp/releases/download/" + version + "/" + name:       archive,
		"https://github.com/davidvanlaatum/inventree-mcp/releases/download/" + version + "/checksums.txt": []byte(hex.EncodeToString(hash[:]) + "  " + name + "\n"),
	}}
}

func releaseJSON(version string, draft, prerelease bool, goos, goarch string) []byte {
	name := releaseArchiveName(goos, goarch)
	return []byte(fmt.Sprintf(`{"tag_name":%q,"draft":%t,"prerelease":%t,"assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
		version, draft, prerelease, name,
		"https://github.com/davidvanlaatum/inventree-mcp/releases/download/"+version+"/"+name,
		"https://github.com/davidvanlaatum/inventree-mcp/releases/download/"+version+"/checksums.txt"))
}

type archiveEntry struct {
	name     string
	mode     int64
	body     []byte
	typeFlag byte
	linkName string
}

func makeArchive(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	r := require.New(t)
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gz)
	for _, entry := range entries {
		typeFlag := entry.typeFlag
		if typeFlag == 0 {
			typeFlag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body)), Typeflag: typeFlag, Linkname: entry.linkName}
		r.NoError(tarWriter.WriteHeader(header))
		if len(entry.body) > 0 {
			_, err := tarWriter.Write(entry.body)
			r.NoError(err)
		}
	}
	r.NoError(tarWriter.Close())
	r.NoError(gz.Close())
	return output.Bytes()
}

func versionOutput(version string) string {
	return "porcelain: 1\nversion: " + version + "\ncommit: test\ndate: 2026-08-16T10:00:00Z\ninventree_version: " + buildinfo.PinnedInvenTreeVersion + "\ninventree_api: " + buildinfo.PinnedInvenTreeAPIVersion + "\n"
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return resolved
}
