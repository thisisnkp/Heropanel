package broker

import (
	"context"
	"os"
	"path"

	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/pty"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The interactive terminal is the one capability that cannot be a one-shot
// exec: it is a long-lived, bidirectional byte stream. It therefore does not go
// through the capability registry (whose contract is request → result), but it
// goes through the *same* authorization and accountability path as everything
// else — deny-by-default policy, then an audit intent before anything privileged
// happens, then an outcome. Nothing about being a stream weakens the boundary.

// CapTerminalOpen is the policy name gating interactive sessions.
const CapTerminalOpen = "terminal.open"

const (
	runuserPath = "/usr/sbin/runuser"
	bashPath    = "/bin/bash"
	shPath      = "/bin/sh"
)

// TerminalRequest asks for an interactive shell as a site's Linux user.
type TerminalRequest struct {
	// Username is the site's Linux user. The shell runs as this account — never
	// as root — which is what bounds what the session can reach.
	Username string
	// Root is the site home. It must be inside a policy path root.
	Root string
	// Cwd is the starting directory, relative to Root and clamped under it.
	Cwd string
	// Cols and Rows are the initial window size.
	Cols, Rows uint16

	Actor capability.Actor
}

// OpenTerminal authorizes, audits, and starts a PTY-backed login shell running
// as the requested site user, with its working directory clamped under the site
// root. The caller owns the returned session and must Close it.
func (b *Broker) OpenTerminal(_ context.Context, req TerminalRequest) (*pty.Session, error) {
	ar := Request{Capability: CapTerminalOpen, Actor: req.Actor}

	// 1. Deny by default.
	if !b.pol.CapabilityEnabled(CapTerminalOpen) {
		b.record(audit.OutcomeDenied, ar, "capability disabled by policy")
		return nil, errx.Forbidden("capability_disabled", "Interactive terminals are not enabled by policy.")
	}

	// 2. Validate the identity we are about to become, and where it starts.
	if err := capability.ValidateUsername(req.Username); err != nil {
		b.record(audit.OutcomeDenied, ar, "invalid username")
		return nil, err
	}
	// Refusing root here is belt-and-braces: policy path roots already keep the
	// session inside /srv/heropanel/sites, but a terminal is the one place where
	// "which user" is the entire security question, so it is stated outright.
	if req.Username == "root" {
		b.record(audit.OutcomeDenied, ar, "root terminal refused")
		return nil, errx.Forbidden("root_terminal_refused", "A terminal may not be opened as root.")
	}
	dir, err := capability.ConfinedPath(req.Root, req.Cwd, b.pol)
	if err != nil {
		b.record(audit.OutcomeDenied, ar, "path not allowed")
		return nil, err
	}
	// The clamp can legitimately land on a path that does not exist — "../../etc"
	// resolves to <site-root>/etc, which usually is not there. Starting in the
	// site home is the useful answer; failing the whole session because a
	// requested directory is missing is not. Still confined either way.
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		dir = path.Clean(req.Root)
	}

	// 3. Record intent before anything privileged happens.
	b.record(audit.OutcomeIntent, ar, req.Username)

	// 4. Start the shell as the site user. No shell string is ever built or
	// interpolated: this is a fixed argv, and runuser drops privileges before
	// the shell is exec'd.
	sess, err := pty.Start(pty.Config{
		Argv: []string{runuserPath, "-u", req.Username, "--", loginShell(), "-l"},
		Dir:  dir,
		Env:  terminalEnv(),
		Cols: req.Cols,
		Rows: req.Rows,
	})
	if err != nil {
		b.log.Error("terminal start failed", "err", err, "user", req.Username)
		b.record(audit.OutcomeFailure, ar, "start_failed")
		return nil, errx.Wrap(err, errx.KindInternal, "terminal_start_failed",
			"Could not start the terminal session.")
	}

	b.record(audit.OutcomeSuccess, ar, req.Username)
	return sess, nil
}

// loginShell prefers bash and falls back to sh on minimal images.
func loginShell() string {
	if _, err := os.Stat(bashPath); err == nil {
		return bashPath
	}
	return shPath
}

// terminalEnv is the environment handed to the session. It is deliberately
// minimal: the login shell derives HOME, USER, and the rest from the account's
// passwd entry, so nothing about the *panel's* environment leaks into a
// customer's shell.
func terminalEnv() []string {
	return []string{
		"TERM=xterm-256color",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"LANG=C.UTF-8",
	}
}
