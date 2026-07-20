//go:build !linux

package pty_test

import (
	"errors"
	"testing"

	"github.com/thisisnkp/heropanel/broker/pty"
)

// On a non-Linux developer machine the PTY layer is a stub. It must fail
// loudly rather than pretend: a Session that silently returns no output would
// make the terminal look merely broken instead of unsupported.
func TestStartIsUnsupportedOffLinux(t *testing.T) {
	s, err := pty.Start(pty.Config{Argv: []string{"/bin/sh"}})
	if !errors.Is(err, pty.ErrUnsupported) {
		t.Fatalf("Start error = %v, want ErrUnsupported", err)
	}
	if s != nil {
		t.Error("no session should be handed back when PTYs are unsupported")
	}
}
