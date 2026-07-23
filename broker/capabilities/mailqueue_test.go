package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
)

// The queue listing is raw postqueue -j output plus whether postfix answered;
// a down postfix is running=false, not an error.
func TestMailQueueListReportsRawAndRunning(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte(`{"queue_id":"A1"}` + "\n")}}
	res, err := (capabilities.MailQueueList{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Data["raw"] != `{"queue_id":"A1"}`+"\n" || res.Data["running"] != true {
		t.Errorf("data = %v", res.Data)
	}
	last, _ := fr.Last()
	if last.Path != "/usr/sbin/postqueue" || strings.Join(last.Args, " ") != "-j" {
		t.Errorf("argv = %s %v", last.Path, last.Args)
	}

	fr = &exec.FakeRunner{Result: exec.Result{ExitCode: 69}}
	res, err = (capabilities.MailQueueList{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{}))
	if err != nil || res.Data["running"] != false {
		t.Errorf("a down postfix must be running=false, not an error: %v %v", res.Data, err)
	}
}

// Deletes are explicit IDs, argv-safe, and there is no delete-ALL.
func TestMailQueueDeleteValidatesIDs(t *testing.T) {
	fr := &exec.FakeRunner{}
	res, err := (capabilities.MailQueueDelete{}).Execute(appCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"ids": []string{"B1F5D2A", "C2E6F3B"}}))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if res.Data["deleted"] != 2 || len(fr.Calls) != 2 {
		t.Errorf("deleted = %v, calls = %d", res.Data["deleted"], len(fr.Calls))
	}
	if strings.Join(fr.Calls[0].Args, " ") != "-d B1F5D2A" {
		t.Errorf("argv = %v", fr.Calls[0].Args)
	}

	for _, bad := range [][]string{{"ALL"}, {"all"}, {"../etc"}, {"x y"}, {}} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.MailQueueDelete{}).Execute(appCtx(fr, fsys.NewFake()),
			raw(t, map[string]any{"ids": bad})); err == nil {
			t.Errorf("ids %v were accepted", bad)
		}
		if len(fr.Calls) != 0 {
			t.Error("postsuper ran for refused input")
		}
	}
}

// Quota parsing: the STORAGE row in doveadm's table, "-" = unlimited, a
// mailbox with no state yet = known:false.
func TestMailQuotaParsesDoveadm(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		user := cmd.Args[len(cmd.Args)-1]
		if user == "fresh@example.com" {
			return exec.Result{ExitCode: 75}, nil // doveadm: no such user state
		}
		return exec.Result{Stdout: []byte(
			"Quota name Type    Value Limit  %\n" +
				"User quota STORAGE   102  1024  9\n" +
				"User quota MESSAGE     3     -  0\n")}, nil
	}}
	res, err := (capabilities.MailQuota{}).Execute(appCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"addresses": []string{"info@example.com", "fresh@example.com"}}))
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	quotas := res.Data["quotas"].(map[string]any)
	info := quotas["info@example.com"].(map[string]any)
	if info["known"] != true || info["used_kb"] != int64(102) || info["limit_kb"] != int64(1024) {
		t.Errorf("info quota = %v", info)
	}
	fresh := quotas["fresh@example.com"].(map[string]any)
	if fresh["known"] != false {
		t.Errorf("fresh quota = %v (never-delivered mailbox must be known=false)", fresh)
	}

	if _, err := (capabilities.MailQuota{}).Execute(appCtx(&exec.FakeRunner{}, fsys.NewFake()),
		raw(t, map[string]any{"addresses": []string{"bad address@x"}})); err == nil {
		t.Error("an argv-unsafe address was accepted")
	}
}
