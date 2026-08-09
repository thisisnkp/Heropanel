package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

const liveRuleset = "table inet nexpanel {\n\tchain input { type filter hook input priority 0; policy accept; }\n}\n"

// nftRunner routes nft calls: `list ruleset` prints the live ruleset, `-f`
// records the file it was asked to load.
func nftRunner(loaded *[]string) *exec.FakeRunner {
	return &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/sbin/nft" {
			if len(cmd.Args) >= 2 && cmd.Args[0] == "list" {
				return exec.Result{Stdout: []byte(liveRuleset)}, nil
			}
			if len(cmd.Args) == 2 && cmd.Args[0] == "-f" && loaded != nil {
				*loaded = append(*loaded, cmd.Args[1])
			}
		}
		return exec.Result{ExitCode: 0}, nil
	}}
}

// Apply snapshots the live ruleset (as a flushable restore point) and then
// loads the new one; the snapshot is left pending.
func TestFirewallApplySnapshotsThenApplies(t *testing.T) {
	var loaded []string
	fr := nftRunner(&loaded)
	fs := fsys.NewFake()

	res, err := (capabilities.FirewallApply{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"ruleset": "flush ruleset\ntable inet nexpanel { chain input { policy drop; } }\n",
	}))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Data["pending"] != true {
		t.Error("apply did not leave the change pending")
	}
	snap, ok := fs.Written("/var/lib/nexpanel/firewall/rollback.nft")
	if !ok {
		t.Fatal("no rollback snapshot was written")
	}
	if !strings.HasPrefix(snap, "flush ruleset\n") || !strings.Contains(snap, "policy accept") {
		t.Errorf("snapshot is not a flushable restore of the live ruleset: %q", snap)
	}
	// The new ruleset was the last thing nft loaded.
	if len(loaded) == 0 || loaded[len(loaded)-1] != "/var/lib/nexpanel/firewall/desired.nft" {
		t.Errorf("the new ruleset was not applied: %v", loaded)
	}
}

// A second apply while one is pending is refused — the snapshot must stay the
// true pre-change baseline, not a half-changed intermediate.
func TestFirewallApplyRefusesWhilePending(t *testing.T) {
	fs := fsys.NewFake()
	_ = fs.WriteFile("/var/lib/nexpanel/firewall/rollback.nft", []byte("flush ruleset\n"), 0o600)

	_, err := (capabilities.FirewallApply{}).Execute(appCtx(nftRunner(nil), fs), raw(t, map[string]any{
		"ruleset": "flush ruleset\n",
	}))
	if err == nil {
		t.Fatal("a second apply was accepted while one was pending")
	}
}

// A ruleset nft rejects leaves nothing pending — the box is untouched.
func TestFirewallApplyCleansUpOnRejected(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/sbin/nft" && len(cmd.Args) >= 1 && cmd.Args[0] == "list" {
			return exec.Result{Stdout: []byte(liveRuleset)}, nil
		}
		if cmd.Path == "/usr/sbin/nft" && len(cmd.Args) >= 1 && cmd.Args[0] == "-f" {
			return exec.Result{ExitCode: 1, Stderr: []byte("syntax error")}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	fs := fsys.NewFake()

	if _, err := (capabilities.FirewallApply{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"ruleset": "garbage\n",
	})); err == nil {
		t.Fatal("a rejected ruleset reported success")
	}
	if _, ok := fs.Written("/var/lib/nexpanel/firewall/rollback.nft"); ok {
		// Written then removed: Fake records the last write; assert it was removed.
		if exists, _ := fs.Exists("/var/lib/nexpanel/firewall/rollback.nft"); exists {
			t.Error("a rejected apply left a pending snapshot")
		}
	}
}

// Confirm discards the snapshot; rollback restores and discards it.
func TestFirewallConfirmAndRollback(t *testing.T) {
	fs := fsys.NewFake()
	_ = fs.WriteFile("/var/lib/nexpanel/firewall/rollback.nft", []byte("flush ruleset\npolicy accept\n"), 0o600)

	res, err := (capabilities.FirewallConfirm{}).Execute(appCtx(nftRunner(nil), fs), raw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if res.Data["was_pending"] != true {
		t.Error("confirm did not see the pending change")
	}
	if exists, _ := fs.Exists("/var/lib/nexpanel/firewall/rollback.nft"); exists {
		t.Error("confirm left the snapshot behind")
	}

	// Rollback with a fresh pending snapshot restores it.
	var loaded []string
	fr := nftRunner(&loaded)
	fs2 := fsys.NewFake()
	_ = fs2.WriteFile("/var/lib/nexpanel/firewall/rollback.nft", []byte("flush ruleset\npolicy accept\n"), 0o600)
	rb, err := (capabilities.FirewallRollback{}).Execute(appCtx(fr, fs2), raw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rb.Data["rolled_back"] != true {
		t.Error("rollback did not restore the snapshot")
	}
	if len(loaded) != 1 || loaded[0] != "/var/lib/nexpanel/firewall/rollback.nft" {
		t.Errorf("rollback did not load the snapshot: %v", loaded)
	}
	if exists, _ := fs2.Exists("/var/lib/nexpanel/firewall/rollback.nft"); exists {
		t.Error("rollback left the snapshot behind")
	}

	// Rollback with nothing pending is a no-op success.
	nb, err := (capabilities.FirewallRollback{}).Execute(appCtx(nftRunner(nil), fsys.NewFake()), raw(t, map[string]any{}))
	if err != nil || nb.Data["rolled_back"] != false {
		t.Errorf("rollback with nothing pending = %v %v", nb.Data, err)
	}
}
