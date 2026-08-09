package capabilities_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
	"github.com/thisisnkp/nexpanel/broker/policy"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

func sliceCtx(r exec.Runner, f *fsys.Fake) capability.Context {
	return capability.Context{Ctx: context.Background(), Runner: r, Policy: policy.Default(), FS: f}
}

func TestSiteSliceAppliesLimits(t *testing.T) {
	fr, f := &exec.FakeRunner{}, fsys.NewFake()
	res, err := (capabilities.SiteApplySlice{}).Execute(sliceCtx(fr, f), raw(t, map[string]any{
		"vhost": "nps1", "cpu_quota_pct": 50, "mem_limit_bytes": 536870912, "pids_max": 100,
	}))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Data["slice"] != "nexpanel-site-nps1.slice" {
		t.Fatalf("result = %+v", res.Data)
	}
	unit, ok := f.Written("/etc/systemd/system/nexpanel-site-nps1.slice")
	if !ok {
		t.Fatal("slice unit was not written")
	}
	for _, want := range []string{
		"[Slice]",
		"CPUQuota=50%",
		"MemoryMax=536870912",
		"TasksMax=100",
		// Accounting is on regardless — it is what makes per-site usage readable.
		"CPUAccounting=true",
		"MemoryAccounting=true",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("slice unit missing %q:\n%s", want, unit)
		}
	}
	// daemon-reload re-applies the attributes to the live cgroup.
	if _, ok := findCall(fr.Calls, "daemon-reload"); !ok {
		t.Fatalf("systemd was not reloaded; calls=%+v", fr.Calls)
	}
}

// An unset limit must be *omitted*, not written as 0: `MemoryMax=0` means "no
// memory at all", which would make every unlimited site fail to start.
func TestSiteSliceOmitsUnsetLimits(t *testing.T) {
	fr, f := &exec.FakeRunner{}, fsys.NewFake()
	if _, err := (capabilities.SiteApplySlice{}).Execute(sliceCtx(fr, f), raw(t, map[string]any{
		"vhost": "nps1",
	})); err != nil {
		t.Fatalf("apply: %v", err)
	}
	unit, _ := f.Written("/etc/systemd/system/nexpanel-site-nps1.slice")
	for _, unwanted := range []string{"CPUQuota=", "MemoryMax=", "TasksMax="} {
		if strings.Contains(unit, unwanted) {
			t.Fatalf("an unlimited site got %q:\n%s", unwanted, unit)
		}
	}
	// But it still exists, with accounting — the cgroup has to be there before
	// anything can be placed in it.
	if !strings.Contains(unit, "MemoryAccounting=true") {
		t.Fatalf("slice has no accounting:\n%s", unit)
	}
}

// `-` is systemd's slice *hierarchy separator*: an unescaped `my-site` would
// nest as nexpanel/site/my/site — a different cgroup than intended, and one
// that collides with a site actually called `my`.
func TestSliceNameEscapesTheHierarchySeparator(t *testing.T) {
	if got := capabilities.SiteSliceName("nps1"); got != "nexpanel-site-nps1.slice" {
		t.Fatalf("plain vhost = %q", got)
	}
	if got := capabilities.SiteSliceName("my-site"); got != `nexpanel-site-my\x2dsite.slice` {
		t.Fatalf("dashed vhost = %q, want the dash escaped", got)
	}
	if got := capabilities.SiteSliceName("a.b"); got != `nexpanel-site-a\x2eb.slice` {
		t.Fatalf("dotted vhost = %q, want the dot escaped", got)
	}
	// Underscores and digits are legal literals and must survive as-is.
	if got := capabilities.SiteSliceName("site_2"); got != "nexpanel-site-site_2.slice" {
		t.Fatalf("underscore vhost = %q", got)
	}
}

func TestSiteSliceValidatesLimits(t *testing.T) {
	cases := map[string]map[string]any{
		"bad vhost":       {"vhost": "../etc"},
		"negative cpu":    {"vhost": "nps1", "cpu_quota_pct": -1},
		"absurd cpu":      {"vhost": "nps1", "cpu_quota_pct": 640000},
		"negative memory": {"vhost": "nps1", "mem_limit_bytes": -1},
		// A ceiling this low cannot start any real process; it would look like a
		// mysterious instant crash rather than a limit.
		"unstartable memory": {"vhost": "nps1", "mem_limit_bytes": 1024},
		"negative pids":      {"vhost": "nps1", "pids_max": -5},
	}
	for name, in := range cases {
		fr, f := &exec.FakeRunner{}, fsys.NewFake()
		if _, err := (capabilities.SiteApplySlice{}).Execute(sliceCtx(fr, f), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation, got %v", name, err)
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("%s: commands ran for invalid input", name)
		}
	}
}

func TestSiteRemoveSliceIsIdempotent(t *testing.T) {
	fr, f := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		// systemctl stop fails for a slice that was never there; that is fine.
		return exec.Result{ExitCode: 5}, nil
	}}, fsys.NewFake()

	res, err := (capabilities.SiteRemoveSlice{}).Execute(sliceCtx(fr, f), raw(t, map[string]any{"vhost": "nps1"}))
	if err != nil {
		t.Fatalf("removing an absent slice should not error: %v", err)
	}
	if res.Data["removed"] != true {
		t.Fatalf("result = %+v", res.Data)
	}
	if _, ok := findCall(fr.Calls, "stop", "nexpanel-site-nps1.slice"); !ok {
		t.Fatalf("slice was not stopped; calls=%+v", fr.Calls)
	}
	if _, ok := findCall(fr.Calls, "daemon-reload"); !ok {
		t.Fatalf("systemd was not reloaded; calls=%+v", fr.Calls)
	}
}

// The whole point of the slice: the app has to actually be *in* it.
func TestAppUnitIsPlacedInItsSiteSlice(t *testing.T) {
	fr, f := &exec.FakeRunner{}, fsys.NewFake()
	if _, err := (capabilities.AppUnitApply{}).Execute(
		capability.Context{Ctx: context.Background(), Runner: fr, Policy: policy.Default(), FS: f},
		raw(t, map[string]any{
			"vhost": "nps1", "username": "nps1", "home": "/srv/nexpanel/sites/1",
			"command": "node server.js", "port": 3000,
		})); err != nil {
		t.Fatalf("apply: %v", err)
	}
	unit, ok := f.Written("/etc/systemd/system/nexpanel-app-nps1.service")
	if !ok {
		t.Fatal("unit was not written")
	}
	// Without this the unit lands in system.slice and the site's limits bound
	// nothing at all.
	if !strings.Contains(unit, "Slice=nexpanel-site-nps1.slice") {
		t.Fatalf("app unit is not in its site slice:\n%s", unit)
	}
}
