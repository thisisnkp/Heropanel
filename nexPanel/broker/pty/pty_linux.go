//go:build linux

package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// Session is a running process attached to a pseudo-terminal. Read and Write
// carry the terminal's output and input; the master fd is the only handle the
// broker keeps once the child is running.
type Session struct {
	master *os.File
	cmd    *exec.Cmd
}

// Start allocates a PTY and starts cfg.Argv attached to it as the session leader
// with the PTY as its controlling terminal — which is what makes job control,
// line editing, and window-size signals work in the resulting shell.
func Start(cfg Config) (*Session, error) {
	if len(cfg.Argv) == 0 {
		return nil, errors.New("pty: empty argv")
	}

	// Open the multiplexer, then unlock and resolve its slave. O_NOCTTY keeps the
	// *broker* from accidentally acquiring this terminal as its own.
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("pty: open /dev/ptmx: %w", err)
	}
	fail := func(e error) (*Session, error) {
		_ = master.Close()
		return nil, e
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return fail(fmt.Errorf("pty: unlock slave: %w", err))
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return fail(fmt.Errorf("pty: get slave number: %w", err))
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return fail(fmt.Errorf("pty: open slave: %w", err))
	}

	// Size the window before the child starts, so its first prompt is already
	// correct rather than reflowing after the first resize.
	setSize(master, cfg.Cols, cfg.Rows)

	cmd := exec.Command(cfg.Argv[0], cfg.Argv[1:]...)
	cmd.Dir = cfg.Dir
	cmd.Env = cfg.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Setsid makes the child a new session (and process-group) leader; Setctty
	// with Ctty=0 makes fd 0 — the slave — its controlling terminal. The new
	// session is also what lets Close() signal the whole process group, so a
	// backgrounded child cannot outlive the session.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}

	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		return fail(fmt.Errorf("pty: start: %w", err))
	}
	// The child holds the slave now; the parent must drop it or the master never
	// sees EOF when the child exits.
	_ = slave.Close()

	return &Session{master: master, cmd: cmd}, nil
}

// Read returns terminal output. When the child exits, Linux reports EIO on the
// master; that is a normal end-of-session, so it is translated to io.EOF-like
// behaviour by returning (0, os.ErrClosed) which the caller treats as "done".
func (s *Session) Read(p []byte) (int, error) {
	n, err := s.master.Read(p)
	if err != nil && isTerminalEOF(err) {
		return n, os.ErrClosed
	}
	return n, err
}

// Write sends input to the terminal.
func (s *Session) Write(p []byte) (int, error) { return s.master.Write(p) }

// InputVisible reports whether what the operator types is visible to them —
// which is the same question as "is this safe to record".
//
// It is *not* simply "is ECHO on". An interactive shell spends nearly all its
// time with ECHO off: readline puts the terminal in raw mode and echoes each
// character itself so it can do line editing. Treating ECHO-off as "password"
// therefore redacts every command anyone types, which is what the live e2e
// caught — the transcripts came back as nothing but redaction markers.
//
// The actual signature of a password prompt is ECHO off *while still in
// canonical mode*: `read -s`, getpass(3), sudo and mysql -p all clear ECHO and
// leave ICANON set, because they want the kernel's line discipline to do the
// reading, just silently. readline clears both. So:
//
//	ECHO on                → visible (a plain program, cat, a shell without readline)
//	ECHO off, ICANON off   → visible (readline/vim: the program echoes it itself)
//	ECHO off, ICANON on    → hidden  (a password prompt) → redact
//
// Querying the *master* is deliberate: on Linux master and slave share the line
// discipline, so the master's termios reflects what the program on the other side
// just set. It reports visible when the state cannot be read: a recording full of
// holes is worse than one that is honest about what it holds, and the failure
// mode of the opposite default is a transcript that says nothing at all.
func (s *Session) InputVisible() bool {
	t, err := unix.IoctlGetTermios(int(s.master.Fd()), unix.TCGETS)
	if err != nil {
		return true
	}
	return t.Lflag&unix.ECHO != 0 || t.Lflag&unix.ICANON == 0
}

// Resize updates the terminal window size and raises SIGWINCH in the session.
func (s *Session) Resize(cols, rows uint16) error {
	return unix.IoctlSetWinsize(int(s.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
		Row: rows, Col: cols,
	})
}

// Wait blocks until the process exits and returns its exit code.
func (s *Session) Wait() int {
	err := s.cmd.Wait()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// Close kills the whole process group and releases the master. Signalling the
// group (not just the leader) is deliberate: a shell that spawned children must
// not leave them running as the site user after the browser tab is gone.
func (s *Session) Close() error {
	if s.cmd.Process != nil {
		_ = unix.Kill(-s.cmd.Process.Pid, unix.SIGKILL)
	}
	return s.master.Close()
}

func setSize(f *os.File, cols, rows uint16) {
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	_ = unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
}

// isTerminalEOF reports whether err is the EIO Linux returns on a master whose
// slave side has been fully closed (i.e. the child exited).
func isTerminalEOF(err error) bool {
	return errors.Is(err, unix.EIO)
}
