package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Per-version OPcache tuning.
//
// OPcache's shared-memory directives — opcache.memory_consumption,
// interned_strings_buffer, max_accelerated_files, jit_buffer_size — are
// PHP_INI_SYSTEM: the FPM master allocates that memory once, at startup, before
// any pool exists. They cannot be set per site (a pool that tried would pass the
// config test and change nothing). So the panel owns one version-wide ini
// (99-nexpanel-opcache.ini) and applies it with a **restart**, not a reload,
// for the same reason extensions need one: SIGUSR2 re-reads pool config but does
// not re-allocate the shared segment. npd renders the ini text and validates the
// numbers; the broker writes the pinned path, config-tests, and rolls back.

// PHPWriteOpcache writes the version-wide OPcache ini and restarts FPM.
type PHPWriteOpcache struct{}

type phpWriteOpcacheInput struct {
	Version string `json:"version"`
	Config  string `json:"config"`
}

// Name implements capability.Capability.
func (PHPWriteOpcache) Name() string { return "php.write_opcache" }

// Execute implements capability.Capability.
func (PHPWriteOpcache) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in phpWriteOpcacheInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for php.write_opcache.")
	}
	if err := capability.ValidatePHPVersion(in.Version); err != nil {
		return capability.Result{}, err
	}
	fam := detectPHPFamily(c)
	path := fam.opcacheININPath(in.Version)

	var prior []byte
	hadPrior := false
	if b, err := c.FS.ReadFile(path); err == nil {
		prior, hadPrior = b, true
	}
	if err := c.FS.WriteFile(path, []byte(in.Config), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "opcache_write_failed", "Could not write the OPcache config.")
	}
	restore := func() {
		if hadPrior {
			_ = c.FS.WriteFile(path, prior, 0o644)
		} else {
			_ = c.FS.Remove(path)
		}
	}

	// Config-test before restarting — one master serves every site on the version.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    fam.fpmBinary(in.Version),
		Args:    []string{"-t"},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		restore()
		return capability.Result{}, errx.New(errx.KindValidation, "fpm_config_invalid",
			"PHP-FPM rejected the OPcache configuration; the change was rolled back.")
	}

	res, err = c.Runner.Run(c.Ctx, exec.Command{
		Path:    systemctlPath,
		Args:    []string{"restart", fam.fpmService(in.Version)},
		Timeout: 60 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "fpm_restart_failed",
			"The OPcache config was written but PHP-FPM could not be restarted.")
	}
	return capability.Result{Data: map[string]any{"version": in.Version}}, nil
}

// PHPReadOpcache returns the version-wide OPcache ini text, or empty if none has
// been written yet. Read-only twin of write, so the UI shows what is actually in
// effect at the version level rather than a value cached elsewhere.
type PHPReadOpcache struct{}

// Name implements capability.Capability.
func (PHPReadOpcache) Name() string { return "php.read_opcache" }

// Execute implements capability.Capability.
func (PHPReadOpcache) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in phpWriteOpcacheInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for php.read_opcache.")
	}
	if err := capability.ValidatePHPVersion(in.Version); err != nil {
		return capability.Result{}, err
	}
	fam := detectPHPFamily(c)
	b, err := c.FS.ReadFile(fam.opcacheININPath(in.Version))
	if err != nil {
		// No file yet is a valid answer (defaults are in force), not a fault.
		return capability.Result{Data: map[string]any{"version": in.Version, "config": ""}}, nil
	}
	return capability.Result{Data: map[string]any{"version": in.Version, "config": string(b)}}, nil
}
