package systemdnotify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/stretchr/testify/require"
)

func TestLifecycleNotifications(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var states []string
	notifier := daemonNotifier{
		notify: func(unsetEnvironment bool, state string) (bool, error) {
			r.False(unsetEnvironment)
			states = append(states, state)
			return true, nil
		},
	}

	r.NoError(notifier.Starting())
	r.NoError(notifier.Ready())
	r.NoError(notifier.Degraded())
	r.NoError(notifier.Stopping())
	r.NoError(notifier.Fatal())
	r.Equal([]string{startingState, readyState, degradedState, stoppingState, fatalState}, states)
}

func TestNotificationErrorsAreReturned(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	wantErr := errors.New("notify failed")
	notifier := daemonNotifier{
		notify: func(bool, string) (bool, error) {
			return false, wantErr
		},
	}

	r.ErrorIs(notifier.Ready(), wantErr)
}

func TestWatchdogUsesHalfConfiguredTimeout(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	ticks := make(chan time.Time, 2)
	createdInterval := make(chan time.Duration, 1)
	stopped := make(chan struct{}, 1)
	states := make(chan string, 2)
	notifier := daemonNotifier{
		notify: func(_ bool, state string) (bool, error) {
			states <- state
			return true, nil
		},
		watchdogEnabled: func(unsetEnvironment bool) (time.Duration, error) {
			r.False(unsetEnvironment)
			return 30 * time.Second, nil
		},
		newTicker: func(interval time.Duration) ticker {
			createdInterval <- interval
			return fakeTicker{ticks: ticks, stopped: stopped}
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		notifier.RunWatchdog(ctx, func(error) {})
		close(done)
	}()

	r.Equal(15*time.Second, <-createdInterval)
	ticks <- time.Now()
	ticks <- time.Now()
	r.Equal(daemon.SdNotifyWatchdog, <-states)
	r.Equal(daemon.SdNotifyWatchdog, <-states)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		r.Fail("watchdog loop did not stop after cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		r.Fail("watchdog ticker was not stopped")
	}
}

func TestWatchdogDisabledOutsideSystemd(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	notifier := daemonNotifier{
		notify: func(bool, string) (bool, error) {
			r.Fail("disabled watchdog sent a notification")
			return false, nil
		},
		watchdogEnabled: func(bool) (time.Duration, error) {
			return 0, nil
		},
		newTicker: func(time.Duration) ticker {
			r.Fail("disabled watchdog created a ticker")
			return nil
		},
	}

	notifier.RunWatchdog(t.Context(), func(error) {
		r.Fail("disabled watchdog reported a failure")
	})
}

func TestNewIsNoOpOutsideSystemd(t *testing.T) {
	r := require.New(t)
	t.Setenv("NOTIFY_SOCKET", "")
	t.Setenv("WATCHDOG_USEC", "")
	t.Setenv("WATCHDOG_PID", "")

	notifier := New()
	r.NoError(notifier.Starting())
	r.NoError(notifier.Ready())
	r.NoError(notifier.Degraded())
	r.NoError(notifier.Stopping())
	r.NoError(notifier.Fatal())
	notifier.RunWatchdog(t.Context(), func(error) {
		r.Fail("non-systemd watchdog reported a failure")
	})
}

func TestWatchdogReportsFailuresAndStopsHeartbeats(t *testing.T) {
	t.Parallel()

	t.Run("configuration", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		wantErr := errors.New("invalid watchdog environment")
		notifier := daemonNotifier{
			watchdogEnabled: func(bool) (time.Duration, error) {
				return 0, wantErr
			},
		}
		var gotErr error
		notifier.RunWatchdog(t.Context(), func(err error) {
			gotErr = err
		})

		r.ErrorIs(gotErr, wantErr)
	})

	t.Run("heartbeat", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		wantErr := errors.New("heartbeat failed")
		ticks := make(chan time.Time, 1)
		notifier := daemonNotifier{
			notify: func(bool, string) (bool, error) {
				return false, wantErr
			},
			watchdogEnabled: func(bool) (time.Duration, error) {
				return 10 * time.Second, nil
			},
			newTicker: func(time.Duration) ticker {
				return fakeTicker{ticks: ticks, stopped: make(chan struct{}, 1)}
			},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		failures := make(chan error, 1)
		done := make(chan struct{})
		go func() {
			notifier.RunWatchdog(ctx, func(err error) {
				failures <- err
			})
			close(done)
		}()

		ticks <- time.Now()
		r.ErrorIs(<-failures, wantErr)
		select {
		case <-done:
		case <-time.After(time.Second):
			r.Fail("watchdog loop continued after a heartbeat failure")
		}
	})
}

type fakeTicker struct {
	ticks   <-chan time.Time
	stopped chan<- struct{}
}

func (t fakeTicker) Chan() <-chan time.Time {
	return t.ticks
}

func (t fakeTicker) Stop() {
	t.stopped <- struct{}{}
}
