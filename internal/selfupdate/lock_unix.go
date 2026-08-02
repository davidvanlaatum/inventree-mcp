//go:build darwin || linux

package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

type updateLock struct {
	file *os.File
}

func acquireLock(executable string) (*updateLock, error) {
	path := executable + ".self-update.lock"
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open self-update lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	closeOnError := func(cause error) (*updateLock, error) {
		return nil, errors.Join(cause, file.Close())
	}
	if err := validateOwnedRegular(path, false); err != nil {
		return closeOnError(fmt.Errorf("unsafe self-update lock: %w", err))
	}
	info, err := file.Stat()
	if err != nil {
		return closeOnError(err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, pathInfo) || info.Mode().Perm()&0o077 != 0 {
		return closeOnError(fmt.Errorf("unsafe self-update lock identity or permissions"))
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			_, _ = closeOnError(nil)
			return nil, ErrUpdateLocked
		}
		return closeOnError(fmt.Errorf("acquire self-update lock: %w", err))
	}
	pathInfo, err = os.Lstat(path)
	if err != nil || !os.SameFile(info, pathInfo) {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		return closeOnError(fmt.Errorf("self-update lock identity changed"))
	}
	return &updateLock{file: file}, nil
}

func (lock *updateLock) Release() {
	_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	_ = lock.file.Close()
}
