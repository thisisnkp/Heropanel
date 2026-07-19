package registry_test

import (
	"context"
	"sync"
	"testing"

	"github.com/thisisnkp/heropanel/internal/registry"
	"github.com/thisisnkp/heropanel/pkg/proto"
)

// fakeProvider is a stand-in module. It records shutdown and can answer Invoke.
type fakeProvider struct {
	slug       string
	apiVersion string
	caps       []string
	mu         sync.Mutex
	shutdowns  int
	invoked    []string
}

func newProvider(slug string, caps ...string) *fakeProvider {
	return &fakeProvider{slug: slug, apiVersion: proto.APIVersion, caps: caps}
}

func (f *fakeProvider) Handshake(context.Context) proto.HandshakeResponse {
	return proto.HandshakeResponse{APIVersion: f.apiVersion, Slug: f.slug, Capabilities: f.caps}
}
func (f *fakeProvider) Health(context.Context) proto.HealthResponse {
	return proto.HealthResponse{State: proto.Serving}
}
func (f *fakeProvider) Invoke(_ context.Context, capability string, _ []byte) ([]byte, error) {
	f.mu.Lock()
	f.invoked = append(f.invoked, capability)
	f.mu.Unlock()
	return []byte(capability + ":ok"), nil
}
func (f *fakeProvider) Shutdown(context.Context) {
	f.mu.Lock()
	f.shutdowns++
	f.mu.Unlock()
}

func TestRegisterAdvertisesCapabilities(t *testing.T) {
	r := registry.New()
	if err := r.Register(context.Background(), newProvider("docker", "docker.container", "docker.compose")); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !r.Has("docker.compose") {
		t.Error("capability not advertised after register")
	}
	if r.Has("mail.send") {
		t.Error("registry claims a capability no module provides")
	}
	if got := r.Capabilities(); len(got) != 2 || got[0] != "docker.compose" || got[1] != "docker.container" {
		t.Errorf("Capabilities = %v, want the two sorted", got)
	}
}

// The version gate is the whole reason Register does a handshake instead of
// taking a manifest: an incompatible module must never reach `enabled`.
func TestRegisterRefusesAnIncompatibleModule(t *testing.T) {
	r := registry.New()
	p := newProvider("old", "old.thing")
	p.apiVersion = "heropanel.io/v0"
	if err := r.Register(context.Background(), p); err == nil {
		t.Fatal("registered a module speaking an incompatible API version")
	}
	if r.Has("old.thing") {
		t.Error("an incompatible module's capability was advertised")
	}
}

// Two modules owning one capability makes routing ambiguous; the second is
// refused, naming the first.
func TestRegisterRefusesADuplicateCapability(t *testing.T) {
	r := registry.New()
	if err := r.Register(context.Background(), newProvider("a", "shared.cap")); err != nil {
		t.Fatalf("register a: %v", err)
	}
	err := r.Register(context.Background(), newProvider("b", "shared.cap"))
	if err == nil {
		t.Fatal("registered a second owner of the same capability")
	}
	// The first module must be untouched — the failed second registration must
	// not have stolen or dropped the capability.
	if !r.Has("shared.cap") {
		t.Error("the original owner lost its capability after a rejected duplicate")
	}
}

func TestRegisterRefusesADuplicateSlug(t *testing.T) {
	r := registry.New()
	_ = r.Register(context.Background(), newProvider("dup", "dup.one"))
	if err := r.Register(context.Background(), newProvider("dup", "dup.two")); err == nil {
		t.Fatal("registered two modules with the same slug")
	}
}

// Deregister withdraws capabilities and drains the module.
func TestDeregisterWithdrawsAndShutsDown(t *testing.T) {
	r := registry.New()
	p := newProvider("docker", "docker.compose")
	_ = r.Register(context.Background(), p)

	r.Deregister(context.Background(), "docker")

	if r.Has("docker.compose") {
		t.Error("capability still advertised after deregister")
	}
	if p.shutdowns != 1 {
		t.Errorf("module was shut down %d times, want 1", p.shutdowns)
	}
	if r.ModuleState("docker") != proto.StateNone {
		t.Error("a deregistered module is not StateNone")
	}
}

func TestInvokeRoutesToTheOwningModule(t *testing.T) {
	r := registry.New()
	docker := newProvider("docker", "docker.compose")
	mail := newProvider("mail", "mail.send")
	_ = r.Register(context.Background(), docker)
	_ = r.Register(context.Background(), mail)

	out, err := r.Invoke(context.Background(), "mail.send", nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if string(out) != "mail.send:ok" {
		t.Errorf("output = %q", out)
	}
	if len(docker.invoked) != 0 {
		t.Error("the call was routed to the wrong module")
	}
}

// A call for a capability no module provides must be a nameable error, not a
// nil-panic — the safety net for a service that forgot to gate on Has.
func TestInvokeUnknownCapabilityErrorsCleanly(t *testing.T) {
	r := registry.New()
	_, err := r.Invoke(context.Background(), "nope.gone", nil)
	if err == nil {
		t.Fatal("Invoke of an unprovided capability did not error")
	}
}

// A degraded module keeps its capabilities: whether to route to it is policy,
// not something the registry decides by hiding it.
func TestDegradedModuleKeepsItsCapabilities(t *testing.T) {
	r := registry.New()
	_ = r.Register(context.Background(), newProvider("docker", "docker.compose"))
	r.SetState("docker", proto.StateDegraded)

	if !r.Has("docker.compose") {
		t.Error("a degraded module's capability was withdrawn")
	}
	if r.ModuleState("docker") != proto.StateDegraded {
		t.Error("state not recorded")
	}
}

func TestModulesListsSortedWithState(t *testing.T) {
	r := registry.New()
	_ = r.Register(context.Background(), newProvider("mail", "mail.send"))
	_ = r.Register(context.Background(), newProvider("docker", "docker.compose"))

	mods := r.Modules()
	if len(mods) != 2 || mods[0].Slug != "docker" || mods[1].Slug != "mail" {
		t.Fatalf("Modules() = %+v, want sorted by slug", mods)
	}
	if mods[0].State != proto.StateRunning {
		t.Errorf("state = %q, want running", mods[0].State)
	}
}

// The registry is read on the request path while lifecycle ops mutate it. Run
// with -race.
func TestConcurrentReadsAndRegistration(t *testing.T) {
	r := registry.New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			slug := string(rune('a'+n)) + "mod"
			_ = r.Register(context.Background(), newProvider(slug, slug+".cap"))
		}(i)
		go func() {
			defer wg.Done()
			_ = r.Has("docker.compose")
			_ = r.Capabilities()
		}()
	}
	wg.Wait()
}
