package capabilities

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// PHP packaging differs between the two distro families the panel supports, and
// every path, service name and extension mechanism differs with it. Rather than
// scatter `if debian` through the pool and extension capabilities, one small
// resolver owns the differences; the capabilities ask it for a path and stay
// layout-agnostic.
//
//   - **Debian/Ubuntu** (Sury's php packages): versioned trees under
//     /etc/php/<v>/, the php-common phpenmod/phpdismod tools toggling symlinks
//     from fpm/conf.d into mods-available, and a `php<v>-fpm` service per version.
//   - **Rocky/Alma** via the **Remi SCL** packages — the standard way to run
//     *multiple* PHP versions on RHEL (base `dnf module` gives exactly one, which
//     does not fit a panel where version is per-site): trees under
//     /etc/opt/remi/php<vv>/ with a flat php.d, a `php<vv>-php-fpm` service, and
//     no phpenmod — an extension is enabled by the presence of its `.ini` in
//     php.d, so the panel toggles that file (renaming to `.ini.disabled` to
//     disable, which keeps the file so it can be turned back on).
type phpFamily int

const (
	phpDebian phpFamily = iota
	phpRHEL
)

// detectPHPFamily picks the layout from what is actually installed. The Debian
// php-common tool phpenmod is the reliable tell; its absence means the Remi SCL
// layout. Read from the filesystem each call, so a host is followed as it is.
func detectPHPFamily(c capability.Context) phpFamily {
	if ok, _ := c.FS.Exists(phpEnmodPath); ok {
		return phpDebian
	}
	return phpRHEL
}

// remiTag turns a major.minor version into the Remi package tag ("8.3" -> "83").
func remiTag(version string) string { return strings.ReplaceAll(version, ".", "") }

// poolDir is where a version's per-site FPM pool configs live.
func (f phpFamily) poolDir(v string) string {
	if f == phpRHEL {
		return fmt.Sprintf("/etc/opt/remi/php%s/php-fpm.d", remiTag(v))
	}
	return fmt.Sprintf("/etc/php/%s/fpm/pool.d", v)
}

// fpmBinary is the php-fpm binary used for `-t` config tests. On RHEL the SCL
// binary reads its own /etc/opt/remi/php<vv>/php-fpm.conf by default.
func (f phpFamily) fpmBinary(v string) string {
	if f == phpRHEL {
		return fmt.Sprintf("/opt/remi/php%s/root/usr/sbin/php-fpm", remiTag(v))
	}
	return fmt.Sprintf("/usr/sbin/php-fpm%s", v)
}

// fpmService is the systemd unit name for a version's FPM master.
func (f phpFamily) fpmService(v string) string {
	if f == phpRHEL {
		return fmt.Sprintf("php%s-php-fpm", remiTag(v))
	}
	return fmt.Sprintf("php%s-fpm", v)
}

// availDir is scanned for the extensions that exist for a version.
func (f phpFamily) availDir(v string) string {
	if f == phpRHEL {
		return fmt.Sprintf("/etc/opt/remi/php%s/php.d", remiTag(v))
	}
	return fmt.Sprintf("%s/%s/%s", phpPoolBase, v, phpModsAvailDir)
}

// enabledDir is scanned for the extensions currently enabled for the FPM SAPI.
// On RHEL this is the same flat php.d (filtered to non-disabled files); on Debian
// it is the FPM SAPI's own conf.d of symlinks.
func (f phpFamily) enabledDir(v string) string {
	if f == phpRHEL {
		return fmt.Sprintf("/etc/opt/remi/php%s/php.d", remiTag(v))
	}
	return fmt.Sprintf("%s/%s/%s/conf.d", phpPoolBase, v, phpFPMSAPI)
}

// opcacheININPath is the version-wide ini the panel owns for the PHP_INI_SYSTEM
// OPcache directives (shared-memory sizes), loaded by the FPM master at startup.
// It lives in the FPM SAPI's conf.d (Debian) / flat php.d (RHEL) so it applies to
// the whole version, not one pool. The 99- prefix makes it win over distro inis.
func (f phpFamily) opcacheININPath(v string) string {
	return f.enabledDir(v) + "/99-heropanel-opcache.ini"
}

// rawINIList returns the raw *.ini / *.ini.disabled filenames in dir. A missing
// directory is an empty list, not an error.
func rawINIList(c capability.Context, dir string) ([]string, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    lsPath,
		Args:    []string{"-1", "--", dir},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, errx.Upstream(err, "ext_list_failed", "Could not list PHP extensions.")
	}
	if res.ExitCode != 0 {
		return []string{}, nil
	}
	var out []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		n := strings.TrimSpace(line)
		if strings.HasSuffix(n, ".ini") || strings.HasSuffix(n, ".ini.disabled") {
			out = append(out, n)
		}
	}
	return out, nil
}

// extNameFromINI strips a Debian priority prefix ("20-gd") and any ".disabled"
// suffix, yielding the bare extension name.
func extNameFromINI(fname string) string {
	n := strings.TrimSuffix(fname, ".disabled")
	n = strings.TrimSuffix(n, ".ini")
	if i := strings.Index(n, "-"); i >= 0 && isAllDigits(n[:i]) {
		n = n[i+1:]
	}
	return n
}

// listExtNames lists the extension names in dir. When includeDisabled is false,
// `.ini.disabled` files are skipped (the "enabled" set on the flat RHEL php.d).
func listExtNames(c capability.Context, dir string, includeDisabled bool) ([]string, error) {
	raw, err := rawINIList(c, dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range raw {
		if !includeDisabled && strings.HasSuffix(f, ".disabled") {
			continue
		}
		if name := extNameFromINI(f); name != "" {
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

// toggleExtRHEL enables/disables an extension on the RHEL/Remi flat php.d by
// renaming its ini file between `.ini` and `.ini.disabled`. Returns whether a
// change was actually made (false = already in the requested state), and errors
// if the extension is not installed at all.
func toggleExtRHEL(c capability.Context, version, ext string, enable bool) (bool, error) {
	dir := phpRHEL.availDir(version)
	raw, err := rawINIList(c, dir)
	if err != nil {
		return false, err
	}
	var current string
	for _, f := range raw {
		if extNameFromINI(f) == ext {
			current = f
			break
		}
	}
	if current == "" {
		return false, errx.New(errx.KindValidation, "ext_toggle_failed",
			"PHP did not accept that extension change; the extension may not be installed.")
	}
	src := dir + "/" + current
	disabled := strings.HasSuffix(current, ".disabled")
	if enable && !disabled {
		return false, nil // already enabled
	}
	if !enable && disabled {
		return false, nil // already disabled
	}
	var dst string
	if enable {
		dst = strings.TrimSuffix(src, ".disabled")
	} else {
		dst = src + ".disabled"
	}
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    mvPath,
		Args:    []string{"-f", "--", src, dst},
		Timeout: 15 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return false, errx.New(errx.KindUpstream, "ext_toggle_failed", "Could not change the PHP extension.")
	}
	return true, nil
}
