package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

// Handler drives a Module through its lifecycle and enforces the rules that do
// not belong in each module's own code: the API version on the handshake, the
// capability allowlist on every Invoke, and the refusal to serve after shutdown.
//
// This is the seam the transport plugs into. Today Serve calls a Handler
// in-process; a future gRPC server would decode a request and call the same
// Handler methods. Because the checks live here and not in the transport, they
// hold no matter which transport is used — a gRPC module cannot skip the
// capability check by speaking a different wire.
type Handler struct {
	mod  Module
	caps map[string]bool // the set advertised at handshake; the Invoke allowlist

	mu   sync.RWMutex
	down bool
}

// NewHandler wraps a module. It reads the module's own handshake to learn the
// capability set, so the allowlist Invoke enforces is exactly what the module
// declared — there is no second list to drift.
func NewHandler(ctx context.Context, mod Module) *Handler {
	h := &Handler{mod: mod, caps: map[string]bool{}}
	for _, c := range mod.Handshake(ctx).Capabilities {
		h.caps[c] = true
	}
	return h
}

// Handshake returns the module's identity with the SDK-supplied API version. The
// module does not set APIVersion itself — the SDK it was built against does, so
// the version reflects the contract the code actually compiled against.
func (h *Handler) Handshake(ctx context.Context) proto.HandshakeResponse {
	resp := h.mod.Handshake(ctx)
	resp.APIVersion = proto.APIVersion
	return resp
}

// Health probes the module, unless it is shutting down — a draining module is
// NotServing by definition, and asking its Health during shutdown could race
// against a backend it has already released.
func (h *Handler) Health(ctx context.Context) proto.HealthResponse {
	if h.isDown() {
		return proto.HealthResponse{State: proto.NotServing, Detail: "shutting down"}
	}
	return h.mod.Health(ctx)
}

// Configure forwards a config push. Refused once shutting down.
func (h *Handler) Configure(ctx context.Context, req proto.ConfigureRequest) proto.ConfigureResponse {
	if h.isDown() {
		return proto.ConfigureResponse{Accepted: false, Detail: "shutting down"}
	}
	return h.mod.Configure(ctx, req)
}

// Invoke dispatches a capability call after two checks the module should not
// have to repeat in every handler: the module is still serving, and the
// capability is one it actually advertised. A call for an unadvertised
// capability is refused here rather than reaching module code — the module's
// manifest is the contract, and Invoke holds it to that.
func (h *Handler) Invoke(ctx context.Context, capability string, input []byte) ([]byte, error) {
	if h.isDown() {
		return nil, fmt.Errorf("plugin: module is shutting down")
	}
	if !h.caps[capability] {
		return nil, fmt.Errorf("plugin: capability %q is not provided by this module", capability)
	}
	return h.mod.Invoke(ctx, capability, input)
}

// Shutdown drains the module once. It is idempotent: enable/disable churn and a
// crash-recovery path can both call it, and a second call must not re-enter the
// module's Shutdown.
func (h *Handler) Shutdown(ctx context.Context) {
	h.mu.Lock()
	if h.down {
		h.mu.Unlock()
		return
	}
	h.down = true
	h.mu.Unlock()
	h.mod.Shutdown(ctx)
}

func (h *Handler) isDown() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.down
}

// Serve runs a module. Today it validates the config and returns a Handler ready
// to be driven in-process (the registry can hold it directly); the network
// listener docs/06 §5 describes is the Phase 9/10 addition, and it will wrap this
// same Handler rather than replace it.
//
// The signature already takes what the networked version needs — a context whose
// cancellation means "stop serving" — so module main() functions written against
// it now do not change when the transport arrives.
func Serve(ctx context.Context, cfg Config, mod Module) (*Handler, error) {
	if cfg.Slug == "" {
		return nil, fmt.Errorf("plugin: Serve needs a module slug")
	}
	if mod == nil {
		return nil, fmt.Errorf("plugin: Serve needs a module implementation")
	}
	h := NewHandler(ctx, mod)
	// When the context is cancelled, drain — the same signal a SIGTERM would
	// carry once this is a real process.
	go func() {
		<-ctx.Done()
		h.Shutdown(context.Background())
	}()
	return h, nil
}
