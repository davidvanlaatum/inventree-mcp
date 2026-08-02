package systemdnotify

import (
	"context"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
)

const (
	startingState = "STATUS=Starting InvenTree MCP HTTP service"
	readyState    = daemon.SdNotifyReady + "\nSTATUS=Ready to serve MCP over HTTP"
	degradedState = "STATUS=Degraded: systemd watchdog notification failed"
	stoppingState = daemon.SdNotifyStopping + "\nSTATUS=Stopping InvenTree MCP HTTP service"
	fatalState    = "STATUS=Fatal error; InvenTree MCP service exiting"
)

// Notifier isolates the application lifecycle from the systemd notification
// protocol and provides a deterministic seam for server tests.
type Notifier interface {
	Starting() error
	Ready() error
	RunWatchdog(context.Context, func(error))
	Degraded() error
	Stopping() error
	Fatal() error
}

type ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	*time.Ticker
}

func (t realTicker) Chan() <-chan time.Time {
	return t.C
}

type daemonNotifier struct {
	notify          func(bool, string) (bool, error)
	watchdogEnabled func(bool) (time.Duration, error)
	newTicker       func(time.Duration) ticker
}

// New returns a systemd notifier. Outside systemd, notification methods are
// no-ops and the watchdog loop remains disabled.
func New() Notifier {
	return &daemonNotifier{
		notify:          daemon.SdNotify,
		watchdogEnabled: daemon.SdWatchdogEnabled,
		newTicker: func(interval time.Duration) ticker {
			return realTicker{Ticker: time.NewTicker(interval)}
		},
	}
}

func (n *daemonNotifier) Starting() error {
	return n.send(startingState)
}

func (n *daemonNotifier) Ready() error {
	return n.send(readyState)
}

func (n *daemonNotifier) Degraded() error {
	return n.send(degradedState)
}

func (n *daemonNotifier) Stopping() error {
	return n.send(stoppingState)
}

func (n *daemonNotifier) Fatal() error {
	return n.send(fatalState)
}

func (n *daemonNotifier) RunWatchdog(ctx context.Context, onFailure func(error)) {
	watchdogTimeout, err := n.watchdogEnabled(false)
	if err != nil {
		onFailure(err)
		return
	}
	if watchdogTimeout <= 0 {
		return
	}

	interval := watchdogTimeout / 2
	if interval <= 0 {
		interval = time.Nanosecond
	}
	ticker := n.newTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			if err := n.send(daemon.SdNotifyWatchdog); err != nil {
				onFailure(err)
				return
			}
		}
	}
}

func (n *daemonNotifier) send(state string) error {
	_, err := n.notify(false, state)
	return err
}
