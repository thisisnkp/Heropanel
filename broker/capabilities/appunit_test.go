package capabilities_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func appCtx(r exec.Runner, fsy fsys.FS) capability.Context {
	return capability.Context{Ctx: context.Background(), Runner: r, FS: fsy, Policy: policy.Default()}
}

func validUnitInput() map[string]any {
	return map[string]any{
		"vhost":    "hps1",
		"username": "hps1",
		"home":     "/srv/heropanel/sites/1",
		"command":  "node server.js",
		"port":     3000,
		"env":      map[string]any{"NODE_ENV": "production"},
		"runtime":  "node",
	}
}

func TestAppUnitApplyWritesHardenedUnit(t *testing.T) {
	fr := &exec.FakeRunner{}
	ff := fsys.NewFake()
	res, err := (capabilities.AppUnitApply{}).Execute(appCtx(fr, ff), raw(t, validUnitInput()))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Data["unit"] != "heropanel-app-hps1.service" {
		t.Fatalf("result = %+v", res.Data)
	}

	// The command lives in a launcher script (so no systemd/shell quoting).
	launcher, ok := ff.Written("/srv/heropanel/sites/1/.heropanel-run")
	if !ok || !strings.Contains(launcher, "exec node server.js") {
		t.Fatalf("launcher = %q", launcher)
	}

	unit, ok := ff.Written("/etc/systemd/system/heropanel-app-hps1.service")
	if !ok {
		t.Fatal("unit file not written")
	}
	for _, want := range []string{
		"User=hps1",
		"Group=hps1",
		"WorkingDirectory=/srv/heropanel/sites/1/current",
		`Environment="NODE_ENV=production"`,
		"Environment=PORT=3000",
		"ExecStart=/srv/heropanel/sites/1/.heropanel-run",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=/srv/heropanel/sites/1",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}

	// systemctl daemon-reload, enable, restart — in that order.
	if len(fr.Calls) != 3 ||
		fr.Calls[0].Args[0] != "daemon-reload" ||
		fr.Calls[1].Args[0] != "enable" || fr.Calls[1].Args[1] != "heropanel-app-hps1.service" ||
		fr.Calls[2].Args[0] != "restart" || fr.Calls[2].Args[1] != "heropanel-app-hps1.service" {
		t.Fatalf("unexpected systemctl calls: %+v", fr.Calls)
	}
}

func TestAppUnitApplyRejectsBadInput(t *testing.T) {
	cases := map[string]map[string]any{
		"bad vhost":   mutate(validUnitInput(), "vhost", "../evil"),
		"low port":    mutate(validUnitInput(), "port", 80),
		"empty cmd":   mutate(validUnitInput(), "command", ""),
		"cmd newline": mutate(validUnitInput(), "command", "node\nrm -rf /"),
		"bad env key": mutate(validUnitInput(), "env", map[string]any{"node-env": "x"}),
	}
	for name, in := range cases {
		fr := &exec.FakeRunner{}
		ff := fsys.NewFake()
		if _, err := (capabilities.AppUnitApply{}).Execute(appCtx(fr, ff), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("%s: nothing should run for invalid input", name)
		}
	}
}

func TestAppUnitApplyRejectsHomeOutsideRoot(t *testing.T) {
	fr := &exec.FakeRunner{}
	ff := fsys.NewFake()
	in := mutate(validUnitInput(), "home", "/etc")
	if _, err := (capabilities.AppUnitApply{}).Execute(appCtx(fr, ff), raw(t, in)); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("home outside root should be forbidden, got %v", err)
	}
}

func TestAppUnitControl(t *testing.T) {
	fr := &exec.FakeRunner{}
	ff := fsys.NewFake()
	if _, err := (capabilities.AppUnitControl{}).Execute(appCtx(fr, ff), raw(t, map[string]any{
		"vhost": "hps1", "action": "restart",
	})); err != nil {
		t.Fatalf("control: %v", err)
	}
	last, _ := fr.Last()
	if last.Args[0] != "restart" || last.Args[1] != "heropanel-app-hps1.service" {
		t.Fatalf("unexpected control call: %+v", last.Args)
	}

	if _, err := (capabilities.AppUnitControl{}).Execute(appCtx(&exec.FakeRunner{}, ff), raw(t, map[string]any{
		"vhost": "hps1", "action": "frobnicate",
	})); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("bad action should be rejected, got %v", err)
	}
}

func TestAppUnitRemoveIsIdempotent(t *testing.T) {
	fr := &exec.FakeRunner{}
	ff := fsys.NewFake()
	_ = ff.WriteFile("/etc/systemd/system/heropanel-app-hps1.service", []byte("stub"), 0o644)

	if _, err := (capabilities.AppUnitRemove{}).Execute(appCtx(fr, ff), raw(t, map[string]any{"vhost": "hps1"})); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := ff.Written("/etc/systemd/system/heropanel-app-hps1.service"); ok {
		t.Fatal("unit file should have been removed")
	}
	// disable --now then daemon-reload were issued.
	if _, ok := findCall(fr.Calls, "disable", "--now", "heropanel-app-hps1.service"); !ok {
		t.Fatalf("disable not issued: %+v", fr.Calls)
	}
	if _, ok := findCall(fr.Calls, "daemon-reload"); !ok {
		t.Fatalf("daemon-reload not issued: %+v", fr.Calls)
	}
}
