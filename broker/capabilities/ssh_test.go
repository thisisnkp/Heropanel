package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

// ssh.harden writes the drop-in, config-tests it with `sshd -t`, then reloads.
func TestSSHHardenWritesTestsReloads(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	res, err := (capabilities.SSHHarden{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"config": "Port 2222\nPasswordAuthentication no\n",
	}))
	if err != nil {
		t.Fatalf("ssh.harden: %v", err)
	}
	if res.Data["hardened"] != true {
		t.Error("harden did not report success")
	}
	got, ok := fs.Written("/etc/ssh/sshd_config.d/50-nexpanel.conf")
	if !ok || !strings.Contains(got, "Port 2222") {
		t.Errorf("drop-in not written: %q", got)
	}
	var tested, reloaded bool
	for _, call := range fr.Calls {
		if call.Path == "/usr/sbin/sshd" && strings.Join(call.Args, " ") == "-t" {
			tested = true
		}
		if call.Path == "/usr/bin/systemctl" && call.Args[0] == "reload" {
			reloaded = true
		}
	}
	if !tested {
		t.Error("sshd -t config test did not run")
	}
	if !reloaded {
		t.Error("sshd was not reloaded")
	}
}

// A config sshd rejects (sshd -t non-zero) is rolled back to the prior drop-in.
func TestSSHHardenRollsBackOnBadConfig(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/sbin/sshd" {
			return exec.Result{ExitCode: 255}, nil // sshd -t rejects
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/etc/ssh/sshd_config.d/50-nexpanel.conf", []byte("Port 22\n"), 0o644)

	_, err := (capabilities.SSHHarden{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"config": "Port totally-invalid\n",
	}))
	if err == nil {
		t.Fatal("a config sshd rejected reported success")
	}
	if got, _ := fs.Written("/etc/ssh/sshd_config.d/50-nexpanel.conf"); got != "Port 22\n" {
		t.Errorf("drop-in was not rolled back: %q", got)
	}
	// The bad config must never reach a reload.
	for _, call := range fr.Calls {
		if call.Path == "/usr/bin/systemctl" && call.Args[0] == "reload" {
			t.Error("sshd was reloaded despite a rejected config")
		}
	}
}

// Empty or NUL-bearing config is refused before anything is written.
func TestSSHHardenRejectsBadInput(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	for _, bad := range []string{"", "Port 22\x00"} {
		if _, err := (capabilities.SSHHarden{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
			"config": bad,
		})); err == nil {
			t.Errorf("bad config %q was accepted", bad)
		}
	}
	if len(fr.Calls) != 0 {
		t.Error("sshd ran for rejected input")
	}
}
