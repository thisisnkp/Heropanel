package capabilities

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// unitDir is where per-site app units live. The unit name is derived from the
// validated vhost id, so it is always a safe filename.
const unitDir = "/etc/systemd/system"

// reAppEnvKey is a conventional environment variable name (defense in depth; hpd
// validates too).
var reAppEnvKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func appUnitName(vhost string) string { return "heropanel-app-" + vhost + ".service" }
func appUnitPath(vhost string) string { return unitDir + "/" + appUnitName(vhost) }

// ── app.unit_apply ───────────────────────────────────────────────────────────

// AppUnitApply writes a hardened per-site systemd unit that runs the app as the
// site's unprivileged user in its current release, then (re)starts it. The start
// command is placed in a launcher script inside the site home (not the unit's
// ExecStart line), so any command runs without systemd/shell quoting issues.
type AppUnitApply struct{}

func (AppUnitApply) Name() string { return "app.unit_apply" }

type appUnitApplyInput struct {
	Vhost    string            `json:"vhost"`
	Username string            `json:"username"`
	Home     string            `json:"home"`
	Command  string            `json:"command"`
	Port     int               `json:"port"`
	Env      map[string]string `json:"env"`
	Runtime  string            `json:"runtime"`
}

func (AppUnitApply) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in appUnitApplyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for app.unit_apply.")
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if in.Port < 1024 || in.Port > 65535 {
		return capability.Result{}, errx.Validation("invalid_port", "Port out of range.")
	}
	if in.Command == "" || len(in.Command) > 1000 || strings.ContainsAny(in.Command, "\x00\n\r") {
		return capability.Result{}, errx.Validation("invalid_command", "Invalid start command.")
	}
	for k, v := range in.Env {
		if !reAppEnvKey.MatchString(k) || strings.ContainsAny(v, "\x00\n\r") {
			return capability.Result{}, errx.Validation("invalid_env", "Invalid environment entry: "+k)
		}
	}

	home := strings.TrimRight(in.Home, "/")
	launcher := home + "/.heropanel-run"
	// The launcher carries the command; the unit just execs it. `exec` replaces
	// the shell so systemd supervises the app process directly.
	if err := c.FS.WriteFile(launcher, []byte("#!/bin/sh\nexec "+in.Command+"\n"), 0o755); err != nil {
		return capability.Result{}, errx.Upstream(err, "launcher_write_failed", "Could not write the app launcher.")
	}

	unit := renderAppUnit(in, home, launcher)
	if err := c.FS.WriteFile(appUnitPath(in.Vhost), []byte(unit), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "unit_write_failed", "Could not write the systemd unit.")
	}

	// daemon-reload, enable (persist across reboot), then (re)start to pick up the
	// new unit/release.
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", appUnitName(in.Vhost)},
		{"restart", appUnitName(in.Vhost)},
	} {
		res, err := c.Runner.Run(c.Ctx, exec.Command{Path: systemctlPath, Args: args, Timeout: 30 * time.Second})
		if err != nil {
			return capability.Result{}, errx.Upstream(err, "systemctl_failed", "systemctl "+args[0]+" failed.")
		}
		if res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "systemctl_failed",
				"systemctl "+args[0]+" returned non-zero: "+string(res.Stderr))
		}
	}

	return capability.Result{Data: map[string]any{"vhost": in.Vhost, "unit": appUnitName(in.Vhost), "applied": true}}, nil
}

// renderAppUnit builds the hardened unit file. Env is sorted for a deterministic,
// testable result; the runtime PORT is emitted last so it is authoritative.
func renderAppUnit(in appUnitApplyInput, home, launcher string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=HeroPanel app " + in.Vhost + "\n")
	b.WriteString("After=network.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("User=" + in.Username + "\n")
	b.WriteString("Group=" + in.Username + "\n")
	b.WriteString("WorkingDirectory=" + home + "/current\n")

	keys := make([]string, 0, len(in.Env))
	for k := range in.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("Environment=\"" + k + "=" + in.Env[k] + "\"\n")
	}
	b.WriteString("Environment=PORT=" + strconv.Itoa(in.Port) + "\n")

	b.WriteString("ExecStart=" + launcher + "\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=2\n")
	// Hardening: the app can only touch its own tree.
	b.WriteString("NoNewPrivileges=true\n")
	b.WriteString("PrivateTmp=true\n")
	b.WriteString("ProtectSystem=strict\n")
	b.WriteString("ProtectHome=true\n")
	b.WriteString("ReadWritePaths=" + home + "\n")
	b.WriteString("UMask=0027\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	return b.String()
}

// ── app.unit_control ─────────────────────────────────────────────────────────

// AppUnitControl starts, stops, or restarts a site's app unit.
type AppUnitControl struct{}

func (AppUnitControl) Name() string { return "app.unit_control" }

type appUnitControlInput struct {
	Vhost  string `json:"vhost"`
	Action string `json:"action"`
}

func (AppUnitControl) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in appUnitControlInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for app.unit_control.")
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	switch in.Action {
	case "start", "stop", "restart":
	default:
		return capability.Result{}, errx.Validation("invalid_action", "Action must be start, stop, or restart.")
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{in.Action, appUnitName(in.Vhost)}, Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "systemctl_failed", "systemctl "+in.Action+" failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "systemctl_failed",
			"systemctl "+in.Action+" returned non-zero.")
	}
	return capability.Result{Data: map[string]any{"vhost": in.Vhost, "action": in.Action}}, nil
}

// ── app.unit_remove ──────────────────────────────────────────────────────────

// AppUnitRemove stops, disables, and deletes a site's app unit. It is
// idempotent: a missing unit is not an error.
type AppUnitRemove struct{}

func (AppUnitRemove) Name() string { return "app.unit_remove" }

type appUnitRemoveInput struct {
	Vhost string `json:"vhost"`
}

func (AppUnitRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in appUnitRemoveInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for app.unit_remove.")
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	// Best-effort stop+disable (ignore failures — the unit may already be gone).
	_, _ = c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"disable", "--now", appUnitName(in.Vhost)}, Timeout: 30 * time.Second,
	})
	_ = c.FS.Remove(appUnitPath(in.Vhost))
	_, _ = c.Runner.Run(c.Ctx, exec.Command{Path: systemctlPath, Args: []string{"daemon-reload"}, Timeout: 30 * time.Second})
	return capability.Result{Data: map[string]any{"vhost": in.Vhost, "removed": true}}, nil
}
