package site_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/webserver"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeApplier records the web-server sites it is asked to apply.
type fakeApplier struct {
	calls [][]webserver.Site
	// failNext makes the next Apply fail, standing in for a config the real
	// server refuses (lshttpd -t) — the case where a status change must not stick.
	failNext bool
}

func (f *fakeApplier) Apply(_ context.Context, sites []webserver.Site) error {
	if f.failNext {
		f.failNext = false
		return errx.New(errx.KindUpstream, "config_rejected", "simulated config test failure")
	}
	f.calls = append(f.calls, sites)
	return nil
}

// mockGateway records broker calls and can be made to fail on a chosen capability.
type mockGateway struct {
	calls  []gwCall
	failOn string
}

type gwCall struct {
	capability string
	input      map[string]any
}

func (m *mockGateway) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	m.calls = append(m.calls, gwCall{capability: capability, input: in})
	if m.failOn == capability {
		return nil, errx.New(errx.KindUpstream, "boom", "simulated broker failure")
	}
	return map[string]any{"ok": true}, nil
}

func (m *mockGateway) Health(context.Context) error { return nil }

func newStore(t *testing.T) (*repository.SiteStore, *repository.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "sites.db")
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A site requires an owner (FK). Seed one.
	if err := repository.NewUserRepository(db).Create(context.Background(),
		&repository.User{Email: "owner@example.com", Username: "owner"}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	return repository.NewSiteStore(db), db
}

func validInput() site.CreateInput {
	return site.CreateInput{Name: "Acme", PrimaryDomain: "acme.example.com", Type: site.TypeStatic, OwnerID: 1}
}

func TestCreateProvisionsAndActivates(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw})

	out, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Status != site.StatusActive {
		t.Fatalf("status = %q, want active", out.Status)
	}
	if out.SystemUser != "hps1" || out.DocumentRoot != "/srv/heropanel/sites/1/public" {
		t.Fatalf("unexpected provisioning: user=%q docroot=%q", out.SystemUser, out.DocumentRoot)
	}

	// Broker was asked to create the user, the directory tree, then the cgroup
	// slice, in order. The slice comes last of the three but before anything is
	// placed in it — the app unit names it in `Slice=`.
	if len(gw.calls) != 3 ||
		gw.calls[0].capability != "system_user.create" ||
		gw.calls[1].capability != "site.create_dirs" ||
		gw.calls[2].capability != "site.apply_slice" {
		t.Fatalf("unexpected broker calls: %+v", gw.calls)
	}
	if gw.calls[0].input["username"] != "hps1" || gw.calls[0].input["home"] != "/srv/heropanel/sites/1" {
		t.Fatalf("system_user.create input = %+v", gw.calls[0].input)
	}
	// A new site's slice has accounting but no caps: 0 means unlimited.
	sl := gw.calls[2].input
	if sl["vhost"] != "hps1" || sl["cpu_quota_pct"] != 0 || sl["pids_max"] != 0 {
		t.Fatalf("site.apply_slice input = %+v", sl)
	}
}

func TestCreateAppliesWebserver(t *testing.T) {
	store, _ := newStore(t)
	web := &fakeApplier{}
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: web})

	out, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Status != site.StatusActive {
		t.Fatalf("status = %q, want active", out.Status)
	}
	if len(web.calls) != 1 {
		t.Fatalf("webserver applied %d times, want 1", len(web.calls))
	}
	applied := web.calls[0]
	if len(applied) != 1 || applied[0].VhostName != "hps1" ||
		applied[0].DocumentRoot != "/srv/heropanel/sites/1/public" ||
		applied[0].PrimaryDomain != "acme.example.com" {
		t.Fatalf("unexpected applied vhost: %+v", applied)
	}
}

func TestCreatePHPSiteEnsuresPoolAndSelector(t *testing.T) {
	store, db := newStore(t)
	gw := &mockGateway{}
	web := &fakeApplier{}
	phpSvc := php.NewService(repository.NewPHPPoolStore(db), gw)
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: web, PHP: phpSvc})
	ctx := context.Background()

	in := validInput()
	in.Type = site.TypePHP
	in.PrimaryDomain = "php.example.com"
	out, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatalf("create php site: %v", err)
	}
	if out.Type != site.TypePHP || out.Status != site.StatusActive {
		t.Fatalf("unexpected site: %+v", out)
	}

	// The broker was asked to write an FPM pool (default version).
	var poolCall *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "php.write_pool" {
			poolCall = &gw.calls[i]
		}
	}
	if poolCall == nil || poolCall.input["version"] != php.DefaultVersion || poolCall.input["pool_name"] != "hps1" {
		t.Fatalf("expected php.write_pool for hps1 @ default version, calls=%+v", gw.calls)
	}

	// The rendered vhost points at the site's FPM socket.
	applied := web.calls[len(web.calls)-1]
	if len(applied) != 1 || !applied[0].IsPHP || applied[0].FpmSocket != "/run/heropanel/fpm/hps1.sock" {
		t.Fatalf("vhost not PHP-wired: %+v", applied)
	}

	// The selector reports and changes the version.
	pv, err := svc.GetPHP(ctx, out.UID)
	if err != nil || pv.Version != php.DefaultVersion {
		t.Fatalf("get php = %+v err=%v", pv, err)
	}
	changed, err := svc.SetPHPVersion(ctx, out.UID, "8.3")
	if err != nil || changed.Version != "8.3" {
		t.Fatalf("set php = %+v err=%v", changed, err)
	}
	if _, err := svc.SetPHPVersion(ctx, out.UID, "5.6"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("unsupported version should be rejected, got %v", err)
	}
}

func TestCreateWithoutBrokerUnavailable(t *testing.T) {
	store, db := newStore(t)
	svc := site.NewService(site.Deps{Repo: store}) // no gateway

	_, err := svc.Create(context.Background(), validInput())
	if !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
	// No site row should have been created.
	sites, _ := svc.List(context.Background(), 0, 50, 0)
	if len(sites) != 0 {
		t.Fatalf("expected no sites, got %d", len(sites))
	}
	_ = db
}

func TestCreateBrokerFailureMarksError(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{failOn: "site.create_dirs"}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw})

	if _, err := svc.Create(context.Background(), validInput()); err == nil {
		t.Fatal("expected an error when the broker fails")
	}
	sites, err := svc.List(context.Background(), 0, 50, 0)
	if err != nil || len(sites) != 1 {
		t.Fatalf("list = %d sites, err=%v; want 1", len(sites), err)
	}
	if sites[0].Status != site.StatusError {
		t.Fatalf("status = %q, want error", sites[0].Status)
	}
}

func TestCreateValidatesInput(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw})

	bad := validInput()
	bad.PrimaryDomain = "not a domain"
	if _, err := svc.Create(context.Background(), bad); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
	if len(gw.calls) != 0 {
		t.Fatal("no broker calls for invalid input")
	}
}

func TestListGetDelete(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}})
	ctx := context.Background()

	a, err := svc.Create(ctx, validInput())
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	second := validInput()
	second.Name = "Beta"
	second.PrimaryDomain = "beta.example.com"
	if _, err := svc.Create(ctx, second); err != nil {
		t.Fatalf("create b: %v", err)
	}

	sites, _ := svc.List(ctx, 0, 50, 0)
	if len(sites) != 2 {
		t.Fatalf("list = %d, want 2", len(sites))
	}

	got, err := svc.Get(ctx, a.UID)
	if err != nil || got.PrimaryDomain != "acme.example.com" {
		t.Fatalf("get = %+v err=%v", got, err)
	}

	if err := svc.Delete(ctx, a.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, a.UID); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found after delete, got %v", err)
	}
}

func TestDeleteDeprovisions(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{}
	web := &fakeApplier{}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: web})
	ctx := context.Background()

	out, err := svc.Create(ctx, validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	createCalls := len(gw.calls) // system_user.create + site.create_dirs

	if err := svc.Delete(ctx, out.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The web server was re-applied with an empty serving set (site is gone).
	last := web.calls[len(web.calls)-1]
	if len(last) != 0 {
		t.Fatalf("expected empty serving set after delete, got %d", len(last))
	}

	// The broker was asked to remove the slice, delete the user, then remove the
	// directories. The slice goes after the unit inside it is gone, so nothing is
	// left pointing at a cgroup that no longer exists.
	deprov := gw.calls[createCalls:]
	if len(deprov) != 3 ||
		deprov[0].capability != "site.remove_slice" ||
		deprov[1].capability != "system_user.delete" ||
		deprov[2].capability != "site.remove_dirs" {
		t.Fatalf("unexpected deprovision calls: %+v", deprov)
	}
	if deprov[0].input["vhost"] != "hps1" {
		t.Fatalf("site.remove_slice input = %+v", deprov[0].input)
	}
	if deprov[1].input["username"] != "hps1" {
		t.Fatalf("userdel input = %+v", deprov[1].input)
	}
	if deprov[2].input["root"] != "/srv/heropanel/sites/1" {
		t.Fatalf("remove_dirs input = %+v", deprov[2].input)
	}
}
