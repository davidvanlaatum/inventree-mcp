//go:build !unix && !windows

package server

import "os"

func openTrafficLogFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func trafficLogFilePermissionsSafe(mode os.FileMode) bool {
	return mode.Perm()&0o077 == 0
}
