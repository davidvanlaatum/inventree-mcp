//go:build windows

package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

const testEnvLockFilename = "inventree-mcp-testenv.lock"

type testEnvLock struct {
	file *os.File
}

func acquireTestEnvLock(ctx context.Context) (*testEnvLock, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for InvenTree test environment lock: %w", ctx.Err())
	default:
	}

	path := filepath.Join(os.TempDir(), testEnvLockFilename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open InvenTree test environment lock %q: %w", path, err)
	}

	for {
		var overlapped windows.Overlapped
		err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
		if err == nil {
			return &testEnvLock{file: file}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, fmt.Errorf("lock InvenTree test environment: %w", err)
		}

		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("wait for InvenTree test environment lock: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (l *testEnvLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	var overlapped windows.Overlapped
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
