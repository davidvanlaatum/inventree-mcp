//go:build unix

package server

import (
	"os"
	"syscall"
)

func openTrafficLogFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
}

func trafficLogFilePermissionsSafe(mode os.FileMode) bool {
	return mode.Perm()&0o077 == 0
}
