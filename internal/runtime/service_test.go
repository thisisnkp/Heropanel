package runtime_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

type gwCall struct {
	capability string
	input      map[string]any
}

type mockGW struct {
	calls  []gwCall
	failOn string
}

func (m *mockGW) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	m.calls = append(m.calls, gwCall{capability: capability, input: in})
	if m.failOn == capability {
		return nil, errx.New(errx.KindUpstream, "boom", "simulated failure")
	}
	return map[string]any{"ok": true}, nil
}

func (m *mockGW) Health(context.Context) error { return nil }

type fakeSites struct{ ref *runtime.SiteRef }

func (f fakeSites) Resolve(_ context.Context, uid string) (*runtime.SiteRef, error) {
	r := *f.ref
	r.UID = uid
	return &r, nil
}

type fakeRepo struct {
	rec    *runtime.Record
	nextID int64
}

func (r *fakeRepo) Upsert(_ context.Context, rec *runtime.Record) error {
	if r.rec != nil {
		rec.UID = r.rec.UID
	}
	if rec.UID == "" {
		r.nextID++
		rec.UID = "rt-1"
	}
	cp := *rec
	r.rec = &cp
	return nil
}

func (r *fakeRepo) GetBySiteID(_ context.Context, _ int64) (*runtime.Record, error) {
	if r.rec == nil {
		return nil, errx.NotFound("runtime_not_found", "none")
	}
	cp := *r.rec
	return &cp, nil
}

func (r *fakeRepo) SetStatus(_ context.Context, _ int64, status string) error {
	if r.rec != nil {
		r.rec.Status = status
	}
	return nil
}

func siteRef() *runtime.SiteRef {
	return &runtime.SiteRef{ID: 1, LinuxUser: "hps1", HomeDir: "/srv/heropanel/sites/1"}
}

func validInput() runtime.SetInput {
	return runtime.SetInput{Runtime: "node", Command: "node server.js", Port: 3000, Env: map[string]string{"NODE_ENV": "production"}}
}

func TestSetRuntimeValidates(t *testing.T) {
	svc := runtime.NewService(&fakeRepo{}, fakeSites{ref: siteRef()}, &mockGW{})
	ctx := context.Background()

	for name, mut := range map[string]func(*runtime.SetInput){
		"empty command": func(in *runtime.SetInput) { in.Command = "" },
		"low port":      func(in *runtime.SetInput) { in.Port = 80 },
		"bad env key":   func(in *runtime.SetInput) { in.Env = map[string]string{"bad-key": "x"} },
		"bad runtime":   func(in *runtime.SetInput) { in.Runtime = "cobol" },
	} {
		in := validInput()
		mut(&in)
		if _, err := svc.SetRuntime(ctx, "s", in); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation, got %v", name, err)
		}
	}
}

func TestSetRuntimeAppliesUnitAndReproxies(t *testing.T) {
	repo := &fakeRepo{}
	gw := &mockGW{}
	reproxied := false
	svc := runtime.NewService(repo, fakeSites{ref: siteRef()}, gw).
		WithReproxy(func(context.Context) error { reproxied = true; return nil })

	rt, err := svc.SetRuntime(context.Background(), "s", validInput())
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if rt.Status != runtime.StatusRunning || rt.Port != 3000 {
		t.Fatalf("runtime = %+v", rt)
	}
	if !reproxied {
		t.Fatal("expected the webserver to be re-rendered")
	}

	var call *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "app.unit_apply" {
			call = &gw.calls[i]
		}
	}
	if call == nil || call.input["vhost"] != "hps1" || call.input["home"] != "/srv/heropanel/sites/1" ||
		call.input["command"] != "node server.js" || call.input["port"] != 3000 {
		t.Fatalf("app.unit_apply input = %+v", call)
	}
}

func TestControlStopSetsStatusStopped(t *testing.T) {
	repo := &fakeRepo{}
	gw := &mockGW{}
	svc := runtime.NewService(repo, fakeSites{ref: siteRef()}, gw).
		WithReproxy(func(context.Context) error { return nil })
	ctx := context.Background()

	if _, err := svc.SetRuntime(ctx, "s", validInput()); err != nil {
		t.Fatalf("set: %v", err)
	}
	rt, err := svc.Control(ctx, "s", "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if rt.Status != runtime.StatusStopped {
		t.Fatalf("status = %q, want stopped", rt.Status)
	}
	var call *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "app.unit_control" {
			call = &gw.calls[i]
		}
	}
	if call == nil || call.input["action"] != "stop" || call.input["vhost"] != "hps1" {
		t.Fatalf("app.unit_control input = %+v", call)
	}

	if _, err := svc.Control(ctx, "s", "bogus"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("bad action should be rejected, got %v", err)
	}
}

func TestRestartForSite(t *testing.T) {
	repo := &fakeRepo{}
	gw := &mockGW{}
	svc := runtime.NewService(repo, fakeSites{ref: siteRef()}, gw).
		WithReproxy(func(context.Context) error { return nil })
	ctx := context.Background()

	// No runtime configured → no-op (not an error), no broker call.
	if err := svc.RestartForSite(ctx, "s"); err != nil {
		t.Fatalf("no-op restart: %v", err)
	}
	if len(gw.calls) != 0 {
		t.Fatalf("expected no broker call when no runtime, got %+v", gw.calls)
	}

	// With a runtime → issues a restart control.
	if _, err := svc.SetRuntime(ctx, "s", validInput()); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := svc.RestartForSite(ctx, "s"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	var restarted bool
	for _, c := range gw.calls {
		if c.capability == "app.unit_control" && c.input["action"] == "restart" {
			restarted = true
		}
	}
	if !restarted {
		t.Fatalf("expected an app.unit_control restart, got %+v", gw.calls)
	}
}

func TestProxyPortAndRemove(t *testing.T) {
	repo := &fakeRepo{}
	gw := &mockGW{}
	svc := runtime.NewService(repo, fakeSites{ref: siteRef()}, gw).
		WithReproxy(func(context.Context) error { return nil })
	ctx := context.Background()

	// No runtime yet → no proxy port, and RemoveForSite is a no-op.
	if _, ok := svc.ProxyPort(ctx, 1); ok {
		t.Fatal("expected no proxy port before a runtime is set")
	}
	if err := svc.RemoveForSite(ctx, "s"); err != nil {
		t.Fatalf("remove no-op: %v", err)
	}

	if _, err := svc.SetRuntime(ctx, "s", validInput()); err != nil {
		t.Fatalf("set: %v", err)
	}
	if port, ok := svc.ProxyPort(ctx, 1); !ok || port != 3000 {
		t.Fatalf("proxy port = %d ok=%v", port, ok)
	}
	if err := svc.RemoveForSite(ctx, "s"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	var removed bool
	for _, c := range gw.calls {
		if c.capability == "app.unit_remove" && c.input["vhost"] == "hps1" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("app.unit_remove not invoked: %+v", gw.calls)
	}
}
