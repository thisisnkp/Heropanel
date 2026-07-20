// Package pty allocates a pseudo-terminal and runs a process attached to it.
//
// It exists so hp-broker can host an interactive shell *as a site's Linux user*:
// a terminal needs a controlling TTY (job control, line editing, SIGWINCH), which
// a plain exec with pipes cannot provide. The implementation is a few Linux
// ioctls over /dev/ptmx rather than a third-party dependency — the broker is the
// root component, and its dependency surface is deliberately tiny (ADR-0007).
//
// Nothing here decides *who* may open a terminal or *as whom*: the caller passes
// a fully-formed argv (in practice `runuser -u <site-user> -- <shell>`), and the
// capability layer above is what validates the user and the working directory.
package pty

// Config describes a PTY-backed process to start.
type Config struct {
	// Argv is the command and its arguments. Argv[0] is an absolute path; no
	// shell is involved in building it.
	Argv []string
	// Dir is the working directory the process starts in.
	Dir string
	// Env is the process environment (nil inherits nothing useful — callers set
	// TERM and a safe PATH explicitly).
	Env []string
	// Cols and Rows are the initial window size.
	Cols, Rows uint16
}
