package capabilities

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Automatic security updates (Debian/Ubuntu). Write the panel's
// unattended-upgrades apt config drop-in, enable the apt timers, and read the
// EFFECTIVE merged config back with `apt-config dump` — the honest source of
// truth (the merge of every apt.conf.d file), the same way SSH uses `sshd -T`.

const (
	aptConfigPath    = "/usr/bin/apt-config"
	unattendedDropin = "/etc/apt/apt.conf.d/52heropanel-unattended"
	unattendedBin    = "/usr/bin/unattended-upgrade"
	updatesMaxConfig = 16 << 10

	dnfAutomaticConf  = "/etc/dnf/automatic.conf"
	dnfAutomaticBin   = "/usr/bin/dnf-automatic"
	dnfAutomaticTimer = "dnf-automatic.timer"
)

// updatesIsDebian reports whether this host is apt-based. apt-config is the
// Debian-family marker (the same distro-detection shape the PHP capabilities
// use); anything else is treated as the RHEL/dnf family.
func updatesIsDebian(c capability.Context) bool {
	ok, _ := c.FS.Exists(aptConfigPath)
	return ok
}

// UpdatesConfigure writes the drop-in and enables the auto-update timers.
type UpdatesConfigure struct{}

type updatesConfigureInput struct {
	Config    string `json:"config"`     // apt.conf drop-in (Debian family)
	DNFConfig string `json:"dnf_config"` // dnf-automatic INI (RHEL family)
	Enabled   bool   `json:"enabled"`
}

// Name implements capability.Capability.
func (UpdatesConfigure) Name() string { return "updates.configure" }

// Execute implements capability.Capability.
func (UpdatesConfigure) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in updatesConfigureInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for updates.configure.")
	}
	if !updatesIsDebian(c) {
		return configureDNFAutomatic(c, in)
	}
	if len(in.Config) == 0 || len(in.Config) > updatesMaxConfig || strings.ContainsRune(in.Config, 0) {
		return capability.Result{}, errx.Validation("bad_config", "The updates config is missing or invalid.")
	}
	if err := c.FS.WriteFile(unattendedDropin, []byte(in.Config), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "updates_write_failed", "Could not write the updates config.")
	}
	// Validate it parses as apt config: `apt-config dump` fails on a malformed
	// apt.conf, so a broken drop-in is caught here before it can wedge apt.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: aptConfigPath, Args: []string{"dump"}, Timeout: 20 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		_ = c.FS.Remove(unattendedDropin)
		return capability.Result{}, errx.New(errx.KindValidation, "updates_config_invalid",
			"apt rejected the updates config; the change was rolled back.")
	}
	// Enable the apt timers so the policy actually fires (best-effort: the timers
	// are the distro's; on a host without systemd this is a no-op and the config
	// still stands).
	timers := map[string]bool{}
	for _, t := range []string{"apt-daily.timer", "apt-daily-upgrade.timer"} {
		enable := "enable"
		if !in.Enabled {
			enable = "disable"
		}
		r, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: systemctlPath, Args: []string{enable, "--now", t}, Timeout: 30 * time.Second,
		})
		timers[t] = err == nil && r.ExitCode == 0
	}
	return capability.Result{Data: map[string]any{"configured": true, "timers": timers}}, nil
}

// configureDNFAutomatic is the RHEL-family branch: write /etc/dnf/automatic.conf
// and enable (or disable) the dnf-automatic timer. dnf-automatic has no
// config-test equivalent to `apt-config dump`, so the guard is a structural
// sanity check on the rendered INI before it lands.
func configureDNFAutomatic(c capability.Context, in updatesConfigureInput) (capability.Result, error) {
	cfg := in.DNFConfig
	if len(cfg) == 0 || len(cfg) > updatesMaxConfig || strings.ContainsRune(cfg, 0) ||
		!strings.Contains(cfg, "[commands]") || !strings.Contains(cfg, "apply_updates") {
		return capability.Result{}, errx.Validation("bad_config", "The dnf-automatic config is missing or invalid.")
	}
	if err := c.FS.WriteFile(dnfAutomaticConf, []byte(cfg), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "updates_write_failed", "Could not write the dnf-automatic config.")
	}
	// Enable the timer so the policy fires (best-effort, like the apt path: on a
	// host without systemd this is a no-op and the config still stands).
	enable := "enable"
	if !in.Enabled {
		enable = "disable"
	}
	r, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{enable, "--now", dnfAutomaticTimer}, Timeout: 30 * time.Second,
	})
	return capability.Result{Data: map[string]any{
		"configured": true,
		"timers":     map[string]bool{dnfAutomaticTimer: err == nil && r.ExitCode == 0},
	}}, nil
}

// UpdatesStatus reports the effective auto-update state.
type UpdatesStatus struct{}

// Name implements capability.Capability.
func (UpdatesStatus) Name() string { return "updates.status" }

// Execute implements capability.Capability.
func (UpdatesStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	if !updatesIsDebian(c) {
		return statusDNFAutomatic(c)
	}
	dropin, _ := c.FS.Exists(unattendedDropin)
	toolPresent, _ := c.FS.Exists(unattendedBin)
	// The effective, merged value apt would actually use.
	unattended := "0"
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: aptConfigPath, Args: []string{"dump"}, Timeout: 20 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		for _, line := range strings.Split(string(res.Stdout), "\n") {
			if strings.HasPrefix(line, "APT::Periodic::Unattended-Upgrade ") {
				// `APT::Periodic::Unattended-Upgrade "1";`
				if i := strings.Index(line, "\""); i >= 0 {
					if j := strings.Index(line[i+1:], "\""); j >= 0 {
						unattended = line[i+1 : i+1+j]
					}
				}
			}
		}
	}
	return capability.Result{Data: map[string]any{
		"dropin_present":       dropin,
		"tool_present":         toolPresent,
		"unattended_effective": unattended,
		"family":               "debian",
	}}, nil
}

// statusDNFAutomatic reports the RHEL-family state. dnf-automatic keeps a single
// config file (no merged-drop-in system), so the effective value is read back
// from the config the panel manages — mapped to the same 1/0 the apt path
// returns so one UI reads both families.
func statusDNFAutomatic(c capability.Context) (capability.Result, error) {
	present, _ := c.FS.Exists(dnfAutomaticConf)
	toolPresent, _ := c.FS.Exists(dnfAutomaticBin)
	effective := "0"
	if b, err := c.FS.ReadFile(dnfAutomaticConf); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "apply_updates") {
				if i := strings.Index(line, "="); i >= 0 && strings.EqualFold(strings.TrimSpace(line[i+1:]), "yes") {
					effective = "1"
				}
			}
		}
	}
	return capability.Result{Data: map[string]any{
		"dropin_present":       present,
		"tool_present":         toolPresent,
		"unattended_effective": effective,
		"family":               "rhel",
	}}, nil
}
