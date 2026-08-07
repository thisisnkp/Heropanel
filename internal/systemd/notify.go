// Package systemd implements the small slice of the sd_notify protocol hpd needs
// to be a Type=notify service with a watchdog: tell systemd when it is ready, and
// periodically pet the watchdog so a hung (not just crashed) process is killed
// and restarted. It is pure Go (no cgo, no dependency) and a safe no-op when not
// running under systemd — NOTIFY_SOCKET is unset in dev, containers, and on
// non-Linux, so the same binary runs everywhere.
package systemd

import (
	"net"
	"os"
	"strconv"
	"time"
)

// Notifier writes sd_notify datagrams to $NOTIFY_SOCKET. A zero/disabled Notifier
// (no socket) makes every call a no-op.
type Notifier struct {
	conn *net.UnixConn
}

// New connects to $NOTIFY_SOCKET, returning a disabled Notifier when it is unset
// or cannot be reached (so callers never need to branch on "am I under systemd").
func New() *Notifier {
	addr, ok := socketAddr(os.Getenv("NOTIFY_SOCKET"))
	if !ok {
		return &Notifier{}
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return &Notifier{}
	}
	return &Notifier{conn: conn}
}

// Enabled reports whether this Notifier is connected to systemd.
func (n *Notifier) Enabled() bool { return n != nil && n.conn != nil }

func (n *Notifier) notify(state string) error {
	if !n.Enabled() {
		return nil
	}
	_, err := n.conn.Write([]byte(state))
	return err
}

// Ready tells systemd the service has finished starting (required for
// Type=notify units, else systemd fails the unit after TimeoutStartSec).
func (n *Notifier) Ready() error { return n.notify("READY=1") }

// Watchdog pets the systemd watchdog. Send it on WatchdogInterval().
func (n *Notifier) Watchdog() error { return n.notify("WATCHDOG=1") }

// Close releases the socket.
func (n *Notifier) Close() error {
	if n.Enabled() {
		return n.conn.Close()
	}
	return nil
}

// socketAddr resolves a NOTIFY_SOCKET value to a dialable unixgram address. Empty
// means "not under systemd". A leading '@' marks a Linux abstract socket, which
// the net package expects as a NUL-prefixed name; a leading '/' is a path socket.
func socketAddr(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	switch v[0] {
	case '@':
		return "\x00" + v[1:], true
	case '/':
		return v, true
	default:
		return "", false
	}
}

// WatchdogInterval returns how often to pet the watchdog, derived from the
// WATCHDOG_USEC systemd sets when WatchdogSec= is configured. It returns half the
// timeout so a ping always lands inside the window; 0 disables watchdog pinging.
func WatchdogInterval() time.Duration {
	return watchdogInterval(os.Getenv("WATCHDOG_PID"), os.Getenv("WATCHDOG_USEC"), os.Getpid())
}

// watchdogInterval is the pure core of WatchdogInterval (testable). If
// WATCHDOG_PID is set it must be this process (systemd targets the main pid); an
// empty pid is tolerated for simpler setups.
func watchdogInterval(pidEnv, usecEnv string, self int) time.Duration {
	if pidEnv != "" {
		if p, err := strconv.Atoi(pidEnv); err != nil || p != self {
			return 0
		}
	}
	us, err := strconv.ParseInt(usecEnv, 10, 64)
	if err != nil || us <= 0 {
		return 0
	}
	return time.Duration(us) * time.Microsecond / 2
}
