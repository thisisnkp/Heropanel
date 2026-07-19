// Package registry is hpd's live view of which modules exist, what state each is
// in, and — the question everything else asks — which capabilities are currently
// available.
//
// docs/06 §6: a service checks `registry.Has("docker.compose")` before offering
// an action, and the UI receives the capability set at login and greys out
// features whose module is not installed. The registry is what makes "every
// feature is a module you can toggle" true at runtime rather than only in the
// build.
//
// It is transport-agnostic by construction. A module here is a Provider — an
// interface satisfied equally by an in-process module (a plugin.Handler) and, in
// Phase 9/10, a gRPC client to a satellite process. The registry never learns
// which; it holds Providers and reasons about capabilities. That is the same
// separation the broker made between a capability and its wire, applied to
// modules.
package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

// Provider is a module the registry can talk to, whatever it is underneath. The
// method set is deliberately the lifecycle-and-invoke subset the registry
// actually needs — not the whole plugin.Module — so both an in-process handler
// and a future gRPC stub satisfy it without either leaking its nature.
type Provider interface {
	Handshake(ctx context.Context) proto.HandshakeResponse
	Health(ctx context.Context) proto.HealthResponse
	Invoke(ctx context.Context, capability string, input []byte) ([]byte, error)
	Shutdown(ctx context.Context)
}

// entry is a registered module and its bookkeeping.
type entry struct {
	slug     string
	state    proto.State
	caps     []string
	provider Provider
}

// Registry tracks modules and the capability set they collectively provide. Safe
// for concurrent use: services read it on the request path while lifecycle
// operations mutate it.
type Registry struct {
	mu      sync.RWMutex
	modules map[string]*entry
	byCap   map[string]string // capability -> owning slug; enforces one owner
}

// New constructs an empty Registry.
func New() *Registry {
	return &Registry{modules: map[string]*entry{}, byCap: map[string]string{}}
}

// Register brings a module online: it handshakes to learn the capabilities, then
// advertises them. It refuses a module whose API version this hpd cannot speak,
// and a module claiming a capability another module already owns.
//
// The version check is here, at the boundary, and not left to each caller: an
// incompatible module must never reach the registry as `enabled`, because the
// moment it does, a service will route a call to something that cannot answer.
func (r *Registry) Register(ctx context.Context, p Provider) error {
	hs := p.Handshake(ctx)
	if !proto.Compatible(hs.APIVersion) {
		return fmt.Errorf("registry: module %q speaks %q, incompatible with %q",
			hs.Slug, hs.APIVersion, proto.APIVersion)
	}
	if hs.Slug == "" {
		return fmt.Errorf("registry: module handshake carried no slug")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.modules[hs.Slug]; exists {
		return fmt.Errorf("registry: module %q is already registered", hs.Slug)
	}
	// Two modules owning one capability would make routing ambiguous — a call to
	// "docker.compose" could go to either. Refuse the second, naming the first,
	// rather than silently letting one shadow the other.
	for _, c := range hs.Capabilities {
		if owner, taken := r.byCap[c]; taken {
			return fmt.Errorf("registry: capability %q is already provided by module %q", c, owner)
		}
	}
	for _, c := range hs.Capabilities {
		r.byCap[c] = hs.Slug
	}
	r.modules[hs.Slug] = &entry{
		slug: hs.Slug, state: proto.StateRunning, caps: hs.Capabilities, provider: p,
	}
	return nil
}

// Deregister withdraws a module: its capabilities stop being advertised and the
// provider is shut down. A feature gated on one of its capabilities degrades to
// "module not installed" at the next check — docs/06 §6's graceful degradation.
func (r *Registry) Deregister(ctx context.Context, slug string) {
	r.mu.Lock()
	e, ok := r.modules[slug]
	if !ok {
		r.mu.Unlock()
		return
	}
	for _, c := range e.caps {
		if r.byCap[c] == slug {
			delete(r.byCap, c)
		}
	}
	delete(r.modules, slug)
	r.mu.Unlock()

	// Shut down outside the lock: a module's drain may be slow, and holding the
	// registry lock through it would stall every capability check hpd makes.
	e.provider.Shutdown(ctx)
}

// Has reports whether a capability is currently available. This is the hot path
// — services call it before offering an action — so it is a map lookup under a
// read lock and nothing more.
func (r *Registry) Has(capability string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byCap[capability]
	return ok
}

// Capabilities returns the full set currently available, sorted. The UI receives
// this at login to decide which features to render.
func (r *Registry) Capabilities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byCap))
	for c := range r.byCap {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Invoke routes a capability call to the module that owns it. A call for a
// capability no module provides is a distinct, nameable error — not a nil-panic —
// so a service that failed to gate on Has first gets told why.
func (r *Registry) Invoke(ctx context.Context, capability string, input []byte) ([]byte, error) {
	r.mu.RLock()
	slug, ok := r.byCap[capability]
	var p Provider
	if ok {
		p = r.modules[slug].provider
	}
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("registry: no module provides capability %q", capability)
	}
	return p.Invoke(ctx, capability, input)
}

// ModuleState reports a module's lifecycle state, or StateNone if it is not
// registered — an absent module and an uninstalled one are the same to a caller.
func (r *Registry) ModuleState(slug string) proto.State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.modules[slug]; ok {
		return e.state
	}
	return proto.StateNone
}

// SetState records a lifecycle transition (running -> degraded on a failed
// health probe, say). It does not touch the capability advertisement: a degraded
// module still owns its capabilities, and whether to keep routing to it is the
// caller's policy, not the registry's to decide by hiding it.
func (r *Registry) SetState(slug string, state proto.State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.modules[slug]; ok {
		e.state = state
	}
}

// Modules lists the registered modules and their states, sorted by slug, for the
// UI's module manager.
func (r *Registry) Modules() []ModuleInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ModuleInfo, 0, len(r.modules))
	for _, e := range r.modules {
		caps := append([]string(nil), e.caps...)
		sort.Strings(caps)
		out = append(out, ModuleInfo{Slug: e.slug, State: e.state, Capabilities: caps})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// ModuleInfo is the registry's public view of one module.
type ModuleInfo struct {
	Slug         string      `json:"slug"`
	State        proto.State `json:"state"`
	Capabilities []string    `json:"capabilities"`
}
