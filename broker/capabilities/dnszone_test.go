package capabilities_test

import (
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestDNSWriteZoneWritesAndReloads(t *testing.T) {
	fr := &exec.FakeRunner{} // named-checkzone + rndc both exit 0
	ff := fsys.NewFake()
	res, err := (capabilities.DNSWriteZone{}).Execute(appCtx(fr, ff), raw(t, map[string]any{
		"zone":       "example.test",
		"zone_file":  "$TTL 3600\n@ IN SOA ns1.example.test. admin.example.test. ( 1 2 3 4 5 )\n",
		"named_conf": "zone \"example.test\" { type master; file \"/etc/bind/zones/db.example.test\"; };\n",
	}))
	if err != nil {
		t.Fatalf("write zone: %v", err)
	}
	if res.Data["applied"] != true {
		t.Fatalf("result = %+v", res.Data)
	}
	if zf, ok := ff.Written("/etc/bind/zones/db.example.test"); !ok || zf == "" {
		t.Fatal("zone file not written")
	}
	if _, ok := ff.Written("/etc/bind/named.conf.heropanel"); !ok {
		t.Fatal("named.conf include not written")
	}
	// named-checkzone was run against the written file, then rndc reload.
	if _, ok := findCall(fr.Calls, "example.test", "/etc/bind/zones/db.example.test"); !ok {
		t.Fatalf("named-checkzone not run: %+v", fr.Calls)
	}
	if _, ok := findCall(fr.Calls, "reload"); !ok {
		t.Fatalf("rndc reload not run: %+v", fr.Calls)
	}
}

func TestDNSWriteZoneRollsBackOnInvalidZone(t *testing.T) {
	// named-checkzone fails; rndc would be exit 0 but must not matter.
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/bin/named-checkzone" {
			return exec.Result{ExitCode: 1, Stderr: []byte("bad zone")}, nil
		}
		return exec.Result{}, nil
	}}
	ff := fsys.NewFake()
	if _, err := (capabilities.DNSWriteZone{}).Execute(appCtx(fr, ff), raw(t, map[string]any{
		"zone": "example.test", "zone_file": "garbage", "named_conf": "x",
	})); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error for invalid zone, got %v", err)
	}
	// The invalid zone file was rolled back (removed — nothing existed before).
	if _, ok := ff.Written("/etc/bind/zones/db.example.test"); ok {
		t.Fatal("invalid zone file should have been rolled back")
	}
}

func TestDNSWriteZoneRejectsBadName(t *testing.T) {
	fr := &exec.FakeRunner{}
	ff := fsys.NewFake()
	if _, err := (capabilities.DNSWriteZone{}).Execute(appCtx(fr, ff), raw(t, map[string]any{
		"zone": "not a domain", "zone_file": "x", "named_conf": "y",
	})); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for bad zone name, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Fatal("nothing should run for an invalid zone name")
	}
}

func TestDNSRemoveZone(t *testing.T) {
	fr := &exec.FakeRunner{}
	ff := fsys.NewFake()
	_ = ff.WriteFile("/etc/bind/zones/db.example.test", []byte("stub"), 0o644)
	if _, err := (capabilities.DNSRemoveZone{}).Execute(appCtx(fr, ff), raw(t, map[string]any{
		"zone": "example.test", "named_conf": "",
	})); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := ff.Written("/etc/bind/zones/db.example.test"); ok {
		t.Fatal("zone file should have been removed")
	}
	if _, ok := findCall(fr.Calls, "reload"); !ok {
		t.Fatalf("rndc reload not run: %+v", fr.Calls)
	}
}
