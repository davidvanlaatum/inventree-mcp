// Package selfupdate implements the local-only inventree-mcp binary updater.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
	"golang.org/x/mod/semver"
)

const (
	repositoryOwner = "davidvanlaatum"
	repositoryName  = "inventree-mcp"
	binaryName      = "inventree-mcp"
)

var (
	ErrUnsupportedPlatform = errors.New("self-update is unsupported on this platform")
	ErrPackageManaged      = errors.New("package-managed installation")
	ErrUnsafeTarget        = errors.New("unsafe update target")
	ErrUpdateLocked        = errors.New("another self-update is active")
	ErrVersionPolicy       = errors.New("version is rejected by update policy")
)

// Options controls one explicit local update.
type Options struct {
	TargetVersion      string
	GitHubToken        string
	AdoptDirectInstall bool
}

// Result describes the outcome of one update attempt. The InvenTree baseline fields are
// populated only when Updated is true; PreviousInvenTree* comes from the already-running
// process's own compiled-in constants, and NewInvenTree* comes from the pre-rename staged
// candidate's parsed version output. Either pair may be empty when the corresponding
// build predates internal/buildinfo's InvenTree-baseline constants.
type Result struct {
	PreviousVersion string
	Version         string
	BackupPath      string
	Updated         bool
	Adopted         bool

	PreviousInvenTreeVersion string
	PreviousInvenTreeAPI     string
	NewInvenTreeVersion      string
	NewInvenTreeAPI          string
}

// Limits bounds all network, archive, and process work.
type Limits struct {
	MetadataBytes   int64
	ChecksumBytes   int64
	ArchiveBytes    int64
	ExecutableBytes int64
	ProcessOutput   int64
	HTTPTimeout     time.Duration
	ProcessTimeout  time.Duration
}

func defaultLimits() Limits {
	return Limits{
		MetadataBytes:   2 << 20,
		ChecksumBytes:   1 << 20,
		ArchiveBytes:    64 << 20,
		ExecutableBytes: 128 << 20,
		ProcessOutput:   64 << 10,
		HTTPTimeout:     30 * time.Second,
		ProcessTimeout:  5 * time.Second,
	}
}

// Dependencies provides deterministic seams for tests.
type Dependencies struct {
	Client                HTTPDoer
	ExecutablePath        func() (string, error)
	ValidateInstallTarget func(string, string) error
	RunCommand            CommandRunner
	GOOS                  string
	GOARCH                string
	Limits                Limits
	APIBaseURL            string
}

// Updater performs one local binary update.
type Updater struct {
	deps Dependencies
}

// New constructs an updater and fills omitted production dependencies.
func New(deps Dependencies) *Updater {
	limits := deps.Limits
	defaults := defaultLimits()
	if limits.MetadataBytes == 0 {
		limits.MetadataBytes = defaults.MetadataBytes
	}
	if limits.ChecksumBytes == 0 {
		limits.ChecksumBytes = defaults.ChecksumBytes
	}
	if limits.ArchiveBytes == 0 {
		limits.ArchiveBytes = defaults.ArchiveBytes
	}
	if limits.ExecutableBytes == 0 {
		limits.ExecutableBytes = defaults.ExecutableBytes
	}
	if limits.ProcessOutput == 0 {
		limits.ProcessOutput = defaults.ProcessOutput
	}
	if limits.HTTPTimeout == 0 {
		limits.HTTPTimeout = defaults.HTTPTimeout
	}
	if limits.ProcessTimeout == 0 {
		limits.ProcessTimeout = defaults.ProcessTimeout
	}
	deps.Limits = limits
	if deps.ExecutablePath == nil {
		deps.ExecutablePath = os.Executable
	}
	if deps.ValidateInstallTarget == nil {
		deps.ValidateInstallTarget = validateInstallTarget
	}
	if deps.RunCommand == nil {
		deps.RunCommand = runCommand
	}
	if deps.GOOS == "" {
		deps.GOOS = runtime.GOOS
	}
	if deps.GOARCH == "" {
		deps.GOARCH = runtime.GOARCH
	}
	if deps.APIBaseURL == "" {
		deps.APIBaseURL = "https://api.github.com"
	}
	if deps.Client == nil {
		deps.Client = newHTTPClient(limits.HTTPTimeout, nil)
	}
	return &Updater{deps: deps}
}

// Run resolves, verifies, and atomically installs the requested release.
func (u *Updater) Run(ctx context.Context, currentVersion string, options Options) (Result, error) {
	current, err := canonicalStableVersion(currentVersion)
	if err != nil {
		return Result{}, fmt.Errorf("current build version %q: %w", currentVersion, err)
	}
	if err := supportedPlatform(u.deps.GOOS, u.deps.GOARCH); err != nil {
		return Result{}, err
	}
	if options.AdoptDirectInstall {
		if options.TargetVersion != "" {
			return Result{}, fmt.Errorf("adoption cannot be combined with a target version")
		}
		executable, err := u.deps.ExecutablePath()
		if err != nil {
			return Result{}, fmt.Errorf("locate current executable: %w", err)
		}
		if err := u.deps.ValidateInstallTarget(executable, u.deps.GOOS); err != nil {
			return Result{}, err
		}
		if err := adoptDirectInstall(executable); err != nil {
			return Result{}, err
		}
		return Result{PreviousVersion: current, Version: current, Adopted: true}, nil
	}

	target := strings.TrimSpace(options.TargetVersion)
	if target != "" {
		target, err = canonicalStableVersion(target)
		if err != nil {
			return Result{}, fmt.Errorf("target version: %w", err)
		}
		if semver.Compare(target, current) < 0 {
			return Result{}, fmt.Errorf("downgrade from %s to %s: %w", current, target, ErrVersionPolicy)
		}
	}
	executable, err := u.deps.ExecutablePath()
	if err != nil {
		return Result{}, fmt.Errorf("locate current executable: %w", err)
	}
	if err := u.deps.ValidateInstallTarget(executable, u.deps.GOOS); err != nil {
		return Result{}, err
	}
	if err := validateDirectInstallMarker(executable); err != nil {
		return Result{}, err
	}
	lock, err := acquireLock(executable)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	recovered, err := recoverInterruptedUpdate(executable)
	if err != nil {
		return Result{}, fmt.Errorf("recover interrupted update: %w", err)
	}
	if recovered {
		return Result{}, fmt.Errorf("recovered an interrupted self-update; rerun self-update so version selection uses the restored executable")
	}

	release, err := u.resolveRelease(ctx, target, options.GitHubToken)
	if err != nil {
		return Result{}, err
	}
	if semver.Compare(release.Version, current) < 0 {
		return Result{}, fmt.Errorf("resolved release %s is older than %s: %w", release.Version, current, ErrVersionPolicy)
	}
	result := Result{PreviousVersion: current, Version: release.Version}
	if semver.Compare(release.Version, current) == 0 {
		return result, nil
	}

	archive, err := u.download(ctx, release.ArchiveURL, options.GitHubToken, u.deps.Limits.ArchiveBytes)
	if err != nil {
		return Result{}, fmt.Errorf("download release archive: %w", err)
	}
	checksums, err := u.download(ctx, release.ChecksumURL, options.GitHubToken, u.deps.Limits.ChecksumBytes)
	if err != nil {
		return Result{}, fmt.Errorf("download release checksums: %w", err)
	}
	if err := verifyChecksum(release.ArchiveName, archive, checksums); err != nil {
		return Result{}, err
	}
	binary, mode, err := extractExecutable(archive, u.deps.Limits.ExecutableBytes)
	if err != nil {
		return Result{}, err
	}
	installed, err := installVerified(ctx, installRequest{
		Executable:    executable,
		Binary:        binary,
		Mode:          mode,
		TargetVersion: release.Version,
		RunCommand:    u.deps.RunCommand,
		Timeout:       u.deps.Limits.ProcessTimeout,
		OutputLimit:   u.deps.Limits.ProcessOutput,
	})
	if err != nil {
		return Result{}, err
	}
	result.Updated = true
	result.BackupPath = installed.PreviousPath
	result.PreviousInvenTreeVersion = buildinfo.PinnedInvenTreeVersion
	result.PreviousInvenTreeAPI = buildinfo.PinnedInvenTreeAPIVersion
	result.NewInvenTreeVersion = installed.Candidate.InvenTreeVersion
	result.NewInvenTreeAPI = installed.Candidate.InvenTreeAPI
	return result, nil
}

func canonicalStableVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "dev" || value == "unknown" {
		return "", fmt.Errorf("%w: a released semantic version is required", ErrVersionPolicy)
	}
	if value[0] != 'v' {
		value = "v" + value
	}
	if !semver.IsValid(value) {
		return "", fmt.Errorf("%w: %q is not semantic version", ErrVersionPolicy, value)
	}
	canonical := semver.Canonical(value)
	if canonical != value {
		return "", fmt.Errorf("%w: version must use exact vX.Y.Z form", ErrVersionPolicy)
	}
	if semver.Prerelease(value) != "" || semver.Build(value) != "" {
		return "", fmt.Errorf("%w: prerelease and build metadata versions are unsupported", ErrVersionPolicy)
	}
	return canonical, nil
}

func supportedPlatform(goos, goarch string) error {
	if (goos != "linux" && goos != "darwin") || (goarch != "amd64" && goarch != "arm64") {
		return fmt.Errorf("%s/%s: %w; download a release manually", goos, goarch, ErrUnsupportedPlatform)
	}
	return nil
}
