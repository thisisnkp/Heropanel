package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
)

// updates.configure writes the drop-in, validates it with `apt-config dump`, and
// enables the apt timers.
func TestUpdatesConfigureWritesAndEnables(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	// apt-config present ⇒ the Debian family branch.
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755)

	res, err := (capabilities.UpdatesConfigure{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"config":  "APT::Periodic::Unattended-Upgrade \"1\";\n",
		"enabled": true,
	}))
	if err != nil {
		t.Fatalf("updates.configure: %v", err)
	}
	if res.Data["configured"] != true {
		t.Error("configure did not report success")
	}
	if got, _ := fs.Written("/etc/apt/apt.conf.d/52nexpanel-unattended"); !strings.Contains(got, "Unattended-Upgrade") {
		t.Errorf("drop-in not written: %q", got)
	}
	var validated bool
	timers := map[string]bool{}
	for _, call := range fr.Calls {
		if call.Path == "/usr/bin/apt-config" && call.Args[0] == "dump" {
			validated = true
		}
		if call.Path == "/usr/bin/systemctl" && call.Args[0] == "enable" {
			timers[call.Args[len(call.Args)-1]] = true
		}
	}
	if !validated {
		t.Error("apt-config dump validation did not run")
	}
	if !timers["apt-daily-upgrade.timer"] {
		t.Error("the apt-daily-upgrade timer was not enabled")
	}
}

// A drop-in apt rejects (apt-config dump non-zero) is rolled back.
func TestUpdatesConfigureRollsBackOnBadConfig(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/bin/apt-config" {
			return exec.Result{ExitCode: 100}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/usr/bin/apt-config", []byte("x"), 0o755)

	_, err := (capabilities.UpdatesConfigure{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"config": "garbage {{{\n", "enabled": true,
	}))
	if err == nil {
		t.Fatal("a config apt rejected reported success")
	}
	if _, ok := fs.Written("/etc/apt/apt.conf.d/52nexpanel-unattended"); ok {
		t.Error("the rejected drop-in was left on disk")
	}
}

// On a host without apt (RHEL family), updates.configure writes the
// dnf-automatic INI and enables its timer instead.
func TestUpdatesConfigureDNFAutomatic(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake() // no apt-config ⇒ RHEL branch

	res, err := (capabilities.UpdatesConfigure{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"config":     "APT::Periodic::Unattended-Upgrade \"1\";\n",
		"dnf_config": "[commands]\nupgrade_type = security\napply_updates = yes\n",
		"enabled":    true,
	}))
	if err != nil {
		t.Fatalf("updates.configure (dnf): %v", err)
	}
	if res.Data["configured"] != true {
		t.Error("dnf configure did not report success")
	}
	if got, _ := fs.Written("/etc/dnf/automatic.conf"); !strings.Contains(got, "apply_updates = yes") {
		t.Errorf("dnf-automatic.conf not written: %q", got)
	}
	if _, ok := fs.Written("/etc/apt/apt.conf.d/52nexpanel-unattended"); ok {
		t.Error("apt drop-in written on a RHEL host")
	}
	var enabledTimer bool
	for _, call := range fr.Calls {
		if call.Path == "/usr/bin/systemctl" && call.Args[0] == "enable" && call.Args[len(call.Args)-1] == "dnf-automatic.timer" {
			enabledTimer = true
		}
	}
	if !enabledTimer {
		t.Error("the dnf-automatic timer was not enabled")
	}
}
