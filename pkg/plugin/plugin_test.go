package plugin_test

import (
	"context"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/pkg/plugin"
	"github.com/thisisnkp/heropanel/pkg/proto"
)

// stubModule is a minimal Module, the kind an SDK user writes.
type stubModule struct {
	caps      []string
	shutdowns int
	configed  bool
}

func (m *stubModule) Handshake(context.Context) proto.HandshakeResponse {
	// A module does NOT set APIVersion — the SDK does. Leaving it blank here
	// proves the handler fills it.
	return proto.HandshakeResponse{Slug: "docker", Version: "1.0.0", Capabilities: m.caps}
}
func (m *stubModule) Health(context.Context) proto.HealthResponse {
	return proto.HealthResponse{State: proto.Serving}
}
func (m *stubModule) Configure(context.Context, proto.ConfigureRequest) proto.ConfigureResponse {
	m.configed = true
	return proto.ConfigureResponse{Accepted: true}
}
func (m *stubModule) Invoke(_ context.Context, capability string, _ []byte) ([]byte, error) {
	return []byte("ran:" + capability), nil
}
func (m *stubModule) Shutdown(context.Context) { m.shutdowns++ }

// The SDK stamps the API version, not the module. Otherwise a module could claim
// compatibility it was not built for.
func TestHandshakeIsStampedWithTheSDKAPIVersion(t *testing.T) {
	h := plugin.NewHandler(context.Background(), &stubModule{caps: []string{"docker.compose"}})
	hs := h.Handshake(context.Background())
	if hs.APIVersion != proto.APIVersion {
		t.Errorf("APIVersion = %q, want the SDK's %q", hs.APIVersion, proto.APIVersion)
	}
}

// Invoke is bounded by exactly what the module advertised — the manifest is the
// contract and the handler holds the module to it.
func TestInvokeRefusesAnUnadvertisedCapability(t *testing.T) {
	h := plugin.NewHandler(context.Background(), &stubModule{caps: []string{"docker.compose"}})

	if _, err := h.Invoke(context.Background(), "docker.compose", nil); err != nil {
		t.Fatalf("advertised capability was refused: %v", err)
	}
	if _, err := h.Invoke(context.Background(), "docker.secret", nil); err == nil {
		t.Fatal("an unadvertised capability was invoked")
	}
}

// After shutdown nothing serves: a late call must not reach module code.
func TestHandlerRefusesEverythingAfterShutdown(t *testing.T) {
	m := &stubModule{caps: []string{"docker.compose"}}
	h := plugin.NewHandler(context.Background(), m)
	h.Shutdown(context.Background())

	if _, err := h.Invoke(context.Background(), "docker.compose", nil); err == nil {
		t.Error("Invoke ran after shutdown")
	}
	if got := h.Health(context.Background()); got.State != proto.NotServing {
		t.Errorf("Health after shutdown = %q, want NotServing", got.State)
	}
	if got := h.Configure(context.Background(), proto.ConfigureRequest{}); got.Accepted {
		t.Error("Configure was accepted after shutdown")
	}
}

// Enable/disable churn and crash recovery can both call Shutdown; the module's
// own Shutdown must run once.
func TestShutdownIsIdempotent(t *testing.T) {
	m := &stubModule{caps: []string{"x.y"}}
	h := plugin.NewHandler(context.Background(), m)
	h.Shutdown(context.Background())
	h.Shutdown(context.Background())
	h.Shutdown(context.Background())
	if m.shutdowns != 1 {
		t.Errorf("module Shutdown ran %d times, want 1", m.shutdowns)
	}
}

// Serve returns a handler and drains it when its context is cancelled — the
// signal a SIGTERM will carry once this is a real process.
func TestServeDrainsOnContextCancel(t *testing.T) {
	m := &stubModule{caps: []string{"x.y"}}
	ctx, cancel := context.WithCancel(context.Background())
	h, err := plugin.Serve(ctx, plugin.Config{Slug: "docker", Socket: "/run/x.sock"}, m)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	cancel()
	// Cancellation drains asynchronously; poll rather than sleep a fixed time,
	// yielding between attempts so the drain goroutine actually gets scheduled.
	for i := 0; i < 200; i++ {
		if _, err := h.Invoke(context.Background(), "x.y", nil); err != nil {
			return // drained: Invoke now refused
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("Serve did not drain the module after its context was cancelled")
}

func TestServeValidatesConfig(t *testing.T) {
	if _, err := plugin.Serve(context.Background(), plugin.Config{}, &stubModule{}); err == nil {
		t.Error("Serve accepted a config with no slug")
	}
	if _, err := plugin.Serve(context.Background(), plugin.Config{Slug: "x"}, nil); err == nil {
		t.Error("Serve accepted a nil module")
	}
}
