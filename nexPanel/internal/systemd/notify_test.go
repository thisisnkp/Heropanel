package systemd

import (
	"testing"
	"time"
)

func TestSocketAddr(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", "", false},
		{"/run/systemd/notify", "/run/systemd/notify", true},
		{"@abstract", "\x00abstract", true},
		{"relative", "", false},
	}
	for _, c := range cases {
		got, ok := socketAddr(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("socketAddr(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestWatchdogInterval(t *testing.T) {
	self := 4242
	cases := []struct {
		name      string
		pid, usec string
		want      time.Duration
	}{
		{"half of usec, no pid", "", "30000000", 15 * time.Second},
		{"matching pid", "4242", "20000000", 10 * time.Second},
		{"wrong pid disables", "9999", "30000000", 0},
		{"missing usec disables", "", "", 0},
		{"zero usec disables", "", "0", 0},
		{"garbage usec disables", "", "abc", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := watchdogInterval(c.pid, c.usec, self); got != c.want {
				t.Errorf("watchdogInterval(%q,%q) = %v, want %v", c.pid, c.usec, got, c.want)
			}
		})
	}
}

// A disabled Notifier (no socket) is a safe no-op — the dev/Windows/container path.
func TestDisabledNotifierNoOp(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	n := New()
	if n.Enabled() {
		t.Fatal("expected disabled Notifier without NOTIFY_SOCKET")
	}
	if err := n.Ready(); err != nil {
		t.Errorf("Ready no-op returned error: %v", err)
	}
	if err := n.Watchdog(); err != nil {
		t.Errorf("Watchdog no-op returned error: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Errorf("Close no-op returned error: %v", err)
	}
}
