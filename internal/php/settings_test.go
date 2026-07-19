package php_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// ── the injection guard ────────────────────────────────────────────────────

// The single most important test in this package.
//
// An ini value is written into the pool file as `php_admin_value[key] = value`.
// A value carrying a newline does not produce a wrong setting — it produces
// *extra pool directives*, and a pool file is where `user =` lives. "256M\nuser
// = root" would hand the site's PHP workers to root.
func TestINIValueCannotEscapeIntoAPoolDirective(t *testing.T) {
	escapes := []string{
		"3600\nuser = root",
		"3600\r\ngroup = root",
		"3600\nphp_admin_value[open_basedir] = /",
		"3600\ndisable_functions =",
		"3600\nlisten = /run/evil.sock",
		"3600\n[otherpool]",
	}
	for _, v := range escapes {
		s := php.DefaultSettings()
		s.INI = map[string]string{"max_execution_time": v}
		err := s.Validate()
		if err == nil {
			t.Errorf("Validate accepted a value that breaks out of its directive: %q", v)
			continue
		}
		if !errx.IsKind(err, errx.KindValidation) {
			t.Errorf("value %q: kind = %v, want validation", v, errx.KindOf(err))
		}
	}
}

// A ";" would comment out whatever the panel wrote after it — including the
// confinement block.
func TestINIValueCannotCommentOutWhatFollows(t *testing.T) {
	s := php.DefaultSettings()
	s.INI = map[string]string{"date.timezone": "UTC ; php_admin_value[open_basedir]=/"}
	if err := s.Validate(); err == nil {
		t.Fatal("Validate accepted a value containing \";\"")
	}
}

// The allowlist is the other half of the guard: the directives that *are* the
// site's confinement must not be settable at all.
func TestConfinementDirectivesAreNotSettable(t *testing.T) {
	forbidden := []string{
		"open_basedir",
		"disable_functions",
		"disable_classes",
		"extension",
		"extension_dir",
		"upload_tmp_dir",
		"session.save_path",
		"memory_limit", // first-class field; two sources of truth would be worse
	}
	for _, key := range forbidden {
		s := php.DefaultSettings()
		s.INI = map[string]string{key: "x"}
		if err := s.Validate(); err == nil {
			t.Errorf("Validate accepted %q, which the panel must own", key)
		}
	}
}

// Defence in depth: even a directive that somehow got past the allowlist must
// not be able to loosen the confinement, because the pool file is last-one-wins
// and the panel writes its block last.
func TestConfinementIsRenderedAfterOperatorOverrides(t *testing.T) {
	gw := &recordingGW{}
	svc := php.NewService(&fakePoolRepo{}, gw)

	s := php.DefaultSettings()
	s.INI = map[string]string{"max_execution_time": "30"}
	if _, err := svc.ApplySettings(context.Background(), poolReq(), s); err != nil {
		t.Fatalf("apply: %v", err)
	}
	cfg := gw.lastConfig(t)

	iOverride := strings.Index(cfg, "php_admin_value[max_execution_time]")
	iBasedir := strings.Index(cfg, "php_admin_value[open_basedir]")
	iMemory := strings.Index(cfg, "php_admin_value[memory_limit]")
	if iOverride < 0 || iBasedir < 0 || iMemory < 0 {
		t.Fatalf("expected directives missing:\n%s", cfg)
	}
	if iBasedir < iOverride {
		t.Errorf("open_basedir is rendered before the operator's overrides; a stray directive could loosen it")
	}
	if iMemory < iOverride {
		t.Errorf("memory_limit is rendered before the operator's overrides")
	}
}

// ── FPM sizing ─────────────────────────────────────────────────────────────

// php-fpm does not warn about these — with `dynamic` it refuses to start, and
// one site's numbers would take down every site sharing the PHP version.
func TestDynamicSizingRulesAreEnforced(t *testing.T) {
	cases := []struct {
		name string
		fpm  php.FPM
	}{
		{"min_spare above max_spare", php.FPM{PM: php.PMDynamic, MaxChildren: 10, StartServers: 5, MinSpareServers: 8, MaxSpareServers: 4}},
		{"max_spare above max_children", php.FPM{PM: php.PMDynamic, MaxChildren: 4, StartServers: 2, MinSpareServers: 1, MaxSpareServers: 9}},
		{"start below min_spare", php.FPM{PM: php.PMDynamic, MaxChildren: 10, StartServers: 1, MinSpareServers: 4, MaxSpareServers: 8}},
		{"start above max_spare", php.FPM{PM: php.PMDynamic, MaxChildren: 10, StartServers: 9, MinSpareServers: 2, MaxSpareServers: 6}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := php.DefaultSettings()
			s.FPM = tc.fpm
			if err := s.Validate(); err == nil {
				t.Fatalf("Validate accepted sizing php-fpm would refuse to start with: %+v", tc.fpm)
			}
		})
	}
}

func TestValidDynamicSizingIsAccepted(t *testing.T) {
	s := php.DefaultSettings()
	s.FPM = php.FPM{PM: php.PMDynamic, MaxChildren: 20, StartServers: 4, MinSpareServers: 2, MaxSpareServers: 8, MaxRequests: 500}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate rejected valid dynamic sizing: %v", err)
	}
}

// max_children × memory_limit is what one site can take from the node.
func TestMaxChildrenIsBounded(t *testing.T) {
	for _, n := range []int{-1, 501, 100000} {
		s := php.DefaultSettings()
		s.FPM.MaxChildren = n
		if err := s.Validate(); err == nil {
			t.Errorf("Validate accepted pm_max_children=%d", n)
		}
	}
}

func TestUnknownProcessManagerIsRejected(t *testing.T) {
	s := php.DefaultSettings()
	s.FPM.PM = "aggressive"
	if err := s.Validate(); err == nil {
		t.Fatal("Validate accepted an unknown process manager")
	}
}

// ── rendering ──────────────────────────────────────────────────────────────

// `dynamic` reads start/spare; `ondemand` reads the idle timeout. Emitting the
// wrong set is not cosmetic: php-fpm warns on directives that do not apply, and
// a missing one under `dynamic` is a startup failure.
func TestRenderEmitsOnlyTheDirectivesTheProcessManagerUses(t *testing.T) {
	t.Run("dynamic", func(t *testing.T) {
		cfg := renderWith(t, func(s *php.Settings) {
			s.FPM = php.FPM{PM: php.PMDynamic, MaxChildren: 20, StartServers: 4, MinSpareServers: 2, MaxSpareServers: 8, MaxRequests: 500}
		})
		for _, want := range []string{"pm = dynamic", "pm.start_servers = 4", "pm.min_spare_servers = 2", "pm.max_spare_servers = 8"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("missing %q:\n%s", want, cfg)
			}
		}
		if strings.Contains(cfg, "pm.process_idle_timeout") {
			t.Error("dynamic pool carries ondemand's idle timeout")
		}
	})
	t.Run("ondemand", func(t *testing.T) {
		cfg := renderWith(t, func(s *php.Settings) {
			s.FPM = php.FPM{PM: php.PMOnDemand, MaxChildren: 10, IdleTimeoutSec: 25, MaxRequests: 500}
		})
		if !strings.Contains(cfg, "pm.process_idle_timeout = 25s") {
			t.Errorf("missing idle timeout:\n%s", cfg)
		}
		if strings.Contains(cfg, "pm.start_servers") {
			t.Error("ondemand pool carries dynamic's start_servers")
		}
	})
	t.Run("static", func(t *testing.T) {
		cfg := renderWith(t, func(s *php.Settings) {
			s.FPM = php.FPM{PM: php.PMStatic, MaxChildren: 8, MaxRequests: 500}
		})
		if !strings.Contains(cfg, "pm = static") || !strings.Contains(cfg, "pm.max_children = 8") {
			t.Errorf("static pool wrong:\n%s", cfg)
		}
		if strings.Contains(cfg, "pm.start_servers") || strings.Contains(cfg, "pm.process_idle_timeout") {
			t.Error("static pool carries directives it does not use")
		}
	})
}

// Go randomizes map iteration; a pool file that reshuffles on every apply makes
// "what actually changed" impossible to read in a diff.
func TestRenderIsStableForIdenticalSettings(t *testing.T) {
	mk := func() string {
		return renderWith(t, func(s *php.Settings) {
			s.INI = map[string]string{
				"max_execution_time":  "30",
				"date.timezone":       "UTC",
				"upload_max_filesize": "64M",
				"post_max_size":       "64M",
				"display_errors":      "Off",
			}
		})
	}
	first := mk()
	for i := 0; i < 10; i++ {
		if got := mk(); got != first {
			t.Fatal("the rendered pool changes between identical applies")
		}
	}
}

func TestOPcacheRendersPerPool(t *testing.T) {
	cfg := renderWith(t, func(s *php.Settings) {
		s.OPcache = php.OPcache{Enabled: true, JIT: php.JITTracing}
	})
	if !strings.Contains(cfg, "php_admin_value[opcache.enable] = 1") {
		t.Errorf("opcache not enabled:\n%s", cfg)
	}
	if !strings.Contains(cfg, "php_admin_value[opcache.jit] = tracing") {
		t.Errorf("jit not set:\n%s", cfg)
	}
}

func TestOPcacheDisabledRendersZeroAndNoJIT(t *testing.T) {
	cfg := renderWith(t, func(s *php.Settings) {
		s.OPcache = php.OPcache{Enabled: false, JIT: php.JITOff}
	})
	if !strings.Contains(cfg, "php_admin_value[opcache.enable] = 0") {
		t.Errorf("opcache not disabled:\n%s", cfg)
	}
	if strings.Contains(cfg, "opcache.jit") {
		t.Errorf("jit emitted while off:\n%s", cfg)
	}
}

func TestUnknownJITModeIsRejected(t *testing.T) {
	s := php.DefaultSettings()
	s.OPcache.JIT = "turbo"
	if err := s.Validate(); err == nil {
		t.Fatal("Validate accepted an unknown JIT mode")
	}
}

// ── value typing ───────────────────────────────────────────────────────────

func TestINIValuesAreTypeChecked(t *testing.T) {
	bad := map[string]string{
		"max_execution_time":  "soon",
		"upload_max_filesize": "lots",
		"display_errors":      "maybe",
		"max_input_vars":      "1",       // below the floor
		"post_max_size":       "9999G",   // above the ceiling
		"realpath_cache_ttl":  "-5",      // below the floor
		"date.timezone":       "UTC\x00", // NUL
	}
	for key, value := range bad {
		s := php.DefaultSettings()
		s.INI = map[string]string{key: value}
		if err := s.Validate(); err == nil {
			t.Errorf("Validate accepted %s = %q", key, value)
		}
	}
}

func TestINIAcceptsWellFormedValues(t *testing.T) {
	s := php.DefaultSettings()
	s.INI = map[string]string{
		"max_execution_time":  "120",
		"upload_max_filesize": "256M",
		"post_max_size":       "256M",
		"max_input_vars":      "5000",
		"display_errors":      "Off",
		"date.timezone":       "Asia/Kolkata",
		"realpath_cache_size": "4M",
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate rejected ordinary settings: %v", err)
	}
}

func TestMemoryLimitIsBounded(t *testing.T) {
	for _, mb := range []int{1, 15, 8192, -10} {
		s := php.DefaultSettings()
		s.MemoryLimitMB = mb
		if err := s.Validate(); err == nil {
			t.Errorf("Validate accepted memory_limit_mb=%d", mb)
		}
	}
}

func TestAllowedINIKeysIsSortedAndExcludesConfinement(t *testing.T) {
	keys := php.AllowedINIKeys()
	if len(keys) == 0 {
		t.Fatal("no allowlisted directives")
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Fatalf("AllowedINIKeys is not sorted: %q before %q", keys[i-1], keys[i])
		}
	}
	for _, k := range keys {
		switch k {
		case "open_basedir", "disable_functions", "extension", "memory_limit":
			t.Errorf("AllowedINIKeys advertises %q, which the panel owns", k)
		}
	}
}

// ── settings persistence ───────────────────────────────────────────────────

// Moving a site between PHP versions must not silently reset a pool someone
// tuned to 40 workers and 1G back to the defaults.
func TestEnsurePoolKeepsExistingSettingsWhenChangingVersion(t *testing.T) {
	repo := &fakePoolRepo{}
	gw := &recordingGW{}
	svc := php.NewService(repo, gw)

	tuned := php.DefaultSettings()
	tuned.Version = "8.2"
	tuned.MemoryLimitMB = 1024
	tuned.FPM = php.FPM{PM: php.PMStatic, MaxChildren: 40, MaxRequests: 1000}
	tuned.INI = map[string]string{"max_execution_time": "300"}
	if _, err := svc.ApplySettings(context.Background(), poolReq(), tuned); err != nil {
		t.Fatalf("apply: %v", err)
	}

	req := poolReq()
	req.Version = "8.3"
	rec, err := svc.EnsurePool(context.Background(), req)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if rec.PHPVersion != "8.3" {
		t.Errorf("version = %q, want 8.3", rec.PHPVersion)
	}
	if rec.MemoryLimitMB != 1024 || rec.MaxChildren != 40 || rec.PM != php.PMStatic {
		t.Errorf("changing the version reset the tuning: %+v", rec)
	}
	if !strings.Contains(rec.INIOverrides, "max_execution_time") {
		t.Errorf("changing the version dropped the php.ini overrides: %s", rec.INIOverrides)
	}
	cfg := gw.lastConfig(t)
	if !strings.Contains(cfg, "pm.max_children = 40") {
		t.Errorf("rendered pool lost the tuning:\n%s", cfg)
	}
}

// A row the running server rejected must not be recorded: the panel would then
// report a configuration that does not exist.
func TestSettingsAreNotRecordedWhenTheBrokerRefuses(t *testing.T) {
	repo := &fakePoolRepo{}
	gw := &recordingGW{failOn: "php.write_pool"}
	svc := php.NewService(repo, gw)

	if _, err := svc.ApplySettings(context.Background(), poolReq(), php.DefaultSettings()); err == nil {
		t.Fatal("ApplySettings reported success despite the broker refusing")
	}
	if repo.upserts != 0 {
		t.Errorf("the pool was recorded %d times despite the broker refusing", repo.upserts)
	}
}

func TestSettingsRoundTripThroughTheRecord(t *testing.T) {
	repo := &fakePoolRepo{}
	svc := php.NewService(repo, &recordingGW{})

	want := php.DefaultSettings()
	want.MemoryLimitMB = 512
	want.FPM = php.FPM{PM: php.PMDynamic, MaxChildren: 12, StartServers: 3, MinSpareServers: 2, MaxSpareServers: 6, MaxRequests: 400}
	want.INI = map[string]string{"max_execution_time": "90", "date.timezone": "UTC"}
	want.OPcache = php.OPcache{Enabled: true, JIT: php.JITFunction}

	rec, err := svc.ApplySettings(context.Background(), poolReq(), want)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := php.SettingsOf(rec)

	if got.MemoryLimitMB != want.MemoryLimitMB || got.FPM != want.FPM || got.OPcache != want.OPcache {
		t.Errorf("round trip lost settings:\ngot  %+v\nwant %+v", got, want)
	}
	if got.INI["max_execution_time"] != "90" || got.INI["date.timezone"] != "UTC" {
		t.Errorf("round trip lost ini overrides: %+v", got.INI)
	}
}

// A row whose JSON will not parse must not stop the operator seeing their pool.
func TestSettingsOfToleratesUnparseableOverrides(t *testing.T) {
	rec := &php.PoolRecord{PHPVersion: "8.3", PM: php.PMOnDemand, MaxChildren: 10,
		MemoryLimitMB: 256, INIOverrides: "{not json", OPcacheEnabled: true}
	got := php.SettingsOf(rec)
	if got.INI == nil {
		t.Fatal("SettingsOf returned a nil INI map")
	}
	if len(got.INI) != 0 {
		t.Errorf("INI = %+v, want empty", got.INI)
	}
	if got.Version != "8.3" {
		t.Errorf("the rest of the record was lost: %+v", got)
	}
}
