package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/buildinfo"
)

// Command describes a bounded, credential-isolated subprocess.
type Command struct {
	Path        string
	Args        []string
	Dir         string
	Env         []string
	Timeout     time.Duration
	OutputLimit int64
}

// CommandRunner executes one bounded command and returns combined output.
type CommandRunner func(context.Context, Command) (string, error)

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func (w *boundedBuffer) Write(data []byte) (int, error) {
	remaining := w.limit - int64(w.buffer.Len())
	if remaining <= 0 {
		w.exceeded = true
		return 0, fmt.Errorf("process output exceeds %d bytes", w.limit)
	}
	if int64(len(data)) > remaining {
		w.exceeded = true
		_, _ = w.buffer.Write(data[:remaining])
		return int(remaining), fmt.Errorf("process output exceeds %d bytes", w.limit)
	}
	return w.buffer.Write(data)
}

func runCommand(ctx context.Context, command Command) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, command.Timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, command.Path, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = append([]string(nil), command.Env...)
	cmd.Stdin = bytes.NewReader(nil)
	configureCommand(cmd)
	cmd.WaitDelay = time.Second
	output := &boundedBuffer{limit: command.OutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if output.exceeded {
		return output.buffer.String(), fmt.Errorf("process output exceeds %d bytes", command.OutputLimit)
	}
	if commandCtx.Err() != nil {
		return output.buffer.String(), fmt.Errorf("command timed out: %w", commandCtx.Err())
	}
	if err != nil {
		return output.buffer.String(), err
	}
	return output.buffer.String(), nil
}

type installRequest struct {
	Executable         string
	Binary             []byte
	Mode               fs.FileMode
	TargetVersion      string
	RunCommand         CommandRunner
	Timeout            time.Duration
	OutputLimit        int64
	SyncDirectory      func(string) error
	RemoveTransaction  func(string) error
	WriteTransaction   func(string, transaction) error
	ReplaceTransaction func(string, transaction) error
}

type transaction struct {
	BackupPath         string `json:"backup_path"`
	CandidatePath      string `json:"candidate_path"`
	PreviousCandidate  string `json:"previous_candidate"`
	PreviousBackupPath string `json:"previous_backup_path,omitempty"`
	PreviousMode       uint32 `json:"previous_mode,omitempty"`
	Mode               uint32 `json:"mode"`
	TargetVersion      string `json:"target_version"`
	Committed          bool   `json:"committed,omitempty"`
}

const directInstallMarkerVersion = "inventree-mcp-direct-install-v1"

func directInstallMarkerPath(executable string) string { return executable + ".direct-install" }

func directInstallMarkerContent(executable string) string {
	return directInstallMarkerVersion + "\nrepository=" + repositoryOwner + "/" + repositoryName + "\nexecutable=" + filepath.Clean(executable) + "\n"
}

func adoptDirectInstall(executable string) error {
	path := directInstallMarkerPath(executable)
	if err := validateDirectInstallMarker(executable); err == nil {
		return nil
	} else if _, statErr := os.Lstat(path); statErr == nil {
		return err
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create direct-install marker: %w", err)
	}
	content := directInstallMarkerContent(executable)
	if _, err := io.WriteString(file, content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write direct-install marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("sync direct-install marker: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close direct-install marker: %w", err)
	}
	if err := syncDirectory(filepath.Dir(executable)); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("sync direct-install marker directory: %w", err)
	}
	return validateDirectInstallMarker(executable)
}

func validateDirectInstallMarker(executable string) error {
	path := directInstallMarkerPath(executable)
	if err := validateOwnedRegular(path, false); err != nil {
		return fmt.Errorf("direct-archive installation marker is missing or unsafe: %w; if this binary came directly from the canonical GitHub archive, run self-update --adopt-direct-install once", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 || info.Size() > 4<<10 {
		return fmt.Errorf("direct-archive installation marker has unsafe permissions or size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(data) != directInstallMarkerContent(executable) {
		return fmt.Errorf("direct-archive installation marker does not match this executable path and repository")
	}
	return nil
}

func validateInstallTarget(executable, goos string) error {
	clean := filepath.Clean(executable)
	if goos == "windows" {
		return fmt.Errorf("windows running-executable replacement: %w", ErrUnsupportedPlatform)
	}
	if goos == "linux" && clean == "/usr/bin/"+binaryName {
		return fmt.Errorf("%s is reserved for deb, rpm, or apk ownership: %w; use the system package manager", clean, ErrPackageManaged)
	}
	if goos == "darwin" && (strings.HasPrefix(clean, "/opt/homebrew/") || strings.Contains(clean, "/Cellar/")) {
		return fmt.Errorf("%s appears package-managed: %w; use the package manager", clean, ErrPackageManaged)
	}
	if err := validateOwnedRegular(clean, true); err != nil {
		return fmt.Errorf("%w: executable: %v", ErrUnsafeTarget, err)
	}
	parent := filepath.Dir(clean)
	if err := validateOwnedDirectory(parent); err != nil {
		return fmt.Errorf("%w: executable directory: %v", ErrUnsafeTarget, err)
	}
	if err := validateTrustedAncestorChain(parent); err != nil {
		return fmt.Errorf("%w: executable ancestor chain: %v", ErrUnsafeTarget, err)
	}
	return nil
}

// installResult reports the prior-executable backup path plus the pre-rename staged
// candidate's parsed version output, so callers can build an update summary without a
// second exec of the installed binary.
type installResult struct {
	PreviousPath string
	Candidate    buildinfo.PorcelainInfo
}

func installVerified(ctx context.Context, request installRequest) (installResult, error) {
	if request.SyncDirectory == nil {
		request.SyncDirectory = syncDirectory
	}
	if request.RemoveTransaction == nil {
		request.RemoveTransaction = os.Remove
	}
	if request.WriteTransaction == nil {
		request.WriteTransaction = writeTransaction
	}
	if request.ReplaceTransaction == nil {
		request.ReplaceTransaction = replaceTransaction
	}
	if err := validateInstallTarget(request.Executable, runtimeGOOS()); err != nil {
		return installResult{}, err
	}
	info, err := os.Lstat(request.Executable)
	if err != nil {
		return installResult{}, fmt.Errorf("stat installed executable: %w", err)
	}
	originalMode := info.Mode().Perm()
	dir := filepath.Dir(request.Executable)
	transactionOwnsArtifacts := false
	candidate, err := os.CreateTemp(dir, "."+binaryName+"-update-*")
	if err != nil {
		return installResult{}, fmt.Errorf("create update staging file: %w", err)
	}
	candidatePath := candidate.Name()
	defer func() {
		_ = candidate.Close()
		if !transactionOwnsArtifacts {
			_ = os.Remove(candidatePath)
		}
	}()
	if err := candidate.Chmod(0o700); err != nil {
		return installResult{}, fmt.Errorf("protect update staging file: %w", err)
	}
	if _, err := io.Copy(candidate, bytes.NewReader(request.Binary)); err != nil {
		return installResult{}, fmt.Errorf("write update staging file: %w", err)
	}
	if err := candidate.Sync(); err != nil {
		return installResult{}, fmt.Errorf("sync update staging file: %w", err)
	}
	if err := candidate.Close(); err != nil {
		return installResult{}, fmt.Errorf("close update staging file: %w", err)
	}
	candidateInfo, err := requireVersion(ctx, request.RunCommand, candidatePath, dir, request.TargetVersion, request.Timeout, request.OutputLimit)
	if err != nil {
		return installResult{}, fmt.Errorf("validate staged executable: %w", err)
	}
	if err := validateInstallTarget(request.Executable, runtimeGOOS()); err != nil {
		return installResult{}, fmt.Errorf("target changed before replacement: %w", err)
	}
	currentInfo, err := os.Lstat(request.Executable)
	if err != nil {
		return installResult{}, fmt.Errorf("re-stat target before replacement: %w", err)
	}
	if !os.SameFile(info, currentInfo) || info.Mode() != currentInfo.Mode() || info.Size() != currentInfo.Size() {
		return installResult{}, fmt.Errorf("target identity, mode, or size changed before replacement: %w", ErrUnsafeTarget)
	}

	backupFile, err := os.CreateTemp(dir, "."+binaryName+"-rollback-*")
	if err != nil {
		return installResult{}, fmt.Errorf("create rollback file: %w", err)
	}
	backupPath := backupFile.Name()
	defer func() {
		_ = backupFile.Close()
		if !transactionOwnsArtifacts {
			_ = os.Remove(backupPath)
		}
	}()
	if err := backupFile.Chmod(0o600); err != nil {
		return installResult{}, fmt.Errorf("protect rollback file: %w", err)
	}
	current, err := os.Open(request.Executable)
	if err != nil {
		return installResult{}, fmt.Errorf("open installed executable for backup: %w", err)
	}
	_, copyErr := io.Copy(backupFile, current)
	openedInfo, statOpenErr := current.Stat()
	closeCurrentErr := current.Close()
	if copyErr != nil {
		return installResult{}, fmt.Errorf("copy installed executable to rollback file: %w", copyErr)
	}
	if closeCurrentErr != nil {
		return installResult{}, fmt.Errorf("close installed executable backup source: %w", closeCurrentErr)
	}
	if statOpenErr != nil {
		return installResult{}, fmt.Errorf("stat installed executable backup source: %w", statOpenErr)
	}
	if !os.SameFile(info, openedInfo) {
		return installResult{}, fmt.Errorf("target changed while creating rollback file: %w", ErrUnsafeTarget)
	}
	if err := backupFile.Sync(); err != nil {
		return installResult{}, fmt.Errorf("sync rollback file: %w", err)
	}
	if err := backupFile.Close(); err != nil {
		return installResult{}, fmt.Errorf("close rollback file: %w", err)
	}
	previousPath := request.Executable + ".previous"
	if err := validateExistingPrevious(previousPath); err != nil {
		return installResult{}, err
	}
	previousBackupPath := ""
	previousMode := fs.FileMode(0)
	if _, err := os.Lstat(previousPath); err == nil {
		previousInfo, statErr := os.Stat(previousPath)
		if statErr != nil {
			return installResult{}, statErr
		}
		previousMode = previousInfo.Mode().Perm()
		previousBackupPath, err = cloneToTemp(previousPath, dir, "."+binaryName+"-previous-backup-*", 0o600)
		if err != nil {
			return installResult{}, fmt.Errorf("preserve existing previous executable: %w", err)
		}
		defer func() {
			if !transactionOwnsArtifacts {
				_ = os.Remove(previousBackupPath)
			}
		}()
	} else if !errors.Is(err, os.ErrNotExist) {
		return installResult{}, err
	}
	previousCandidate, err := cloneToTemp(backupPath, dir, "."+binaryName+"-previous-candidate-*", 0o600)
	if err != nil {
		return installResult{}, fmt.Errorf("prepare previous executable: %w", err)
	}
	defer func() {
		if !transactionOwnsArtifacts {
			_ = os.Remove(previousCandidate)
		}
	}()
	finalInfo, err := os.Lstat(request.Executable)
	if err != nil {
		return installResult{}, fmt.Errorf("final target validation: %w", err)
	}
	if !os.SameFile(info, finalInfo) || info.Mode() != finalInfo.Mode() || info.Size() != finalInfo.Size() {
		return installResult{}, fmt.Errorf("target changed before atomic replacement: %w", ErrUnsafeTarget)
	}
	if err := os.Chmod(candidatePath, originalMode); err != nil {
		return installResult{}, fmt.Errorf("set installed mode on candidate: %w", err)
	}

	txn := transaction{
		BackupPath:         backupPath,
		CandidatePath:      candidatePath,
		PreviousCandidate:  previousCandidate,
		PreviousBackupPath: previousBackupPath,
		PreviousMode:       uint32(previousMode),
		Mode:               uint32(originalMode),
		TargetVersion:      request.TargetVersion,
	}
	transactionPath := request.Executable + ".self-update.transaction"
	if err := request.WriteTransaction(request.Executable, txn); err != nil {
		if _, statErr := os.Lstat(transactionPath); statErr == nil {
			transactionOwnsArtifacts = true
		}
		return installResult{}, err
	}
	transactionOwnsArtifacts = true
	if err := os.Rename(candidatePath, request.Executable); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("atomically replace executable: %w", err))
	}
	if err := request.SyncDirectory(dir); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("sync executable directory: %w", err))
	}
	if _, err := requireVersion(ctx, request.RunCommand, request.Executable, dir, request.TargetVersion, request.Timeout, request.OutputLimit); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("validate installed executable: %w", err))
	}
	if err := os.Chmod(backupPath, originalMode); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("set previous executable mode: %w", err))
	}
	if err := os.Chmod(previousCandidate, originalMode); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("set previous candidate mode: %w", err))
	}
	if err := os.Rename(previousCandidate, previousPath); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("publish previous executable: %w", err))
	}
	if err := request.SyncDirectory(dir); err != nil {
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("sync previous executable: %w", err))
	}
	txn.Committed = true
	result := installResult{PreviousPath: previousPath, Candidate: candidateInfo}
	if err := request.ReplaceTransaction(request.Executable, txn); err != nil {
		var publishedError *transactionPublishError
		if errors.As(err, &publishedError) {
			return result, nil
		}
		return installResult{}, rollbackTransaction(request.Executable, txn, fmt.Errorf("commit update transaction: %w", err))
	}
	if err := cleanupCommittedTransaction(request.Executable, txn, request.RemoveTransaction, request.SyncDirectory); err != nil {
		return result, nil
	}
	transactionOwnsArtifacts = false
	return result, nil
}

func cloneToTemp(source, dir, pattern string, mode fs.FileMode) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if mode == 0 {
		mode = info.Mode().Perm()
	}
	output, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := output.Name()
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := output.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := io.Copy(output, input); err != nil {
		return "", err
	}
	if err := output.Chmod(mode); err != nil {
		return "", err
	}
	if err := output.Sync(); err != nil {
		return "", err
	}
	if err := output.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

// requireVersion runs executable's bounded `version` command and requires its porcelain
// output to name exactly the wanted release, returning the parsed fields alongside its
// pass/fail result so callers can reuse them instead of re-executing the candidate.
func requireVersion(ctx context.Context, runner CommandRunner, executable, dir, want string, timeout time.Duration, outputLimit int64) (buildinfo.PorcelainInfo, error) {
	output, err := runner(ctx, Command{
		Path:        executable,
		Args:        []string{"version"},
		Dir:         dir,
		Env:         []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin"},
		Timeout:     timeout,
		OutputLimit: outputLimit,
	})
	if err != nil {
		return buildinfo.PorcelainInfo{}, err
	}
	info, err := buildinfo.ParsePorcelain(output)
	if err != nil {
		return buildinfo.PorcelainInfo{}, fmt.Errorf("malformed version output: %w", err)
	}
	got, err := canonicalStableVersion(info.Version)
	if err != nil {
		return buildinfo.PorcelainInfo{}, err
	}
	if got != want {
		return buildinfo.PorcelainInfo{}, fmt.Errorf("version mismatch: got %s, want %s", got, want)
	}
	return info, nil
}

func writeTransaction(executable string, txn transaction) error {
	path := executable + ".self-update.transaction"
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("create update transaction: file already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect update transaction: %w", err)
	}
	return publishTransaction(executable, txn)
}

func replaceTransaction(executable string, txn transaction) error {
	return publishTransaction(executable, txn)
}

func publishTransaction(executable string, txn transaction) error {
	path := executable + ".self-update.transaction"
	file, err := os.CreateTemp(filepath.Dir(executable), "."+binaryName+"-transaction-*")
	if err != nil {
		return fmt.Errorf("create update transaction: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect update transaction: %w", err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(txn); err != nil {
		_ = file.Close()
		return fmt.Errorf("write update transaction: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync update transaction: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close update transaction: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish update transaction: %w", err)
	}
	if err := syncDirectory(filepath.Dir(executable)); err != nil {
		return &transactionPublishError{err: err}
	}
	return nil
}

type transactionPublishError struct{ err error }

func (err *transactionPublishError) Error() string {
	return "transaction was published but its directory sync failed: " + err.err.Error()
}
func (err *transactionPublishError) Unwrap() error { return err.err }

func recoverInterruptedUpdate(executable string) (bool, error) {
	path := executable + ".self-update.transaction"
	if err := validateOwnedRegular(path, false); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("unsafe transaction file: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Size() > 64<<10 {
		return false, fmt.Errorf("transaction file exceeds size policy")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var txn transaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&txn); err != nil {
		return false, fmt.Errorf("decode transaction: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return false, fmt.Errorf("decode transaction: %w", err)
	}
	dir := filepath.Dir(executable)
	for _, artifact := range []string{txn.BackupPath, txn.CandidatePath, txn.PreviousCandidate} {
		if filepath.Dir(artifact) != dir {
			return false, fmt.Errorf("transaction paths leave executable directory")
		}
	}
	if txn.PreviousBackupPath != "" && filepath.Dir(txn.PreviousBackupPath) != dir {
		return false, fmt.Errorf("transaction paths leave executable directory")
	}
	if txn.Committed {
		if err := cleanupCommittedTransaction(executable, txn, os.Remove, syncDirectory); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := validateOwnedRegular(txn.BackupPath, false); err != nil {
		return false, fmt.Errorf("unsafe rollback file: %w", err)
	}
	if err := rollbackTransaction(executable, txn, nil); err != nil {
		return false, err
	}
	return true, nil
}

func cleanupCommittedTransaction(executable string, txn transaction, removeTransaction func(string) error, syncDir func(string) error) error {
	dir := filepath.Dir(executable)
	for _, artifact := range []string{txn.BackupPath, txn.CandidatePath, txn.PreviousCandidate, txn.PreviousBackupPath} {
		if artifact == "" {
			continue
		}
		if err := validateOwnedRegular(artifact, false); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("unsafe committed cleanup artifact: %w", err)
		}
		if err := os.Remove(artifact); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove committed cleanup artifact: %w", err)
		}
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync committed artifact cleanup: %w", err)
	}
	transactionPath := executable + ".self-update.transaction"
	if err := removeTransaction(transactionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove committed transaction: %w", err)
	}
	if err := syncDir(dir); err != nil {
		republishErr := replaceTransaction(executable, txn)
		return errors.Join(fmt.Errorf("sync committed transaction removal: %w", err), republishErr)
	}
	return nil
}

func rollbackTransaction(executable string, txn transaction, cause error) error {
	mode, err := validatedTransactionMode(txn.Mode)
	if err != nil {
		return errors.Join(cause, err)
	}
	restoreCandidate, err := cloneToTemp(txn.BackupPath, filepath.Dir(executable), "."+binaryName+"-restore-*", mode)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("prepare executable rollback: %w", err))
	}
	defer func() { _ = os.Remove(restoreCandidate) }()
	if err := os.Rename(restoreCandidate, executable); err != nil {
		return errors.Join(cause, fmt.Errorf("restore previous executable: %w", err))
	}
	previousPath := executable + ".previous"
	if txn.PreviousBackupPath == "" {
		if err := os.Remove(previousPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(cause, fmt.Errorf("remove newly published previous executable: %w", err))
		}
	} else {
		if err := validateOwnedRegular(txn.PreviousBackupPath, false); err != nil {
			return errors.Join(cause, fmt.Errorf("validate previous executable rollback: %w", err))
		}
		previousMode, err := validatedTransactionMode(txn.PreviousMode)
		if err != nil {
			return errors.Join(cause, fmt.Errorf("validate prior previous executable mode: %w", err))
		}
		previousRestore, err := cloneToTemp(txn.PreviousBackupPath, filepath.Dir(executable), "."+binaryName+"-previous-restore-*", previousMode)
		if err != nil {
			return errors.Join(cause, fmt.Errorf("prepare prior previous executable rollback: %w", err))
		}
		defer func() { _ = os.Remove(previousRestore) }()
		if err := os.Rename(previousRestore, previousPath); err != nil {
			return errors.Join(cause, fmt.Errorf("restore prior previous executable: %w", err))
		}
	}
	_ = os.Remove(txn.CandidatePath)
	_ = os.Remove(txn.PreviousCandidate)
	if err := syncDirectory(filepath.Dir(executable)); err != nil {
		return errors.Join(cause, fmt.Errorf("sync restored rollback state: %w", err))
	}
	txn.Committed = true
	if err := replaceTransaction(executable, txn); err != nil {
		var publishedError *transactionPublishError
		if errors.As(err, &publishedError) {
			return cause
		}
		return errors.Join(cause, fmt.Errorf("commit rollback cleanup state: %w", err))
	}
	if err := cleanupCommittedTransaction(executable, txn, os.Remove, syncDirectory); err != nil {
		return errors.Join(cause, fmt.Errorf("clean completed rollback artifacts: %w", err))
	}
	return cause
}

func validatedTransactionMode(raw uint32) (fs.FileMode, error) {
	mode := fs.FileMode(raw)
	if mode != mode.Perm() || mode&0o111 == 0 || mode&0o022 != 0 {
		return 0, fmt.Errorf("unsafe rollback executable mode %v", mode)
	}
	return mode, nil
}

func validateExistingPrevious(path string) error {
	err := validateOwnedRegular(path, true)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unsafe existing previous executable: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}
