package security

import (
	"strings"
	"testing"
)

// The rendered apt.conf enables periodic unattended upgrades, scopes to the
// security origin, and honours the reboot policy.
func TestRenderUnattendedConfig(t *testing.T) {
	cfg := RenderUnattendedConfig(UpdatesOptions{
		Enabled: true, SecurityOnly: true, AutomaticReboot: true, RebootTime: "03:30",
	})
	for _, want := range []string{
		`APT::Periodic::Unattended-Upgrade "1";`,
		`${distro_id}:${distro_codename}-security`,
		`Unattended-Upgrade::Automatic-Reboot "true";`,
		`Unattended-Upgrade::Automatic-Reboot-Time "03:30";`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q\n%s", want, cfg)
		}
	}
	// Security-only must NOT widen to -updates.
	if strings.Contains(cfg, "-updates") {
		t.Error("security-only policy included the -updates origin")
	}

	// Disabled renders the periodic keys as 0.
	off := RenderUnattendedConfig(UpdatesOptions{Enabled: false, SecurityOnly: true})
	if !strings.Contains(off, `APT::Periodic::Unattended-Upgrade "0";`) {
		t.Error("disabled policy did not turn the periodic key off")
	}
}

// A full-updates policy widens the origins but always keeps security.
func TestRenderUnattendedFullUpdates(t *testing.T) {
	cfg := RenderUnattendedConfig(UpdatesOptions{Enabled: true, SecurityOnly: false})
	if !strings.Contains(cfg, "-security") || !strings.Contains(cfg, "-updates") {
		t.Errorf("full-updates policy missing an origin:\n%s", cfg)
	}
}

// The dnf-automatic (RHEL) render maps the same options faithfully: security-only
// → upgrade_type security, disabled → apply_updates no, auto-reboot → when-needed.
func TestRenderDNFAutomaticConfig(t *testing.T) {
	sec := RenderDNFAutomaticConfig(UpdatesOptions{Enabled: true, SecurityOnly: true, AutomaticReboot: true})
	if !strings.Contains(sec, "[commands]") || !strings.Contains(sec, "upgrade_type = security") {
		t.Errorf("security dnf config wrong:\n%s", sec)
	}
	if !strings.Contains(sec, "apply_updates = yes") || !strings.Contains(sec, "reboot = when-needed") {
		t.Errorf("enabled+reboot dnf config wrong:\n%s", sec)
	}
	off := RenderDNFAutomaticConfig(UpdatesOptions{Enabled: false, SecurityOnly: false})
	if !strings.Contains(off, "apply_updates = no") || !strings.Contains(off, "upgrade_type = default") || !strings.Contains(off, "reboot = never") {
		t.Errorf("disabled/full dnf config wrong:\n%s", off)
	}
}

// The reboot time is validated (HH:MM) and normalised.
func TestUpdatesValidate(t *testing.T) {
	o := UpdatesOptions{RebootTime: "3:5"}
	if err := o.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if o.RebootTime != "03:05" {
		t.Errorf("reboot time not normalised: %q", o.RebootTime)
	}
	for _, bad := range []string{"25:00", "12:99", "noon", "12"} {
		if err := (&UpdatesOptions{RebootTime: bad}).validate(); err == nil {
			t.Errorf("bad reboot time %q accepted", bad)
		}
	}
}
