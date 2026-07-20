//go:build linux

package pty_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/broker/pty"
)

// The PTY layer is what turns "run a command" into "host a shell": a controlling
// terminal, a window size, and a process *group* that dies with the session.
// The Linux e2e proves it end to end as a real site user; these prove the
// mechanics with no privileges, so a change to the ioctls fails in CI rather
// than in a customer's browser.

// readUntil reads from the session until want appears or the deadline passes.
func readUntil(t *testing.T, s *pty.Session, want string, timeout time.Duration) string {
	t.Helper()
	var got bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := s.Read(buf)
			if n > 0 {
				got.Write(buf[:n])
				if strings.Contains(got.String(), want) {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %q; got %q", want, got.String())
	}
	return got.String()
}

func TestStartRejectsEmptyArgv(t *testing.T) {
	if _, err := pty.Start(pty.Config{}); err == nil {
		t.Fatal("an empty argv must be refused rather than exec'ing something ambient")
	}
}

func TestSessionRoundTripsInputAndOutput(t *testing.T) {
	s, err := pty.Start(pty.Config{
		Argv: []string{"/bin/cat"},
		Dir:  "/tmp",
		Env:  []string{"TERM=xterm-256color", "PATH=/usr/bin:/bin"},
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Write([]byte("hello terminal\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A PTY echoes input by default, so the text comes back even from `cat`.
	if out := readUntil(t, s, "hello terminal", 5*time.Second); !strings.Contains(out, "hello terminal") {
		t.Errorf("output = %q, want the written line", out)
	}
}

func TestSessionReportsTheWindowSizeItWasGiven(t *testing.T) {
	// The shell learns its width from the PTY, so `stty size` reflects whatever
	// the ioctl set. Getting this wrong means every prompt wraps in the wrong
	// place, which is the most visible way a web terminal can look broken.
	s, err := pty.Start(pty.Config{
		Argv: []string{"/bin/sh", "-c", "stty size"},
		Dir:  "/tmp",
		Env:  []string{"PATH=/usr/bin:/bin"},
		Cols: 120, Rows: 40,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Close() }()

	if out := readUntil(t, s, "40 120", 5*time.Second); !strings.Contains(out, "40 120") {
		t.Errorf("stty size = %q, want \"40 120\"", out)
	}
}

func TestResizeUpdatesTheWindow(t *testing.T) {
	s, err := pty.Start(pty.Config{
		Argv: []string{"/bin/sh"},
		Dir:  "/tmp",
		Env:  []string{"PATH=/usr/bin:/bin", "PS1="},
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Resize(132, 50); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if _, err := s.Write([]byte("stty size\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if out := readUntil(t, s, "50 132", 5*time.Second); !strings.Contains(out, "50 132") {
		t.Errorf("after resize stty size = %q, want \"50 132\"", out)
	}
}

func TestWaitReturnsTheChildExitCode(t *testing.T) {
	s, err := pty.Start(pty.Config{
		Argv: []string{"/bin/sh", "-c", "exit 7"},
		Dir:  "/tmp",
		Env:  []string{"PATH=/usr/bin:/bin"},
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Drain until the master reports the session is over, which is what the
	// transport loop does before calling Wait.
	buf := make([]byte, 1024)
	for {
		if _, rerr := s.Read(buf); rerr != nil {
			if !errors.Is(rerr, os.ErrClosed) {
				t.Fatalf("read ended with %v, want the os.ErrClosed end-of-session signal", rerr)
			}
			break
		}
	}
	if code := s.Wait(); code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// The invariant that matters most: closing a session must not leave the site
// user's processes running. Close signals the whole process group, so a child
// the shell backgrounded dies with it.
func TestCloseKillsTheWholeProcessGroup(t *testing.T) {
	marker := "hp-pty-test-" + strconv.Itoa(os.Getpid())
	s, err := pty.Start(pty.Config{
		// The shell backgrounds a long sleep and then waits, so at Close there is
		// a live child that is *not* the process the broker started.
		Argv: []string{"/bin/sh", "-c", "sleep 300 & echo " + marker + " started; wait"},
		Dir:  "/tmp",
		Env:  []string{"PATH=/usr/bin:/bin"},
		Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	readUntil(t, s, marker+" started", 5*time.Second)

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// SIGKILL delivery and reaping are not instantaneous; poll briefly.
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := exec.Command("/bin/ps", "-eo", "args").Output()
		if !strings.Contains(string(out), "sleep 300") {
			return // the backgrounded child is gone
		}
		if time.Now().After(deadline) {
			t.Fatal("a backgrounded child outlived the session; Close must signal the process group")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// InputVisible decides whether session recording redacts what is typed, so its
// discriminator has to be exactly right. "Is ECHO on" is *not* it: an
// interactive shell runs with ECHO off nearly all the time, because readline
// echoes each character itself so it can do line editing. Using ECHO alone
// redacted every command anyone typed — the transcripts came back as nothing but
// markers — which the live e2e caught and these pin.
func TestInputVisibleDistinguishesAPasswordPromptFromLineEditing(t *testing.T) {
	cases := []struct {
		name string
		stty string
		want bool
		why  string
	}{
		{
			name: "default cooked terminal", stty: "sane",
			want: true, why: "a plain terminal echoes what is typed",
		},
		{
			name: "password prompt", stty: "-echo",
			want: false,
			why:  "ECHO off while still canonical is exactly what read -s, sudo and getpass do",
		},
		{
			name: "readline / raw mode", stty: "-echo -icanon",
			want: true,
			why:  "the program echoes input itself; redacting here would blank every command typed at a shell",
		},
		{
			name: "raw with echo", stty: "-icanon",
			want: true, why: "still visible",
		},
	}
	for _, tc := range cases {
		s, err := pty.Start(pty.Config{
			// The shell sets the mode, then blocks so the session stays open long
			// enough to inspect the termios it left behind.
			Argv: []string{"/bin/sh", "-c", "stty " + tc.stty + "; echo READY; sleep 30"},
			Dir:  "/tmp",
			Env:  []string{"PATH=/usr/bin:/bin"},
			Cols: 80, Rows: 24,
		})
		if err != nil {
			t.Fatalf("%s: start: %v", tc.name, err)
		}
		readUntil(t, s, "READY", 5*time.Second)
		// stty writes the mode before printing READY, but the ioctl and the write
		// are not atomic from out here; a short settle avoids a flaky read.
		time.Sleep(150 * time.Millisecond)

		if got := s.InputVisible(); got != tc.want {
			t.Errorf("%s (stty %s): InputVisible = %v, want %v — %s", tc.name, tc.stty, got, tc.want, tc.why)
		}
		_ = s.Close()
	}
}
