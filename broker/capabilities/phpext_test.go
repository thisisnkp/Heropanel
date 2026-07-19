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

func TestPHPSetExtensionEnablesForFPMOnly(t *testing.T) {
	// enmod (ok), php-fpm -t (ok), systemctl restart (ok).
	fr := &exec.FakeRunner{}
	res, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
	if _, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
	_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
		_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
	_, err := (capabilities.PHPSetExtension{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
	res, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
	if _, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
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
	_, err := (capabilities.PHPListExtensions{}).Execute(sliceCtx(fr, fsys.NewFake()), []byte(`{bad`))
	if err == nil || !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}
