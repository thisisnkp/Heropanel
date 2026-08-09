// Package capability defines the broker's privileged-operation abstraction.
//
// Each Capability is a single, named, allowlisted privileged action. There is
// deliberately no generic "run command" capability: adding a capability is a
// code change subject to review, not a configuration toggle. The broker
// orchestrator (package broker) authorizes against policy, audits, then calls
// Execute. See docs/05-security-architecture.md and docs/06-plugin-architecture.md.
package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
	"github.com/thisisnkp/nexpanel/broker/policy"
)

// Actor identifies who requested a privileged action (propagated from npd for
// correlation and audit). It never confers authority by itself — the broker
// authorizes against policy.
type Actor struct {
	UserID        string
	IP            string
	CorrelationID string

	// Node is the calling node's identity, and it is the one field here the
	// broker establishes for itself: the transport fills it from the verified
	// TLS client certificate and it is never read off the wire. Everything else
	// in this struct is what the caller said about itself, which is fine for
	// correlation and worthless as evidence. Empty means the call arrived over
	// the local Unix socket, where SO_PEERCRED already proved the peer.
	Node string
}

// Context carries everything a capability needs to execute.
type Context struct {
	Ctx    context.Context
	Runner exec.Runner
	FS     fsys.FS
	Policy policy.Policy
	Actor  Actor
	Log    *slog.Logger
}

// Result is a capability's structured output.
type Result struct {
	Data map[string]any
}

// Capability is one privileged operation.
type Capability interface {
	// Name is the stable identifier, e.g. "service.restart".
	Name() string
	// Execute validates the raw JSON input and performs the operation. It must
	// treat input as untrusted and enforce all invariants (policy checks,
	// validation) before touching the system.
	Execute(c Context, input json.RawMessage) (Result, error)
}

// Registry is an immutable-after-startup set of capabilities keyed by name.
type Registry struct {
	m map[string]Capability
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{m: make(map[string]Capability)} }

// Register adds c. It panics on a duplicate name, which is a programmer error
// caught at startup.
func (r *Registry) Register(c Capability) {
	name := c.Name()
	if _, exists := r.m[name]; exists {
		panic(fmt.Sprintf("capability: duplicate registration for %q", name))
	}
	r.m[name] = c
}

// Get returns the capability for name.
func (r *Registry) Get(name string) (Capability, bool) {
	c, ok := r.m[name]
	return c, ok
}

// Names returns the registered capability names, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.m))
	for n := range r.m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
