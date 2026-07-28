package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// argvOf joins a command for easy matching.
func argvOf(c exec.Command) string { return c.Path + " " + strings.Join(c.Args, " ") }

// debianFS returns a fake FS on which phpenmod is present, so detectPHPFamily
// picks the Debian layout (the default the original tests assume).
func debianFS() *fsys.Fake {
	f := fsys.NewFake()
	_ = f.WriteFile("/usr/sbin/phpenmod", nil, 0o755)
	return f
}

func TestPHPSetExtensionEnablesForFPMOnly(t *testing.T) {
	// enmod (ok), php-fpm -t (ok), systemctl restart (ok).
	fr := &exec.FakeRunner{}
	res, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
		"version": "8.3", "extension": "gd", "enabled": true,
	}))
	if err != nil {
		t.Fatalf("set_extension: %v", err)
	}
	if len(fr.Calls) != 3 {
		t.Fatalf("got %d commands, want enmod + test + restart", len(fr.Calls))
	}

	// -s fpm is what keeps this from also changing the CLI SAPI, which would
	// alter what a site's own `php` command sees as a side effect.
	if got := argvOf(fr.Calls[0]); got != "/usr/sbin/phpenmod -v 8.3 -s fpm gd" {
		t.Errorf("enmod argv = %q", got)
	}
	// A restart, not a reload: an extension is linked into the master at exec
	// time, so SIGUSR2 would report success and load nothing.
	if got := argvOf(fr.Calls[2]); !strings.HasSuffix(got, "systemctl restart php8.3-fpm") {
		t.Errorf("restart argv = %q, want a restart not a reload", got)
	}
	if res.Data["enabled"] != true {
		t.Errorf("result = %+v", res.Data)
	}
}

func TestPHPSetExtensionDisableUsesDismod(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
		"version": "8.3", "extension": "xdebug", "enabled": false,
	})); err != nil {
		t.Fatalf("set_extension: %v", err)
	}
	if got := argvOf(fr.Calls[0]); got != "/usr/sbin/phpdismod -v 8.3 -s fpm xdebug" {
		t.Errorf("argv = %q, want phpdismod", got)
	}
}

// One FPM master serves every site on a version. A config that fails -t must be
// rolled back before it can take the master down on the next restart.
func TestPHPSetExtensionRollsBackWhenConfigTestFails(t *testing.T) {
	n := 0
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		n++
		if n == 2 { // the php-fpm -t
			return exec.Result{ExitCode: 1, Stderr: []byte("failed to load")}, nil
		}
		return exec.Result{}, nil
	}}
	_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
		"version": "8.3", "extension": "gd", "enabled": true,
	}))
	if err == nil {
		t.Fatal("set_extension reported success despite a failing config test")
	}
	// The rollback must have re-run the opposite tool, and the master must NOT
	// have been restarted onto a config that would not load.
	if len(fr.Calls) != 3 {
		t.Fatalf("got %d commands, want enmod + failing test + rollback dismod", len(fr.Calls))
	}
	if got := argvOf(fr.Calls[2]); got != "/usr/sbin/phpdismod -v 8.3 -s fpm gd" {
		t.Errorf("rollback argv = %q, want the opposite of the change", got)
	}
	for _, c := range fr.Calls {
		if strings.Contains(argvOf(c), "systemctl restart") {
			t.Error("FPM was restarted onto a config that failed the test")
		}
	}
}

func TestPHPSetExtensionRejectsBadExtensionNames(t *testing.T) {
	for _, name := range []string{"", "../evil", "gd; rm -rf /", "GD", "a b", strings.Repeat("x", 40)} {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
			"version": "8.3", "extension": name, "enabled": true,
		}))
		if err == nil {
			t.Errorf("accepted extension name %q", name)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("name %q reached the runner", name)
		}
	}
}

func TestPHPSetExtensionValidatesVersion(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
		"version": "8.3; rm", "extension": "gd", "enabled": true,
	}))
	if err == nil {
		t.Fatal("accepted an invalid version")
	}
	if len(fr.Calls) != 0 {
		t.Error("the runner was called for an invalid version")
	}
}

func TestPHPListExtensionsReadsFPMConfD(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		// available (mods-available) then enabled (fpm/conf.d).
		if strings.Contains(strings.Join(c.Args, " "), "mods-available") {
			return exec.Result{Stdout: []byte("gd.ini\nxdebug.ini\nopcache.ini\n")}, nil
		}
		return exec.Result{Stdout: []byte("10-opcache.ini\n20-gd.ini\n")}, nil
	}}
	res, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
		"version": "8.3",
	}))
	if err != nil {
		t.Fatalf("list_extensions: %v", err)
	}

	avail, _ := res.Data["available"].([]string)
	enabled, _ := res.Data["enabled"].([]string)
	if strings.Join(avail, ",") != "gd,opcache,xdebug" {
		t.Errorf("available = %v, want sorted gd,opcache,xdebug", avail)
	}
	// The priority prefix ("20-") must be stripped so the two lists are
	// comparable — otherwise "gd" and "20-gd" would never match.
	if strings.Join(enabled, ",") != "gd,opcache" {
		t.Errorf("enabled = %v, want gd,opcache with the prefixes stripped", enabled)
	}
}

func TestPHPListExtensionsIsNotFooledByTheCLISAPI(t *testing.T) {
	// The enabled list must come from fpm/conf.d, never from `php -m` (the CLI).
	var listed []string
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		listed = append(listed, argvOf(c))
		return exec.Result{Stdout: []byte("")}, nil
	}}
	if _, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, debianFS()), raw(t, map[string]any{
		"version": "8.3",
	})); err != nil {
		t.Fatalf("list_extensions: %v", err)
	}
	for _, cmd := range listed {
		if strings.Contains(cmd, "php -m") || strings.Contains(cmd, "php8.3 -m") {
			t.Errorf("the enabled list was read from the CLI SAPI: %q", cmd)
		}
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d directories, want mods-available + fpm/conf.d", len(listed))
	}
	if !strings.Contains(listed[1], "fpm/conf.d") {
		t.Errorf("the enabled list did not come from fpm/conf.d: %q", listed[1])
	}
}

func TestPHPListExtensionsBadInput(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, debianFS()), []byte(`{bad`))
	if err == nil || !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

// ── Rocky/Alma (Remi SCL) layout ─────────────────────────────────────────────
// With no phpenmod present, detectPHPFamily picks the RHEL layout: /etc/opt/remi
// paths, a php<vv>-php-fpm service, and extensions toggled by renaming the ini
// file in the flat php.d rather than via phpenmod.

func TestPHPSetExtensionRHELTogglesIniFile(t *testing.T) {
	// A plain fake FS (no phpenmod) => RHEL. The php.d listing reports gd as a
	// disabled ini, so enabling it renames the file back to .ini.
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if c.Path == "/bin/ls" {
			return exec.Result{Stdout: []byte("20-gd.ini.disabled\n30-opcache.ini\n")}, nil
		}
		return exec.Result{}, nil
	}}
	res, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"version": "8.3", "extension": "gd", "enabled": true,
	}))
	if err != nil {
		t.Fatalf("set_extension (RHEL): %v", err)
	}
	// ls (find the file) + mv (enable) + php-fpm -t + systemctl restart.
	if len(fr.Calls) != 4 {
		t.Fatalf("got %d commands, want ls + mv + test + restart", len(fr.Calls))
	}
	if got := argvOf(fr.Calls[1]); got != "/bin/mv -f -- /etc/opt/remi/php83/php.d/20-gd.ini.disabled /etc/opt/remi/php83/php.d/20-gd.ini" {
		t.Errorf("mv argv = %q, want the .disabled suffix removed", got)
	}
	// The config test uses the SCL php-fpm binary, and the restart the SCL unit.
	if got := fr.Calls[2].Path; got != "/opt/remi/php83/root/usr/sbin/php-fpm" {
		t.Errorf("config-test binary = %q, want the Remi SCL php-fpm", got)
	}
	if got := argvOf(fr.Calls[3]); !strings.HasSuffix(got, "systemctl restart php83-php-fpm") {
		t.Errorf("restart argv = %q, want the SCL unit", got)
	}
	if res.Data["enabled"] != true {
		t.Errorf("result = %+v", res.Data)
	}
}

func TestPHPSetExtensionRHELDisableAddsDisabledSuffix(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if c.Path == "/bin/ls" {
			return exec.Result{Stdout: []byte("20-xdebug.ini\n")}, nil
		}
		return exec.Result{}, nil
	}}
	if _, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"version": "8.3", "extension": "xdebug", "enabled": false,
	})); err != nil {
		t.Fatalf("set_extension (RHEL disable): %v", err)
	}
	if got := argvOf(fr.Calls[1]); got != "/bin/mv -f -- /etc/opt/remi/php83/php.d/20-xdebug.ini /etc/opt/remi/php83/php.d/20-xdebug.ini.disabled" {
		t.Errorf("mv argv = %q, want a .disabled suffix added", got)
	}
}

func TestPHPSetExtensionRHELMissingExtensionFails(t *testing.T) {
	// The php.d has no such ini — the extension is not installed; nothing moves.
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if c.Path == "/bin/ls" {
			return exec.Result{Stdout: []byte("20-gd.ini\n")}, nil
		}
		return exec.Result{}, nil
	}}
	_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"version": "8.3", "extension": "imagick", "enabled": true,
	}))
	if err == nil {
		t.Fatal("enabling an uninstalled extension must fail")
	}
	for _, c := range fr.Calls {
		if c.Path == "/bin/mv" || strings.Contains(argvOf(c), "systemctl") {
			t.Errorf("no move or restart may happen: %q", argvOf(c))
		}
	}
}

func TestPHPListExtensionsRHELFlatPHPD(t *testing.T) {
	// One flat php.d holds both enabled (.ini) and disabled (.ini.disabled).
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		return exec.Result{Stdout: []byte("20-gd.ini\n30-opcache.ini\n40-xdebug.ini.disabled\n")}, nil
	}}
	res, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"version": "8.3",
	}))
	if err != nil {
		t.Fatalf("list_extensions (RHEL): %v", err)
	}
	avail, _ := res.Data["available"].([]string)
	enabled, _ := res.Data["enabled"].([]string)
	// Available includes the disabled one; enabled does not.
	if strings.Join(avail, ",") != "gd,opcache,xdebug" {
		t.Errorf("available = %v, want gd,opcache,xdebug (disabled included)", avail)
	}
	if strings.Join(enabled, ",") != "gd,opcache" {
		t.Errorf("enabled = %v, want gd,opcache (xdebug is .disabled)", enabled)
	}
}
