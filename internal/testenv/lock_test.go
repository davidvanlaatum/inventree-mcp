package testenv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTestEnvLockHonorsContextCancellation(t *testing.T) {
	lock, err := acquireTestEnvLock(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.release()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	second, err := acquireTestEnvLock(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, second)
}

func TestTestEnvLockCanBeReleased(t *testing.T) {
	first, err := acquireTestEnvLock(context.Background())
	require.NoError(t, err)
	require.NoError(t, first.release())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	second, err := acquireTestEnvLock(ctx)
	require.NoError(t, err)
	require.NoError(t, second.release())
}

func TestTestEnvLockRejectsPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lock, err := acquireTestEnvLock(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, lock)
}

func TestTestEnvLockRetainedWhenCleanupFails(t *testing.T) {
	lock, err := acquireTestEnvLock(context.Background())
	require.NoError(t, err)

	cleanupErr := context.Canceled
	require.ErrorIs(t, releaseTestEnvLock(lock, cleanupErr), cleanupErr)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	second, err := acquireTestEnvLock(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, second)
	require.NoError(t, lock.release())
}

func TestCleanupWithRetryRecoversBeforeLockRelease(t *testing.T) {
	var calls int
	errFirst := context.Canceled
	err := cleanupWithRetry(context.Background(), func(context.Context) error {
		calls++
		if calls == 1 {
			return errFirst
		}
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
