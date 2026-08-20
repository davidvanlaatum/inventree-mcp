//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return &testEnvLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
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
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
