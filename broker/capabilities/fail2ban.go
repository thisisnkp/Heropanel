package capabilities

import (
	"encoding/json"
	"net"
	"regexp"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Fail2Ban, driven through its own client. The broker runs fail2ban-client and
// returns raw output; npd parses it (the schema lives where it can be tested
// over fixtures). Bans and unbans take a validated jail name and a parsed IP,
// so nothing user-derived reaches the process as anything but an argument.

const fail2banClient = "/usr/bin/fail2ban-client"

// reJailName bounds a jail name (fail2ban's own naming: letters, digits, dash,
// underscore).
var reJailName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ── fail2ban.status ──────────────────────────────────────────────────────────

// Fail2BanStatus returns `fail2ban-client status` (the jail list) or, with a
// jail, that jail's detail (banned IPs). Read-only.
type Fail2BanStatus struct{}

type fail2banStatusInput struct {
	Jail string `json:"jail,omitempty"`
}

// Name implements capability.Capability.
func (Fail2BanStatus) Name() string { return "fail2ban.status" }

// Execute implements capability.Capability.
func (Fail2BanStatus) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in fail2banStatusInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for fail2ban.status.")
	}
	args := []string{"status"}
	if in.Jail != "" {
		if !reJailName.MatchString(in.Jail) {
			return capability.Result{}, errx.Validation("invalid_jail", "Invalid jail name.")
		}
		args = append(args, in.Jail)
	}
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: fail2banClient, Args: args, Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "fail2ban_failed", "Could not query fail2ban.")
	}
	// A stopped server is an answer (running=false), not an error.
	return capability.Result{Data: map[string]any{
		"raw": string(res.Stdout), "running": res.ExitCode == 0,
	}}, nil
}

// ── fail2ban.ban / fail2ban.unban ────────────────────────────────────────────

// Fail2BanBan manually bans an IP in a jail.
type Fail2BanBan struct{}

type fail2banBanInput struct {
	Jail string `json:"jail"`
	IP   string `json:"ip"`
}

// Name implements capability.Capability.
func (Fail2BanBan) Name() string { return "fail2ban.ban" }

// Execute implements capability.Capability.
func (Fail2BanBan) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	return runFail2BanAction(c, raw, "banip")
}

// Fail2BanUnban lifts a ban.
type Fail2BanUnban struct{}

// Name implements capability.Capability.
func (Fail2BanUnban) Name() string { return "fail2ban.unban" }

// Execute implements capability.Capability.
func (Fail2BanUnban) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	return runFail2BanAction(c, raw, "unbanip")
}

// runFail2BanAction validates a jail+IP and runs `set <jail> <action> <ip>`.
func runFail2BanAction(c capability.Context, raw json.RawMessage, action string) (capability.Result, error) {
	var in fail2banBanInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for fail2ban.")
	}
	if !reJailName.MatchString(in.Jail) {
		return capability.Result{}, errx.Validation("invalid_jail", "Invalid jail name.")
	}
	if net.ParseIP(in.IP) == nil {
		return capability.Result{}, errx.Validation("invalid_ip", "Invalid IP address.")
	}
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: fail2banClient, Args: []string{"set", in.Jail, action, in.IP}, Timeout: 30 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "fail2ban_failed",
			"fail2ban rejected the "+action+" request.")
	}
	return capability.Result{Data: map[string]any{"ok": true, "action": action, "ip": in.IP}}, nil
}
