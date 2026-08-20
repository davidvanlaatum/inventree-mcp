//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || windows

package testenv

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTestEnvLockCrossProcess(t *testing.T) {
	lock, err := acquireTestEnvLock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.release()) })

	cmd := exec.Command(os.Args[0], "-test.run=^TestTestEnvLockHelper$")
	cmd.Env = append(os.Environ(), "TESTENV_LOCK_HELPER=blocked")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("blocked lock helper output:\n%s", output)
	}
	require.NoError(t, err)
	require.NoError(t, lock.release())

	cmd = exec.Command(os.Args[0], "-test.run=^TestTestEnvLockHelper$")
	cmd.Env = append(os.Environ(), "TESTENV_LOCK_HELPER=available")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("available lock helper output:\n%s", output)
	}
	require.NoError(t, err)
}

func TestTestEnvLockHelper(t *testing.T) {
	mode := os.Getenv("TESTENV_LOCK_HELPER")
	if mode == "" {
		t.Skip()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	lock, err := acquireTestEnvLock(ctx)
	if mode == "blocked" {
		require.ErrorIs(t, err, context.DeadlineExceeded)
		return
	}
	require.NoError(t, err)
	require.NoError(t, lock.release())
}
