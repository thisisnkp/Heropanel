package capabilities

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
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

	available, err := listININames(c, fmt.Sprintf("%s/%s/%s", phpPoolBase, in.Version, phpModsAvailDir))
	if err != nil {
		return capability.Result{}, err
	}
	// What FPM has enabled is what is symlinked into its conf.d. Deliberately not
	// `php -m`: that reports the *CLI* SAPI, which has its own conf.d and its own
	// answer — a list that would be confidently wrong about the thing being asked.
	enabled, err := listININames(c, fmt.Sprintf("%s/%s/%s/conf.d", phpPoolBase, in.Version, phpFPMSAPI))
	if err != nil {
		return capability.Result{}, err
	}

	return capability.Result{Data: map[string]any{
		"version":   in.Version,
		"available": available,
		"enabled":   enabled,
	}}, nil
}

// listININames lists a directory's *.ini entries, stripped of the priority
// prefix Debian adds in conf.d ("20-gd.ini" -> "gd").
func listININames(c capability.Context, dir string) ([]string, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    lsPath,
		Args:    []string{"-1", "--", dir},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, errx.Upstream(err, "ext_list_failed", "Could not list PHP extensions.")
	}
	if res.ExitCode != 0 {
		// A version whose directory is missing has no extensions, which is an
		// answer rather than a fault.
		return []string{}, nil
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		name := strings.TrimSpace(line)
		if !strings.HasSuffix(name, ".ini") {
			continue
		}
		name = strings.TrimSuffix(name, ".ini")
		if i := strings.Index(name, "-"); i >= 0 && isAllDigits(name[:i]) {
			name = name[i+1:]
		}
		if name != "" {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
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

	// Config-test before reload, exactly as php.write_pool does: one master
	// serves every site on this version.
	res, err = c.Runner.Run(c.Ctx, exec.Command{
		Path:    fmt.Sprintf("/usr/sbin/php-fpm%s", in.Version),
		Args:    []string{"-t"},
		Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		// Put it back the way it was before reporting a failure.
		back := phpEnmodPath
		if in.Enabled {
			back = phpDismodPath
		}
		_, _ = c.Runner.Run(c.Ctx, exec.Command{
			Path:    back,
			Args:    []string{"-v", in.Version, "-s", phpFPMSAPI, in.Extension},
			Timeout: 30 * time.Second,
		})
		return capability.Result{}, errx.New(errx.KindValidation, "fpm_config_invalid",
			"PHP-FPM rejected the configuration with that extension change; it was rolled back.")
	}

	// A restart, not a reload. Extensions are loaded by the master when it execs;
	// SIGUSR2 re-reads pool config but does not re-link the extension into a
	// process that has already started. Reloading here would report success and
	// leave the extension exactly as it was — the same silent no-op this
	// capability exists to avoid.
	service := fmt.Sprintf("php%s-fpm", in.Version)
	res, err = c.Runner.Run(c.Ctx, exec.Command{
		Path:    systemctlPath,
		Args:    []string{"restart", service},
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
