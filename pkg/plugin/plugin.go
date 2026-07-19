// Package plugin is the module SDK: the small surface a satellite module
// implements, and the harness that runs it. A module's author writes a Module
// and calls Serve; everything about being supervised — the handshake, health,
// config round-trip, clean shutdown — is handled here.
//
// It is a skeleton, deliberately. docs/06 §5 describes Serve as setting up a
// gRPC listener on a Unix socket; that transport is Phase 9/10. What exists now
// is the *contract* — the Module interface and the Handler that drives it — with
// an in-process Serve so the interface is real, exercised, and testable before
// any wire is chosen. When gRPC lands it becomes another caller of Handler; the
// module author's code does not change, which is the entire point of pinning the
// interface down first.
//
// The reverse channel (a module calling back into hpd for privileged actions,
// docs/06 §3) is the Core interface: the module receives one, and every
// privileged thing it can do goes through it — never the DB or broker directly.
package plugin

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

// Module is what a satellite module implements. It is small on purpose: the four
// lifecycle methods below are the whole obligation, and the SDK turns them into
// a supervised process. Capability-specific work is dispatched through Invoke.
type Module interface {
	// Handshake announces who the module is and what it provides. The SDK fills
	// APIVersion; the module supplies its slug, version, and capabilities.
	Handshake(ctx context.Context) proto.HandshakeResponse

	// Health answers a probe. A module that has lost its backend (docker daemon
	// down, say) returns Degraded with a reason rather than lying Serving —
	// docs/12 §3.1's rule, that "started" and "works" are different claims,
	// applies to modules too.
	Health(ctx context.Context) proto.HealthResponse

	// Configure applies validated config, hot. The config has already been
	// schema-checked by hpd.
	Configure(ctx context.Context, req proto.ConfigureRequest) proto.ConfigureResponse

	// Invoke handles one capability call. The capability is one the module named
	// in its Handshake; input and output are JSON, matching the broker's own
	// envelope so the two privileged surfaces feel the same.
	Invoke(ctx context.Context, capability string, input []byte) ([]byte, error)

	// Shutdown drains gracefully. After it returns, the SDK stops serving.
	Shutdown(ctx context.Context)
}

// Core is the reverse channel: what a module is allowed to ask of hpd. A module
// never touches the database, the broker, or the realtime hub directly — it
// holds a Core and goes through it, and hpd enforces the module's declared
// allowlist on every call (docs/06 §3, §8). The interface is defined from the
// module's side so a module can be tested against a fake Core with no hpd at all.
type Core interface {
	// RequestBroker asks hpd to run a privileged capability on the module's
	// behalf. hpd checks it against the module's manifest RequiresBroker before
	// forwarding to the broker; a call the module did not declare is refused.
	RequestBroker(ctx context.Context, capability string, input []byte) ([]byte, error)

	// Persist writes module-scoped state. The module cannot reach the database;
	// hpd owns it and namespaces the write to the module.
	Persist(ctx context.Context, key string, value []byte) error

	// Emit publishes an event to the realtime hub / notifications.
	Emit(ctx context.Context, channel string, payload []byte) error
}

// Config is what Serve needs to run a module: its identity and where it listens.
// Populated by the module's main() from its manifest and process arguments.
type Config struct {
	Slug   string
	Socket string // where the module listens; hpd dials in
}
