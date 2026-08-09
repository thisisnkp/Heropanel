package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

// Status runs fail2ban-client and returns its raw output plus whether the
// server answered.
func TestFail2BanStatus(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte("`- Jail list:\tsshd\n")}}
	res, err := (capabilities.Fail2BanStatus{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Data["running"] != true || !strings.Contains(res.Data["raw"].(string), "sshd") {
		t.Errorf("data = %v", res.Data)
	}
	last, _ := fr.Last()
	if last.Path != "/usr/bin/fail2ban-client" || strings.Join(last.Args, " ") != "status" {
		t.Errorf("argv = %s %v", last.Path, last.Args)
	}

	// A jail detail passes the validated jail name through.
	fr2 := &exec.FakeRunner{}
	if _, err := (capabilities.Fail2BanStatus{}).Execute(appCtx(fr2, fsys.NewFake()),
		raw(t, map[string]any{"jail": "sshd"})); err != nil {
		t.Fatalf("jail status: %v", err)
	}
	last, _ = fr2.Last()
	if strings.Join(last.Args, " ") != "status sshd" {
		t.Errorf("jail argv = %v", last.Args)
	}

	// A bad jail name is refused.
	fr3 := &exec.FakeRunner{}
	if _, err := (capabilities.Fail2BanStatus{}).Execute(appCtx(fr3, fsys.NewFake()),
		raw(t, map[string]any{"jail": "sshd; rm -rf /"})); err == nil {
		t.Error("a shell-shaped jail name was accepted")
	}
	if len(fr3.Calls) != 0 {
		t.Error("fail2ban-client ran for a bad jail name")
	}
}

// Ban and unban validate the jail and IP, and build `set <jail> <action> <ip>`.
func TestFail2BanBanUnban(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.Fail2BanBan{}).Execute(appCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"jail": "sshd", "ip": "203.0.113.4"})); err != nil {
		t.Fatalf("ban: %v", err)
	}
	last, _ := fr.Last()
	if strings.Join(last.Args, " ") != "set sshd banip 203.0.113.4" {
		t.Errorf("ban argv = %v", last.Args)
	}

	fr2 := &exec.FakeRunner{}
	if _, err := (capabilities.Fail2BanUnban{}).Execute(appCtx(fr2, fsys.NewFake()),
		raw(t, map[string]any{"jail": "sshd", "ip": "203.0.113.4"})); err != nil {
		t.Fatalf("unban: %v", err)
	}
	last, _ = fr2.Last()
	if strings.Join(last.Args, " ") != "set sshd unbanip 203.0.113.4" {
		t.Errorf("unban argv = %v", last.Args)
	}

	// A non-IP is refused before the client runs.
	for _, bad := range []map[string]any{
		{"jail": "sshd", "ip": "not-an-ip"},
		{"jail": "bad jail", "ip": "203.0.113.4"},
	} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.Fail2BanBan{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, bad)); err == nil {
			t.Errorf("bad ban input accepted: %v", bad)
		}
		if len(fr.Calls) != 0 {
			t.Error("fail2ban-client ran for refused input")
		}
	}
}
