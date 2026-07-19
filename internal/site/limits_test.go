package site_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// seedSite provisions a site and returns its uid plus how many broker calls that
// took, so a test can look at only the calls it made itself.
func seedSite(t *testing.T, svc *site.Service) string {
	t.Helper()
	out, err := svc.Create(context.Background(), site.CreateInput{
		OwnerID: 1, Name: "Acme", PrimaryDomain: "acme.test", Type: site.TypeStatic,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return out.UID
}

func TestNewSiteHasUnlimitedLimits(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	uid := seedSite(t, svc)

	// A site nobody has limited must read as unlimited, not as a not-found error.
	l, err := svc.GetLimits(context.Background(), uid)
	if err != nil {
		t.Fatalf("get limits: %v", err)
	}
	if l.CPUQuotaPct != 0 || l.MemLimitBytes != 0 || l.PidsMax != 0 {
		t.Fatalf("a fresh site should be unlimited, got %+v", l)
	}
}

func TestSetLimitsAppliesToTheSliceAndPersists(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})
	uid := seedSite(t, svc)
	before := len(gw.calls)
	ctx := context.Background()

	want := site.Limits{CPUQuotaPct: 50, MemLimitBytes: 512 << 20, PidsMax: 100}
	got, err := svc.SetLimits(ctx, uid, want)
	if err != nil {
		t.Fatalf("set limits: %v", err)
	}
	if *got != want {
		t.Fatalf("returned %+v, want %+v", *got, want)
	}

	// The kernel was told first.
	calls := gw.calls[before:]
	if len(calls) != 1 || calls[0].capability != "site.apply_slice" {
		t.Fatalf("unexpected broker calls: %+v", calls)
	}
	in := calls[0].input
	if in["vhost"] != "hps1" || in["cpu_quota_pct"] != 50 || in["pids_max"] != 100 {
		t.Fatalf("site.apply_slice input = %+v", in)
	}

	// And it survives a read.
	reread, err := svc.GetLimits(ctx, uid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if *reread != want {
		t.Fatalf("reread %+v, want %+v", *reread, want)
	}
}

// A stored limit the kernel is not enforcing is worse than no limit: the panel
// would report a cap that does not exist.
func TestSetLimitsDoesNotPersistWhenTheSliceFails(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{failOn: "site.apply_slice"}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})
	gw.failOn = "" // let provisioning succeed first
	uid := seedSite(t, svc)
	ctx := context.Background()

	gw.failOn = "site.apply_slice"
	if _, err := svc.SetLimits(ctx, uid, site.Limits{CPUQuotaPct: 50}); err == nil {
		t.Fatal("a failing slice apply should surface an error")
	}
	l, err := svc.GetLimits(ctx, uid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if l.CPUQuotaPct != 0 {
		t.Fatalf("a limit systemd rejected was recorded anyway: %+v", l)
	}
}

func TestSetLimitsValidates(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	uid := seedSite(t, svc)
	ctx := context.Background()

	for name, l := range map[string]site.Limits{
		"negative cpu":    {CPUQuotaPct: -1},
		"absurd cpu":      {CPUQuotaPct: 640000},
		"negative memory": {MemLimitBytes: -1},
		// Below a few MiB nothing can start; that presents as a mystery crash,
		// not as a limit.
		"unstartable memory": {MemLimitBytes: 4096},
		"negative pids":      {PidsMax: -1},
	} {
		if _, err := svc.SetLimits(ctx, uid, l); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation, got %v", name, err)
		}
	}
}

func TestLimitsRejectUnknownSite(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	ctx := context.Background()

	if _, err := svc.GetLimits(ctx, "nope"); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
	if _, err := svc.SetLimits(ctx, "nope", site.Limits{}); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}
