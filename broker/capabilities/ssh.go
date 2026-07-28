package capabilities

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// SSH hardening: write the panel's sshd drop-in, config-test it with `sshd -t`
// BEFORE it can take effect, and reload (not restart) so live sessions survive.
// The config text is rendered by hpd from validated fields; the broker writes
// only the one pinned path, tests it, reloads, and rolls back on any failure —
// the same reload-first discipline as php.write_pool and webserver.apply, and
// it matters as much here: a bad sshd config that the daemon refuses on restart
// would leave the box unreachable.

const (
	sshdPath          = "/usr/sbin/sshd"
	sshHardenDropin   = "/etc/ssh/sshd_config.d/50-heropanel.conf"
	sshMaxConfig      = 32 << 10
	sshServiceName    = "ssh"
	sshServiceNameAlt = "sshd"
)

// SSHHarden writes the sshd drop-in, tests it, and reloads.
type SSHHarden struct{}

type sshHardenInput struct {
	Config string `json:"config"`
}

// Name implements capability.Capability.
func (SSHHarden) Name() string { return "ssh.harden" }

// Execute implements capability.Capability.
func (SSHHarden) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in sshHardenInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for ssh.harden.")
	}
	if len(in.Config) == 0 || len(in.Config) > sshMaxConfig || strings.ContainsRune(in.Config, 0) {
		return capability.Result{}, errx.Validation("bad_config", "The sshd config is missing or invalid.")
	}

	// Back the current drop-in up so a failed test can restore it.
	var prior []byte
	hadPrior := false
	if b, err := c.FS.ReadFile(sshHardenDropin); err == nil {
		prior, hadPrior = b, true
	}
	if err := c.FS.WriteFile(sshHardenDropin, []byte(in.Config), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "ssh_write_failed", "Could not write the sshd drop-in.")
	}
	restore := func() {
		if hadPrior {
			_ = c.FS.WriteFile(sshHardenDropin, prior, 0o644)
		} else {
			_ = c.FS.Remove(sshHardenDropin)
		}
	}

	// Config-test the WHOLE sshd config (drop-in included). `sshd -t` exits non-
	// zero on any error, printing to stderr; a config the daemon would refuse
	// never reaches a reload.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: sshdPath, Args: []string{"-t"}, Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		restore()
		return capability.Result{}, errx.New(errx.KindValidation, "sshd_config_invalid",
			"sshd rejected the configuration; the change was rolled back.")
	}

	// Reload, not restart: existing sessions keep running while new connections
	// pick up the change. Tolerate either service name across distros.
	reloaded := false
	for _, svc := range []string{sshServiceName, sshServiceNameAlt} {
		if r, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: systemctlPath, Args: []string{"reload", svc}, Timeout: 30 * time.Second,
		}); err == nil && r.ExitCode == 0 {
			reloaded = true
			break
		}
	}
	return capability.Result{Data: map[string]any{"hardened": true, "reloaded": reloaded}}, nil
}

// SSHStatus reads the effective sshd configuration with `sshd -T`, which
// resolves the entire config (drop-ins included) without needing a running
// daemon — the honest source of truth for what sshd would actually enforce.
type SSHStatus struct{}

// Name implements capability.Capability.
func (SSHStatus) Name() string { return "ssh.status" }

// Execute implements capability.Capability.
func (SSHStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: sshdPath, Args: []string{"-T"}, Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "sshd_status_failed",
			"Could not read the effective sshd configuration.")
	}
	dropin, _ := c.FS.Exists(sshHardenDropin)
	return capability.Result{Data: map[string]any{
		"effective":      string(res.Stdout),
		"dropin_present": dropin,
	}}, nil
}
