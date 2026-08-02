//go:build !windows

package systemdnotify

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/stretchr/testify/require"
)

func TestNewSendsLifecycleAndWatchdogDatagrams(t *testing.T) {
	r := require.New(t)
	tempDir, err := os.MkdirTemp("/tmp", "inventree-mcp-systemd-notify-")
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(os.RemoveAll(tempDir))
	})
	socketPath := filepath.Join(tempDir, "notify.sock")
	socket, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(socket.Close())
	})
	t.Setenv("NOTIFY_SOCKET", socketPath)
	t.Setenv("WATCHDOG_USEC", "20000")
	t.Setenv("WATCHDOG_PID", strconv.Itoa(os.Getpid()))
	notifier := New()

	r.NoError(notifier.Ready())
	r.Equal(readyState, readDatagram(t, socket))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		notifier.RunWatchdog(ctx, func(error) {})
		close(done)
	}()
	r.Equal(daemon.SdNotifyWatchdog, readDatagram(t, socket))
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		r.Fail("watchdog loop did not stop after cancellation")
	}
}

func readDatagram(t *testing.T, socket *net.UnixConn) string {
	t.Helper()
	r := require.New(t)
	r.NoError(socket.SetReadDeadline(time.Now().Add(time.Second)))
	buffer := make([]byte, 512)
	read, _, err := socket.ReadFromUnix(buffer)
	r.NoError(err)
	return string(buffer[:read])
}
