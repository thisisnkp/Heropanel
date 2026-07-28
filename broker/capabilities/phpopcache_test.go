package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
)

const debianOpcachePath = "/etc/php/8.3/fpm/conf.d/99-heropanel-opcache.ini"

func TestPHPWriteOpcacheTestsThenRestarts(t *testing.T) {
	fs := debianFS()
	fr := &exec.FakeRunner{}
	_, err := (capabilities.PHPWriteOpcache{}).Execute(sliceCtx(fr, fs), raw(t, map[string]any{
		"version": "8.3", "config": "opcache.memory_consumption=256\n",
	}))
	if err != nil {
		t.Fatalf("write_opcache: %v", err)
	}
	if len(fr.Calls) != 2 {
		t.Fatalf("got %d commands, want php-fpm -t + systemctl restart", len(fr.Calls))
	}
	if got := fr.Calls[0].Path; got != "/usr/sbin/php-fpm8.3" {
		t.Errorf("config-test binary = %q", got)
	}
	if got := argvOf(fr.Calls[1]); !strings.HasSuffix(got, "systemctl restart php8.3-fpm") {
		t.Errorf("restart argv = %q, want a restart (SYSTEM settings need a fresh master)", got)
	}
	if b, err := fs.ReadFile(debianOpcachePath); err != nil || !strings.Contains(string(b), "memory_consumption=256") {
		t.Errorf("the version-wide ini was not written: %v / %s", err, b)
	}
}

func TestPHPWriteOpcacheRollsBackOnBadConfig(t *testing.T) {
	fs := debianFS()
	fr := &exec.FakeRunner{Fn: func(c exec.Command) (exec.Result, error) {
		if strings.Contains(c.Path, "php-fpm") { // the -t
			return exec.Result{ExitCode: 1, Stderr: []byte("bad")}, nil
		}
		return exec.Result{}, nil
	}}
	_, err := (capabilities.PHPWriteOpcache{}).Execute(sliceCtx(fr, fs), raw(t, map[string]any{
		"version": "8.3", "config": "opcache.memory_consumption=nonsense\n",
	}))
	if err == nil {
		t.Fatal("a failing config test must fail the write")
	}
	// The file must be removed again (there was no prior), and FPM never restarted.
	if _, err := fs.ReadFile(debianOpcachePath); err == nil {
		t.Error("the bad ini was left on disk after rollback")
	}
	for _, c := range fr.Calls {
		if strings.Contains(argvOf(c), "systemctl restart") {
			t.Error("FPM was restarted onto a config that failed the test")
		}
	}
}

func TestPHPReadOpcacheReturnsContentOrEmpty(t *testing.T) {
	fs := debianFS()
	fr := &exec.FakeRunner{}
	// Nothing written yet => empty, not an error.
	res, err := (capabilities.PHPReadOpcache{}).Execute(sliceCtx(fr, fs), raw(t, map[string]any{"version": "8.3"}))
	if err != nil {
		t.Fatalf("read_opcache (absent): %v", err)
	}
	if res.Data["config"] != "" {
		t.Errorf("absent file should read as empty, got %q", res.Data["config"])
	}
	// After a write, read returns the content.
	_ = fs.WriteFile(debianOpcachePath, []byte("opcache.memory_consumption=512\n"), 0o644)
	res, err = (capabilities.PHPReadOpcache{}).Execute(sliceCtx(fr, fs), raw(t, map[string]any{"version": "8.3"}))
	if err != nil {
		t.Fatalf("read_opcache: %v", err)
	}
	if !strings.Contains(res.Data["config"].(string), "512") {
		t.Errorf("read did not return the written content: %q", res.Data["config"])
	}
}
