package capabilities

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// PHP extension management.
//
// **Extensions are per PHP version and per SAPI — never per site.** This is not
// a simplification, it is how PHP works: the FPM master loads extensions at
// startup, from /etc/php/<v>/fpm/conf.d, long before any pool's config is
// applied. A pool may carry `php_admin_value[extension] = foo.so` and php-fpm -t
// will call the config valid — it is simply ignored. That combination is the
// trap here: a per-site extension switch would look like it worked, pass the
// config test, and do nothing at all.
//
// So the API is honest about its scope: enabling an extension enables it for
// every site on that version, and the panel says so.
//
// Layout is Debian/Ubuntu's (php-common's phpenmod/phpdismod over symlinks from
// fpm/conf.d into mods-available). Rocky/Alma put a flat /etc/php.d in front of
// a single version and need a different implementation; see docs/16.
const (
	phpEnmodPath    = "/usr/sbin/phpenmod"
	phpDismodPath   = "/usr/sbin/phpdismod"
	phpModsAvailDir = "mods-available"
	phpFPMSAPI      = "fpm"
)

// reExtension bounds what can reach phpenmod's argv. Extension names are short
// lowercase identifiers; anything else is not a name we could act on anyway.
var reExtension = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func validateExtension(name string) error {
	if !reExtension.MatchString(name) {
		return errx.Validation("invalid_extension", "Invalid PHP extension name.",
			errx.Field{Field: "extension", Code: "invalid", Message: "expected a name like \"gd\" or \"pdo_mysql\""})
	}
	return nil
}

// PHPListExtensions reports which extensions exist for a version and which are
// enabled for its FPM SAPI.
type PHPListExtensions struct{}

type phpListExtInput struct {
	Version string `json:"version"`
}

// Name implements capability.Capability.
func (PHPListExtensions) Name() string { return "php.list_extensions" }

// Execute implements capability.Capability.
func (PHPListExtensions) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in phpListExtInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for php.list_extensions.")
	}
	if err := capability.ValidatePHPVersion(in.Version); err != nil {
		return capability.Result{}, err
	}

	fam := detectPHPFamily(c)
	// Available extensions are everything that exists for the version, disabled
	// ones included (on RHEL a disabled ext is a `.ini.disabled` still in php.d).
	available, err := listExtNames(c, fam.availDir(in.Version), true)
	if err != nil {
		return capability.Result{}, err
	}
	// What FPM has enabled: on Debian, the symlinks in its conf.d; on RHEL, the
	// non-disabled inis in the flat php.d. Deliberately not `php -m`: that reports
	// the *CLI* SAPI, a different conf.d and a different answer — confidently wrong
	// about the thing being asked.
	enabled, err := listExtNames(c, fam.enabledDir(in.Version), false)
	if err != nil {
		return capability.Result{}, err
	}

	return capability.Result{Data: map[string]any{
		"version":   in.Version,
		"available": available,
		"enabled":   enabled,
	}}, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// PHPSetExtension enables or disables an extension for a version's FPM SAPI.
type PHPSetExtension struct{}

type phpSetExtInput struct {
	Version   string `json:"version"`
	Extension string `json:"extension"`
	Enabled   bool   `json:"enabled"`
}

// Name implements capability.Capability.
func (PHPSetExtension) Name() string { return "php.set_extension" }

// Execute implements capability.Capability.
func (PHPSetExtension) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in phpSetExtInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for php.set_extension.")
	}
	if err := capability.ValidatePHPVersion(in.Version); err != nil {
		return capability.Result{}, err
	}
	if err := validateExtension(in.Extension); err != nil {
		return capability.Result{}, err
	}

	fam := detectPHPFamily(c)

	// undo restores the prior extension state, so a failed config-test leaves the
	// host exactly as it was found. Each family has its own inverse.
	var undo func()
	if fam == phpRHEL {
		if _, err := toggleExtRHEL(c, in.Version, in.Extension, in.Enabled); err != nil {
			return capability.Result{}, err
		}
		undo = func() { _, _ = toggleExtRHEL(c, in.Version, in.Extension, !in.Enabled) }
	} else {
		tool := phpDismodPath
		if in.Enabled {
			tool = phpEnmodPath
		}
		// -s fpm scopes the change to the FPM SAPI. Without it phpenmod would also
		// touch the CLI's conf.d, changing what a site's own `php` command sees as a
		// side effect of a web-server setting.
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path:    tool,
			Args:    []string{"-v", in.Version, "-s", phpFPMSAPI, in.Extension},
			Timeout: 30 * time.Second,
		})
		if err != nil {
			return capability.Result{}, errx.Upstream(err, "ext_toggle_failed", "Could not change the PHP extension.")
		}
		if res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindValidation, "ext_toggle_failed",
				"PHP did not accept that extension change; the extension may not be installed.")
		}
		back := phpEnmodPath
		if in.Enabled {
			back = phpDismodPath
		}
		undo = func() {
			_, _ = c.Runner.Run(c.Ctx, exec.Command{
				Path:    back,
				Args:    []string{"-v", in.Version, "-s", phpFPMSAPI, in.Extension},
				Timeout: 30 * time.Second,
			})
		}
	}

	// Config-test before reload, exactly as php.write_pool does: one master
	// serves every site on this version.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    fam.fpmBinary(in.Version),
		Args:    []string{"-t"},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		undo() // put it back the way it was before reporting a failure
		return capability.Result{}, errx.New(errx.KindValidation, "fpm_config_invalid",
			"PHP-FPM rejected the configuration with that extension change; it was rolled back.")
	}

	// A restart, not a reload. Extensions are loaded by the master when it execs;
	// SIGUSR2 re-reads pool config but does not re-link the extension into a
	// process that has already started. Reloading here would report success and
	// leave the extension exactly as it was — the same silent no-op this
	// capability exists to avoid.
	res, err = c.Runner.Run(c.Ctx, exec.Command{
		Path:    systemctlPath,
		Args:    []string{"restart", fam.fpmService(in.Version)},
		Timeout: 60 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "fpm_restart_failed",
			"The extension was changed but PHP-FPM could not be restarted.")
	}

	return capability.Result{Data: map[string]any{
		"version":   in.Version,
		"extension": in.Extension,
		"enabled":   in.Enabled,
	}}, nil
}
