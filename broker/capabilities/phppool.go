package capabilities

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// phpPoolBase is the PHP-FPM pool directory base (Debian/Ubuntu layout):
// /etc/php/<version>/fpm/pool.d/<pool>.conf.
const phpPoolBase = "/etc/php"

// PHPWritePool writes a per-site PHP-FPM pool config and reloads the matching
// php-fpm service. The pool config is rendered by hpd; the broker writes the
// validated path and reloads, rolling back on failure.
type PHPWritePool struct{}

type phpWritePoolInput struct {
	Version  string `json:"version"`
	PoolName string `json:"pool_name"`
	Config   string `json:"config"`
}

// Name implements capability.Capability.
func (PHPWritePool) Name() string { return "php.write_pool" }

// Execute implements capability.Capability.
func (PHPWritePool) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in phpWritePoolInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for php.write_pool.")
	}
	if err := capability.ValidatePHPVersion(in.Version); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.PoolName); err != nil {
		return capability.Result{}, err
	}

	dir := fmt.Sprintf("%s/%s/fpm/pool.d", phpPoolBase, in.Version)
	path := dir + "/" + in.PoolName + ".conf"

	if err := c.FS.MkdirAll(dir, 0o755); err != nil {
		return capability.Result{}, errx.Upstream(err, "pool_mkdir_failed", "Could not create the PHP pool directory.")
	}

	var prior []byte
	hadPrior := false
	if b, err := c.FS.ReadFile(path); err == nil {
		prior, hadPrior = b, true
	}
	if err := c.FS.WriteFile(path, []byte(in.Config), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "pool_write_failed", "Could not write the PHP pool config.")
	}

	restore := func() {
		if hadPrior {
			_ = c.FS.WriteFile(path, prior, 0o644)
		} else {
			_ = c.FS.Remove(path)
		}
	}

	// Config-test before reloading — the same reload-first discipline as
	// webserver.apply, and it matters more here. One FPM master serves every site
	// on a PHP version, and it re-reads *all* pool files on SIGUSR2. A pool this
	// site made invalid does not fail this site's reload; it stops the master
	// coming back, and every other site on that version goes down with it. `-t`
	// asks php-fpm whether it would accept the config while the running master is
	// still untouched.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    fmt.Sprintf("/usr/sbin/php-fpm%s", in.Version),
		Args:    []string{"-t"},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		restore()
		return capability.Result{}, errx.New(errx.KindValidation, "fpm_config_invalid",
			"PHP-FPM rejected the pool configuration; the change was rolled back.")
	}

	service := fmt.Sprintf("php%s-fpm", in.Version)
	res, err = c.Runner.Run(c.Ctx, exec.Command{
		Path:    systemctlPath,
		Args:    []string{"reload", service},
		Timeout: 30 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		// Roll back the pool config on reload failure.
		restore()
		return capability.Result{}, errx.New(errx.KindUpstream, "fpm_reload_failed",
			"Reloading PHP-FPM failed; the pool change was rolled back.")
	}

	return capability.Result{Data: map[string]any{
		"version": in.Version,
		"pool":    in.PoolName,
	}}, nil
}
