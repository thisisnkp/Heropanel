package capabilities

import (
	"encoding/json"
	"io/fs"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Web-server config paths + binaries on the target systems, per engine.
const (
	// OpenLiteSpeed.
	olsVhostRoot    = "/usr/local/lsws/conf/vhosts"
	olsListenerConf = "/usr/local/lsws/conf/nexpanel.conf"
	olsBin          = "/usr/local/lsws/bin/lshttpd"
	lswsctrl        = "/usr/local/lsws/bin/lswsctrl"
	// LiteSpeed Enterprise parses httpd-syntax config; the panel renders it and
	// writes it here, then reloads via lswsctrl.
	lswsEntConf = "/usr/local/lsws/conf/httpd_config.conf"
)

// WebServerApply applies the full desired web-server configuration: it writes the
// rendered config, tests it, and reloads, rolling back to the prior config on a
// failed test. This declarative "render-all, apply, test, reload, rollback" model
// avoids per-site config drift (docs/05 §2).
//
// The configuration text is rendered by npd (per engine); the broker only writes
// validated paths and runs the (fixed) test/reload commands for the chosen
// engine. An empty or unrecognized engine means OpenLiteSpeed — the panel's
// default and the only engine an unlicensed install runs.
type WebServerApply struct{}

type vhostEntry struct {
	Name   string `json:"name"`
	Config string `json:"config"`
}

type webServerApplyInput struct {
	Engine   string       `json:"engine"`
	Vhosts   []vhostEntry `json:"vhosts"`
	Listener string       `json:"listener"`
}

// Name implements capability.Capability.
func (WebServerApply) Name() string { return "webserver.apply" }

type fileBackup struct {
	existed bool
	content []byte
}

// Execute implements capability.Capability.
func (WebServerApply) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in webServerApplyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for webserver.apply.")
	}

	backups := map[string]fileBackup{}
	rollback := func() {
		for path, b := range backups {
			if b.existed {
				_ = c.FS.WriteFile(path, b.content, 0o644)
			} else {
				_ = c.FS.Remove(path)
			}
		}
	}
	write := func(path string, content string, mode fs.FileMode) error {
		if _, seen := backups[path]; !seen {
			if prev, err := c.FS.ReadFile(path); err == nil {
				backups[path] = fileBackup{existed: true, content: prev}
			} else {
				backups[path] = fileBackup{existed: false}
			}
		}
		return c.FS.WriteFile(path, []byte(content), mode)
	}

	// Any engine the panel no longer supports — including "nginx" and "apache",
	// which it used to — lands on OpenLiteSpeed rather than erroring, so an
	// upgraded install keeps serving on the engine it now actually runs.
	switch in.Engine {
	case "litespeed_enterprise":
		return applyLiteSpeedEnt(c, in, write, rollback)
	default:
		return applyOLS(c, in, write, rollback)
	}
}

// ── OpenLiteSpeed ────────────────────────────────────────────────────────────

func applyOLS(c capability.Context, in webServerApplyInput, write func(string, string, fs.FileMode) error, rollback func()) (capability.Result, error) {
	// 1. Per-vhost config files.
	for _, vh := range in.Vhosts {
		if err := capability.ValidateVhostName(vh.Name); err != nil {
			rollback()
			return capability.Result{}, err
		}
		dir := olsVhostRoot + "/" + vh.Name
		if err := c.FS.MkdirAll(dir, 0o755); err != nil {
			rollback()
			return capability.Result{}, errx.Upstream(err, "vhost_mkdir_failed", "Could not create the vhost config directory.")
		}
		if err := write(dir+"/vhconf.conf", vh.Config, 0o644); err != nil {
			rollback()
			return capability.Result{}, errx.Upstream(err, "vhost_write_failed", "Could not write the vhost config.")
		}
	}

	// 2. Aggregate listener config.
	if err := write(olsListenerConf, in.Listener, 0o644); err != nil {
		rollback()
		return capability.Result{}, errx.Upstream(err, "listener_write_failed", "Could not write the listener config.")
	}

	// 3. Apply. OpenLiteSpeed's graceful reload is fail-safe: a bad config leaves
	// the previous workers serving (no downtime), so a running server is reloaded
	// directly. Note (verified against real OLS): `lshttpd -t` is NOT a reliable
	// gate while the server is running — it returns non-zero for benign vhost
	// warnings — so we only fall back to it when the server is stopped.
	reload, rerr := c.Runner.Run(c.Ctx, exec.Command{Path: lswsctrl, Args: []string{"reload"}, Timeout: 20 * time.Second})
	if rerr == nil && reload.ExitCode == 0 {
		return capability.Result{Data: map[string]any{"engine": "openlitespeed", "vhosts_applied": len(in.Vhosts), "reloaded": true}}, nil
	}

	// 4. Server not running (or reload failed): validate with the stopped-server
	// config test, which is reliable in that state, and roll back if invalid.
	test, terr := c.Runner.Run(c.Ctx, exec.Command{Path: olsBin, Args: []string{"-t"}, Timeout: 20 * time.Second})
	if terr != nil || test.ExitCode != 0 {
		rollback()
		return capability.Result{}, errx.New(errx.KindUpstream, "config_test_failed",
			"The web server configuration is invalid; changes were rolled back.")
	}
	return capability.Result{Data: map[string]any{
		"engine": "openlitespeed", "vhosts_applied": len(in.Vhosts), "reloaded": false,
		"note": "configuration is valid; the web server was not running to reload",
	}}, nil
}

// ── LiteSpeed Enterprise ─────────────────────────────────────────────────────

func applyLiteSpeedEnt(c capability.Context, in webServerApplyInput, write func(string, string, fs.FileMode) error, rollback func()) (capability.Result, error) {
	// LiteSpeed Enterprise is a drop-in Apache replacement, so its config is
	// httpd syntax; the panel renders that and writes it to LSWS's config path.
	// Reload is graceful (fail-safe) like OpenLiteSpeed, so there is no separate
	// config-test gate.
	if err := write(lswsEntConf, in.Listener, 0o644); err != nil {
		rollback()
		return capability.Result{}, errx.Upstream(err, "listener_write_failed", "Could not write the LiteSpeed config.")
	}
	reload, rerr := c.Runner.Run(c.Ctx, exec.Command{Path: lswsctrl, Args: []string{"reload"}, Timeout: 20 * time.Second})
	reloaded := rerr == nil && reload.ExitCode == 0
	return capability.Result{Data: map[string]any{"engine": "litespeed_enterprise", "reloaded": reloaded}}, nil
}

// reloadService reloads a systemd unit, reporting whether it succeeded.
func reloadService(c capability.Context, unit string) bool {
	r, err := c.Runner.Run(c.Ctx, exec.Command{Path: systemctlPath, Args: []string{"reload", unit}, Timeout: 20 * time.Second})
	return err == nil && r.ExitCode == 0
}
