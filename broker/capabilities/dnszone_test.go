package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
	"github.com/thisisnkp/nexpanel/pkg/errx"
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
	if _, ok := ff.Written("/etc/bind/named.conf.nexpanel"); !ok {
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

func TestDNSSECStatusReadsKSKAndDS(t *testing.T) {
	ff := fsys.NewFake()
	// A KSK (flags 257) and a ZSK (256) in the key directory.
	_ = ff.WriteFile("/etc/bind/keys/Ksigned.test.+013+11111.key",
		[]byte("; comment\nsigned.test. IN DNSKEY 257 3 13 AAAAKSK==\n"), 0o644)
	_ = ff.WriteFile("/etc/bind/keys/Ksigned.test.+013+22222.key",
		[]byte("; comment\nsigned.test. IN DNSKEY 256 3 13 AAAAZSK==\n"), 0o644)
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/bin/ls" {
			return exec.Result{Stdout: []byte("Ksigned.test.+013+11111.key\nKsigned.test.+013+11111.private\nKsigned.test.+013+22222.key\n")}, nil
		}
		if cmd.Path == "/usr/bin/dnssec-dsfromkey" {
			return exec.Result{Stdout: []byte("signed.test. IN DS 11111 13 2 ABCDEF\n")}, nil
		}
		return exec.Result{}, nil
	}}
	res, err := (capabilities.DNSSECStatus{}).Execute(appCtx(fr, ff), raw(t, map[string]any{"zone": "signed.test"}))
	if err != nil {
		t.Fatalf("dnssec_status: %v", err)
	}
	if res.Data["signed"] != true {
		t.Errorf("want signed=true, got %+v", res.Data)
	}
	ds, _ := res.Data["ds"].([]string)
	if len(ds) != 1 || ds[0] != "signed.test. IN DS 11111 13 2 ABCDEF" {
		t.Errorf("DS = %v, want the KSK's DS only", ds)
	}
	dnskey, _ := res.Data["dnskey"].([]string)
	// Only the KSK (257) yields a DNSKEY entry; the ZSK is skipped.
	if len(dnskey) != 1 || !strings.Contains(dnskey[0], "257") {
		t.Errorf("DNSKEY = %v, want only the KSK", dnskey)
	}
	// dnssec-dsfromkey must be run only against the KSK, not the ZSK.
	dsCalls := 0
	for _, c := range fr.Calls {
		if c.Path == "/usr/bin/dnssec-dsfromkey" {
			dsCalls++
		}
	}
	if dsCalls != 1 {
		t.Errorf("dnssec-dsfromkey called %d times, want once (KSK only)", dsCalls)
	}
}

func TestDNSSECStatusUnsignedWhenNoKeys(t *testing.T) {
	ff := fsys.NewFake()
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/bin/ls" {
			return exec.Result{ExitCode: 2, Stderr: []byte("no such dir")}, nil
		}
		return exec.Result{}, nil
	}}
	res, err := (capabilities.DNSSECStatus{}).Execute(appCtx(fr, ff), raw(t, map[string]any{"zone": "signed.test"}))
	if err != nil {
		t.Fatalf("dnssec_status: %v", err)
	}
	if res.Data["signed"] != false {
		t.Errorf("a zone with no keys must report signed=false, got %+v", res.Data)
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
