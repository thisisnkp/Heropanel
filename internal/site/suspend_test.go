package site_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/webserver"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeRuntime records Control calls so a suspension can be checked to actually
// stop the app, not just hide it.
type fakeRuntime struct {
	actions []string
	port    int
	failOn  string
}

func (f *fakeRuntime) ProxyPort(context.Context, int64) (int, bool) {
	return f.port, f.port > 0
}
func (f *fakeRuntime) RemoveForSite(context.Context, string) error { return nil }
func (f *fakeRuntime) Control(_ context.Context, _, action string) error {
	f.actions = append(f.actions, action)
	if f.failOn == action {
		return errx.New(errx.KindUpstream, "boom", "simulated unit failure")
	}
	return nil
}

func lastApply(t *testing.T, web *fakeApplier) []webserver.Site {
	t.Helper()
	if len(web.calls) == 0 {
		t.Fatal("the web server was never applied")
	}
	return web.calls[len(web.calls)-1]
}

func findVhost(sites []webserver.Site, vhost string) (webserver.Site, bool) {
	for _, s := range sites {
		if s.VhostName == vhost {
			return s, true
		}
	}
	return webserver.Site{}, false
}

func TestSuspendMarksTheSiteSuspended(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := svc.Suspend(context.Background(), created.UID)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if out.Status != site.StatusSuspended {
		t.Errorf("status = %q, want %q", out.Status, site.StatusSuspended)
	}
}

// The invariant that matters. OpenLiteSpeed answers an unrecognized Host with
// its *first* vhost, so dropping a suspended site from the config would hand its
// domains to another customer's site. It must stay, walled off.
func TestSuspendedSiteKeepsItsVhostAndDomainMapping(t *testing.T) {
	store, _ := newStore(t)
	web := &fakeApplier{}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: web})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Suspend(context.Background(), created.UID); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	v, ok := findVhost(lastApply(t, web), created.SystemUser)
	if !ok {
		t.Fatal("the suspended site was dropped from the web-server config; its domains would fall through to another site")
	}
	if !v.Suspended {
		t.Error("the vhost is still rendered as serving; it should be a 503 wall")
	}
	if len(v.Domains) == 0 {
		t.Error("the suspended vhost has no domains mapped; they would fall through to another site")
	}
}

// A 503 is a curtain. Behind it the app would keep running, keep its memory, and
// keep reaching its database.
func TestSuspendStopsAProxySiteApp(t *testing.T) {
	store, _ := newStore(t)
	rt := &fakeRuntime{port: 3000}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Runtime: rt})

	in := validInput()
	in.Type = site.TypeProxy
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Suspend(context.Background(), created.UID); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	if len(rt.actions) != 1 || rt.actions[0] != "stop" {
		t.Errorf("runtime actions = %v, want one \"stop\"", rt.actions)
	}
}

func TestResumeStartsAProxySiteApp(t *testing.T) {
	store, _ := newStore(t)
	rt := &fakeRuntime{port: 3000}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Runtime: rt})

	in := validInput()
	in.Type = site.TypeProxy
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Suspend(context.Background(), created.UID); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := svc.Resume(context.Background(), created.UID); err != nil {
		t.Fatalf("resume: %v", err)
	}

	if len(rt.actions) != 2 || rt.actions[1] != "start" {
		t.Errorf("runtime actions = %v, want stop then start", rt.actions)
	}
}

// A static site has no app; asking the runtime to stop one would be nonsense.
func TestSuspendDoesNotTouchTheRuntimeForAStaticSite(t *testing.T) {
	store, _ := newStore(t)
	rt := &fakeRuntime{}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Runtime: rt})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Suspend(context.Background(), created.UID); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if len(rt.actions) != 0 {
		t.Errorf("runtime actions = %v, want none for a static site", rt.actions)
	}
}

// The site is already walled off at the web server, which is what was asked for.
// Failing the whole call because a unit would not stop would leave it serving.
func TestSuspendSucceedsEvenIfTheAppWillNotStop(t *testing.T) {
	store, _ := newStore(t)
	rt := &fakeRuntime{port: 3000, failOn: "stop"}
	web := &fakeApplier{}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: web, Runtime: rt})

	in := validInput()
	in.Type = site.TypeProxy
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := svc.Suspend(context.Background(), created.UID)
	if err != nil {
		t.Fatalf("suspend returned an error even though the site was walled off: %v", err)
	}
	if out.Status != site.StatusSuspended {
		t.Errorf("status = %q, want suspended", out.Status)
	}
	v, _ := findVhost(lastApply(t, web), created.SystemUser)
	if !v.Suspended {
		t.Error("the vhost is not a 503 wall despite the suspension reporting success")
	}
}

// A site recorded as suspended while still serving is worse than a failure: the
// panel would report the problem as handled.
func TestSuspendRollsBackTheStatusIfTheWebServerRefuses(t *testing.T) {
	store, _ := newStore(t)
	web := &fakeApplier{}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: web})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	web.failNext = true
	if _, err := svc.Suspend(context.Background(), created.UID); err == nil {
		t.Fatal("suspend reported success despite the web server refusing the config")
	}

	got, err := svc.Get(context.Background(), created.UID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != site.StatusActive {
		t.Errorf("status = %q, want it rolled back to active — the site is still serving", got.Status)
	}
}

func TestSuspendIsIdempotent(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Suspend(context.Background(), created.UID); err != nil {
		t.Fatalf("first suspend: %v", err)
	}
	out, err := svc.Suspend(context.Background(), created.UID)
	if err != nil {
		t.Fatalf("second suspend: %v", err)
	}
	if out.Status != site.StatusSuspended {
		t.Errorf("status = %q, want suspended", out.Status)
	}
}

// Suspending a site that is broken would overwrite the status that says *why*.
func TestSuspendRefusesASiteThatIsNotActive(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{failOn: "site.create_dirs"}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})

	if _, err := svc.Create(context.Background(), validInput()); err == nil {
		t.Fatal("create succeeded despite the broker failing")
	}
	list, err := svc.List(context.Background(), 0, 10, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v (%d sites)", err, len(list))
	}
	if list[0].Status != site.StatusError {
		t.Fatalf("precondition: status = %q, want error", list[0].Status)
	}

	_, err = svc.Suspend(context.Background(), list[0].UID)
	if err == nil {
		t.Fatal("suspend accepted a site in status \"error\"")
	}
	if !errx.IsKind(err, errx.KindConflict) {
		t.Errorf("error kind = %v, want conflict", errx.KindOf(err))
	}
}

func TestResumeRefusesASiteThatIsNotSuspended(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Already active: resuming is a no-op, not an error.
	out, err := svc.Resume(context.Background(), created.UID)
	if err != nil {
		t.Fatalf("resume on an active site: %v", err)
	}
	if out.Status != site.StatusActive {
		t.Errorf("status = %q, want active", out.Status)
	}
}
