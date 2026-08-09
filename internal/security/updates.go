package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Automatic security updates: on Debian/Ubuntu, drive `unattended-upgrades`
// through a panel-owned apt config drop-in. npd renders the apt.conf from
// validated options; the broker writes the one pinned path and enables the apt
// timers, and reads the *effective* merged config back with `apt-config dump`
// (the honest source of truth, the same way SSH uses `sshd -T`). On Rocky/Alma
// the same policy drives `dnf-automatic` (an INI config + its timer); the broker
// detects the family and applies whichever one the host uses, so the panel
// presents one auto-updates policy across both distro families.

// UpdatesOptions is the validated auto-update policy.
type UpdatesOptions struct {
	Enabled         bool   `json:"enabled"`          // run unattended upgrades at all
	SecurityOnly    bool   `json:"security_only"`    // security origin only (default true)
	AutomaticReboot bool   `json:"automatic_reboot"` // reboot if an update needs it
	RebootTime      string `json:"reboot_time"`      // "HH:MM" when AutomaticReboot
}

// DefaultUpdatesOptions enables security-only auto-updates with no auto-reboot.
func DefaultUpdatesOptions() UpdatesOptions {
	return UpdatesOptions{Enabled: true, SecurityOnly: true, AutomaticReboot: false, RebootTime: "02:00"}
}

// Updates manages the host's automatic-update policy.
type Updates struct {
	broker broker.Gateway
}

// NewUpdates constructs the service.
func NewUpdates(gw broker.Gateway) *Updates { return &Updates{broker: gw} }

// Available reports whether auto-updates can be configured.
func (u *Updates) Available() bool { return u != nil && u.broker != nil }

func (u *Updates) requireAvailable() error {
	if u.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "updates_unavailable", "Automatic updates need the broker.")
}

func (o *UpdatesOptions) validate() error {
	if o.RebootTime == "" {
		o.RebootTime = "02:00"
	}
	// "HH:MM", 24-hour. Reject anything else so it cannot smuggle into apt.conf.
	var h, m int
	if _, err := fmt.Sscanf(o.RebootTime, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return errx.Validation("invalid_reboot_time", "Reboot time must be HH:MM (24-hour).")
	}
	o.RebootTime = fmt.Sprintf("%02d:%02d", h, m)
	return nil
}

// Configure renders and applies the auto-update policy through the broker,
// returning the effective state the broker read back.
func (u *Updates) Configure(ctx context.Context, o UpdatesOptions) (map[string]string, error) {
	if err := u.requireAvailable(); err != nil {
		return nil, err
	}
	if err := o.validate(); err != nil {
		return nil, err
	}
	// Render both families' configs; the broker writes whichever one the host
	// actually uses (apt vs dnf). Rendering both here keeps the render pure and
	// testable and the broker free of policy.
	if _, err := u.broker.Invoke(ctx, "updates.configure", map[string]any{
		"config":     RenderUnattendedConfig(o),
		"dnf_config": RenderDNFAutomaticConfig(o),
		"enabled":    o.Enabled,
	}); err != nil {
		return nil, err
	}
	return u.Status(ctx)
}

// Status returns the effective auto-update state (config drop-in present, the
// merged apt-config values, whether the unattended-upgrades tool is installed).
func (u *Updates) Status(ctx context.Context) (map[string]string, error) {
	if err := u.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := u.broker.Invoke(ctx, "updates.status", map[string]any{})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range res {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out, nil
}

// RenderUnattendedConfig renders the panel's unattended-upgrades apt.conf. Pure
// over validated options. Security origin only by default; a full-updates policy
// widens the allowed origins but security is always included.
func RenderUnattendedConfig(o UpdatesOptions) string {
	var b strings.Builder
	b.WriteString("// NexPanel automatic security updates (rendered; do not edit).\n")
	enable := "1"
	if !o.Enabled {
		enable = "0"
	}
	fmt.Fprintf(&b, "APT::Periodic::Update-Package-Lists \"%s\";\n", enable)
	fmt.Fprintf(&b, "APT::Periodic::Unattended-Upgrade \"%s\";\n", enable)
	b.WriteString("APT::Periodic::Download-Upgradeable-Packages \"" + enable + "\";\n")
	b.WriteString("APT::Periodic::AutocleanInterval \"7\";\n")
	b.WriteString("Unattended-Upgrade::Allowed-Origins {\n")
	b.WriteString("\t\"${distro_id}:${distro_codename}-security\";\n")
	b.WriteString("\t\"${distro_id}ESMApps:${distro_codename}-apps-security\";\n")
	b.WriteString("\t\"${distro_id}ESM:${distro_codename}-infra-security\";\n")
	if !o.SecurityOnly {
		b.WriteString("\t\"${distro_id}:${distro_codename}-updates\";\n")
	}
	b.WriteString("};\n")
	b.WriteString("Unattended-Upgrade::Remove-Unused-Dependencies \"true\";\n")
	if o.AutomaticReboot {
		b.WriteString("Unattended-Upgrade::Automatic-Reboot \"true\";\n")
		fmt.Fprintf(&b, "Unattended-Upgrade::Automatic-Reboot-Time \"%s\";\n", o.RebootTime)
	} else {
		b.WriteString("Unattended-Upgrade::Automatic-Reboot \"false\";\n")
	}
	return b.String()
}

// RenderDNFAutomaticConfig renders the panel's dnf-automatic INI for Rocky/Alma,
// the RHEL-family counterpart of the apt config above. Pure over the same
// validated options: security-only maps to `upgrade_type = security`, disabling
// flips `apply_updates` to no (the timer is also disabled), and an auto-reboot
// becomes `reboot = when-needed` with a short delayed reboot command.
func RenderDNFAutomaticConfig(o UpdatesOptions) string {
	var b strings.Builder
	b.WriteString("# NexPanel automatic security updates (rendered; do not edit).\n")
	upgradeType := "default"
	if o.SecurityOnly {
		upgradeType = "security"
	}
	apply := "yes"
	if !o.Enabled {
		apply = "no"
	}
	reboot := "never"
	if o.AutomaticReboot {
		reboot = "when-needed"
	}
	b.WriteString("[commands]\n")
	fmt.Fprintf(&b, "upgrade_type = %s\n", upgradeType)
	b.WriteString("download_updates = yes\n")
	fmt.Fprintf(&b, "apply_updates = %s\n", apply)
	fmt.Fprintf(&b, "reboot = %s\n", reboot)
	// A delayed reboot gives an operator a window to cancel; the time-of-day
	// preference apt honours has no dnf-automatic equivalent, so a short delay is
	// the closest faithful behaviour.
	b.WriteString("reboot_command = \"shutdown -r +5 'NexPanel: rebooting to finish applying updates'\"\n\n")
	b.WriteString("[emitters]\n")
	b.WriteString("emit_via = stdio\n")
	return b.String()
}
