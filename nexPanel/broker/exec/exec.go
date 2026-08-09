// Package exec provides the broker's safe command execution primitive.
//
// SECURITY: commands are always executed as an argument array against an
// absolute binary path — never through a shell. There is deliberately no way to
// pass a shell string, so user-derived input cannot be interpreted as shell
// syntax (no command injection surface). See docs/05-security-architecture.md.
package exec

import (
	"bytes"
	"context"
	"errors"
	osexec "os/exec"
	"path"
	"time"
)

// Command is a single privileged execution request.
type Command struct {
	// Path is the absolute path to the binary (e.g. "/usr/bin/systemctl").
	Path string
	// Args are the arguments, excluding the program name. Passed as an array.
	Args []string
	// Env is the explicit environment. A nil Env runs with an empty environment
	// (the safest default); callers opt in to variables they need.
	Env []string
	// Stdin is optional data piped to the process.
	Stdin []byte
	// Timeout bounds execution. Zero means no explicit timeout (the caller's
	// context still applies).
	Timeout time.Duration
}

// Result captures the outcome of a Command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner executes commands. Implementations: OSRunner (real) and, in tests,
// FakeRunner.
type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

// ErrNotAbsolute is returned when a Command.Path is not an absolute Unix path.
var ErrNotAbsolute = errors.New("exec: command path must be absolute")

// OSRunner executes commands against the real OS.
type OSRunner struct{}

// Run implements Runner. It refuses relative binary paths and never invokes a
// shell.
func (OSRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	// Use path (Unix semantics) so validation is correct for the Linux target
	// regardless of the build OS.
	if !path.IsAbs(cmd.Path) {
		return Result{}, ErrNotAbsolute
	}

	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}

	c := osexec.CommandContext(ctx, cmd.Path, cmd.Args...)
	c.Env = cmd.Env // nil => empty environment
	if len(cmd.Stdin) > 0 {
		c.Stdin = bytes.NewReader(cmd.Stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}

	var exitErr *osexec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		return res, nil // non-zero exit is reported via ExitCode, not a Go error
	default:
		return res, err // failed to start, timeout, etc.
	}
}
