package capabilities

import (
	"encoding/json"
	"io/fs"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// OpenLiteSpeed paths on the target systems.
const (
	olsVhostRoot    = "/usr/local/lsws/conf/vhosts"
	olsListenerConf = "/usr/local/lsws/conf/heropanel.conf"
	olsBin          = "/usr/local/lsws/bin/lshttpd"
	lswsctrl        = "/usr/local/lsws/bin/lswsctrl"
)

// WebServerApply applies the full desired web-server configuration: it writes
// each site's rendered vhost config plus the aggregate listener config, tests
// the configuration, and reloads. On a failed test it rolls back to the prior
// configuration. This declarative "render-all, apply, test, reload, rollback"
// model avoids per-site config drift (docs/05 §2).
//
// The configuration text is rendered by hpd; the broker only writes validated
// paths and runs the (fixed) test/reload commands.
type WebServerApply struct{}

type vhostEntry struct {
	Name   string `json:"name"`
	Config string `json:"config"`
}

type webServerApplyInput struct {
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

	// 3. Test the configuration; roll back if invalid.
	test, err := c.Runner.Run(c.Ctx, exec.Command{Path: olsBin, Args: []string{"-t"}, Timeout: 20 * time.Second})
	if err != nil || test.ExitCode != 0 {
		rollback()
		return capability.Result{}, errx.New(errx.KindUpstream, "config_test_failed",
			"The web server configuration test failed; changes were rolled back.")
	}

	// 4. Reload (graceful). The config is valid, so we do not roll back on a
	// transient reload failure — we surface it for retry.
	reload, err := c.Runner.Run(c.Ctx, exec.Command{Path: lswsctrl, Args: []string{"reload"}, Timeout: 20 * time.Second})
	if err != nil || reload.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "reload_failed",
			"The web server reload failed after applying a valid configuration.")
	}

	return capability.Result{Data: map[string]any{
		"vhosts_applied": len(in.Vhosts),
		"reloaded":       true,
	}}, nil
}
