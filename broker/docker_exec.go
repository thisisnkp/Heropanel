package broker

import (
	"context"
	"os/exec"
	"strings"

	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/pty"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// A shell *inside* a container.
//
// This reuses the interactive terminal's machinery rather than growing a second
// one: the same PTY implementation, the same stream upgrade on the wire, the
// same audit shape. `docker exec` is simply a different argv to put behind a
// pseudo-terminal, and treating it as a new subsystem would have meant a second
// place where "is this session authorised" is answered.
//
// The authorisation is *not* the same, though, and that difference is the whole
// point of this file. An interactive terminal is bounded by which Linux user it
// runs as; a container exec is bounded by **which container**. A shell inside a
// container the panel does not manage is a shell inside someone else's
// workload — the site database, another tenant — so the ownership label is
// checked here exactly as it is for stop and remove.

// CapContainerExec is the policy name gating container shells.
const CapContainerExec = "docker.container.exec"

const dockerBinary = "/usr/bin/docker"

// ExecRequest asks for an interactive shell inside a container.
type ExecRequest struct {
	// Container is the name or id. It must carry the panel's managed label.
	Container string
	// Shell is the program to run. Only a fixed set is accepted (see execShells):
	// this is the one field that names a binary, and an arbitrary string here
	// would turn an exec into "run anything, as root, inside any container".
	Shell string
	// Cols and Rows are the initial window size.
	Cols, Rows uint16

	Actor capability.Actor
}

// execShells is the allowlist. A container image may have either; nothing else
// is offered, because the value is an argv element in a privileged command.
var execShells = map[string]bool{"/bin/bash": true, "/bin/sh": true, "/bin/ash": true}

// OpenContainerExec authorizes, audits, and starts a PTY-backed shell inside a
// container the panel manages. The caller owns the returned session.
func (b *Broker) OpenContainerExec(ctx context.Context, req ExecRequest) (*pty.Session, error) {
	ar := Request{Capability: CapContainerExec, Actor: req.Actor}

	// 1. Deny by default.
	if !b.pol.CapabilityEnabled(CapContainerExec) {
		b.record(audit.OutcomeDenied, ar, "capability disabled by policy")
		return nil, errx.Forbidden("capability_disabled", "Container shells are not enabled by policy.")
	}

	// 2. Validate the target. The pattern cannot start with "-", so a container
	// named like a flag is unrepresentable here as everywhere else.
	if err := capabilities.ValidateContainerRef(req.Container); err != nil {
		b.record(audit.OutcomeDenied, ar, "invalid container reference")
		return nil, err
	}

	shell := req.Shell
	if shell == "" {
		shell = "/bin/sh" // present in essentially every image, including alpine
	}
	if !execShells[shell] {
		b.record(audit.OutcomeDenied, ar, "shell not allowed")
		return nil, errx.Validation("invalid_shell", "That shell is not available for container sessions.")
	}

	// 3. Ownership. A shell inside a container the panel did not create is a
	// shell inside someone else's workload, and it would bypass every other
	// refusal in this module — you could simply stop the process from within.
	if err := b.containerIsManaged(ctx, req.Container); err != nil {
		b.record(audit.OutcomeDenied, ar, "container not managed")
		return nil, err
	}

	// 4. Record intent before anything privileged happens.
	b.record(audit.OutcomeIntent, ar, req.Container)

	// 5. Start the shell. Fixed argv, no shell string built anywhere.
	sess, err := pty.Start(pty.Config{
		Argv: []string{dockerBinary, "exec", "--interactive", "--tty", req.Container, shell},
		Env:  terminalEnv(),
		Cols: req.Cols,
		Rows: req.Rows,
	})
	if err != nil {
		b.log.Error("container exec failed", "err", err, "container", req.Container)
		b.record(audit.OutcomeFailure, ar, "start_failed")
		return nil, errx.Wrap(err, errx.KindInternal, "exec_start_failed",
			"Could not start a shell in the container.")
	}

	b.record(audit.OutcomeSuccess, ar, req.Container)
	return sess, nil
}

// containerIsManaged repeats the capability layer's ownership check for the
// streaming path, which does not go through the capability registry.
//
// It is a live inspect for the same reason it is there: a name is what the
// caller supplied, a label is what the daemon reports.
func (b *Broker) containerIsManaged(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, dockerBinary, "inspect",
		"--format", "{{index .Config.Labels \""+capabilities.LabelManaged+"\"}}", ref)
	out, err := cmd.Output()
	if err != nil {
		return errx.NotFound("container_not_found", "No such container.")
	}
	if strings.TrimSpace(string(out)) != "1" {
		return errx.New(errx.KindForbidden, "container_not_managed",
			"That container was not created by HeroPanel, so the panel will not open a shell in it.")
	}
	return nil
}
