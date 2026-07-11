// Package broker is the privilege boundary of HeroPanel. It is the small,
// audited component that performs privileged operations on behalf of the
// unprivileged core (hpd). Invoke authorizes a request against policy, records
// an audit intent, executes the capability, then records the outcome.
//
// SECURITY INVARIANT: a compromise of hpd (the large, network-facing process)
// cannot yield arbitrary root — it can only request the fixed set of validated
// capabilities registered here. See docs/05-security-architecture.md.
package broker

import (
	"context"
	"log/slog"

	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Version is the broker binary version (overridable at build time via ldflags).
var Version = "0.0.0-dev"

// Request is a single privileged-operation request from the core.
type Request struct {
	Capability string
	Input      []byte // JSON-encoded, capability-specific
	Actor      capability.Actor
}

// Response is the result of a successful Invoke.
type Response struct {
	Data map[string]any
}

// Broker authorizes, audits, and executes privileged capabilities.
type Broker struct {
	reg    *capability.Registry
	pol    policy.Policy
	audit  *audit.Chain
	runner exec.Runner
	fs     fsys.FS
	log    *slog.Logger
}

// New constructs a Broker. The filesystem defaults to the real OS; tests may
// override it via SetFS.
func New(reg *capability.Registry, pol policy.Policy, chain *audit.Chain, runner exec.Runner, log *slog.Logger) *Broker {
	if log == nil {
		log = slog.Default()
	}
	return &Broker{reg: reg, pol: pol, audit: chain, runner: runner, fs: fsys.OS{}, log: log}
}

// SetFS overrides the filesystem (used in tests to inject a fake).
func (b *Broker) SetFS(fs fsys.FS) { b.fs = fs }

// DefaultRegistry returns a registry populated with all built-in capabilities.
func DefaultRegistry() *capability.Registry {
	reg := capability.NewRegistry()
	for _, c := range capabilities.All() {
		reg.Register(c)
	}
	return reg
}

// Capabilities lists the registered capability names.
func (b *Broker) Capabilities() []string { return b.reg.Names() }

// Invoke authorizes, audits, and runs the requested capability.
func (b *Broker) Invoke(ctx context.Context, req Request) (Response, error) {
	// 1. Deny by default: capability must be enabled by policy.
	if !b.pol.CapabilityEnabled(req.Capability) {
		b.record(audit.OutcomeDenied, req, "capability disabled by policy")
		return Response{}, errx.Forbidden("capability_disabled",
			"This capability is not enabled by policy.")
	}

	// 2. Capability must be registered.
	impl, ok := b.reg.Get(req.Capability)
	if !ok {
		b.record(audit.OutcomeDenied, req, "unknown capability")
		return Response{}, errx.NotFound("unknown_capability", "No such capability.")
	}

	// 3. Record intent before doing anything privileged.
	b.record(audit.OutcomeIntent, req, "")

	// 4. Execute. The capability re-validates all input internally.
	res, err := impl.Execute(capability.Context{
		Ctx:    ctx,
		Runner: b.runner,
		FS:     b.fs,
		Policy: b.pol,
		Actor:  req.Actor,
		Log:    b.log,
	}, req.Input)
	if err != nil {
		// Only the sanitized code/message is audited — never raw causes.
		detail := "failed"
		if e, ok := errx.As(err); ok {
			detail = e.Code
		}
		b.record(audit.OutcomeFailure, req, detail)
		return Response{}, err
	}

	b.record(audit.OutcomeSuccess, req, "")
	return Response{Data: res.Data}, nil
}

// record appends an audit entry, logging (but not failing the caller) if the
// audit sink itself errors — that condition is surfaced via logs/metrics.
func (b *Broker) record(outcome audit.Outcome, req Request, detail string) {
	if b.audit == nil {
		return
	}
	if _, err := b.audit.Append(audit.Record{
		Actor:      req.Actor.CorrelationID,
		Capability: req.Capability,
		Outcome:    outcome,
		Detail:     detail,
	}); err != nil {
		b.log.Error("audit append failed", "err", err, "capability", req.Capability, "outcome", outcome)
	}
}
