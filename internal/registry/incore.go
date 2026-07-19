package registry

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

// InCoreProvider advertises the capabilities of a feature compiled into hpd.
//
// docs/06 §1: in-core features are "Go packages behind interfaces,
// feature-flagged" that still implement the same logical contract as satellite
// modules, "so the UI and services treat them uniformly". This is the adapter
// that makes that literally true — a compiled-in feature registers as a Provider
// just as a separate process would, and the registry cannot tell the difference.
// It is also what keeps the capability set the UI sees honest: a feature is
// advertised only when the service backing it was actually wired (its datastore
// is present), so a panel booted without a database does not offer database
// management.
//
// It has no process to health-check or shut down, so Health is always Serving
// and Shutdown is a no-op.
type InCoreProvider struct {
	slug string
	caps []string
}

// NewInCore builds a Provider for an in-core feature.
func NewInCore(slug string, capabilities ...string) *InCoreProvider {
	return &InCoreProvider{slug: slug, caps: capabilities}
}

// Handshake reports the in-core feature's identity. It stamps the current API
// version because an in-core feature is, by definition, built against this hpd.
func (p *InCoreProvider) Handshake(context.Context) proto.HandshakeResponse {
	return proto.HandshakeResponse{
		APIVersion:   proto.APIVersion,
		Slug:         p.slug,
		Version:      "in-core",
		Capabilities: p.caps,
	}
}

// Health is always Serving: an in-core feature lives or dies with hpd itself, so
// if this code is running to answer, the feature is up.
func (p *InCoreProvider) Health(context.Context) proto.HealthResponse {
	return proto.HealthResponse{State: proto.Serving}
}

// Invoke is unused for in-core features — they are called through their own Go
// service interfaces, not routed as opaque bytes. It exists only to satisfy
// Provider, and returns a clear error if something ever routes here by mistake.
func (p *InCoreProvider) Invoke(_ context.Context, capability string, _ []byte) ([]byte, error) {
	return nil, errInCoreInvoke(capability)
}

// Shutdown is a no-op: there is no separate process to drain.
func (p *InCoreProvider) Shutdown(context.Context) {}

type errInCoreInvoke string

func (e errInCoreInvoke) Error() string {
	return "registry: in-core capability " + string(e) + " is called through its Go service, not Invoke"
}
