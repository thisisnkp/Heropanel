package php

import (
	"sort"
	"strconv"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// ── FPM process manager ─────────────────────────────────────────────────────

// Process managers PHP-FPM supports.
const (
	PMStatic   = "static"
	PMDynamic  = "dynamic"
	PMOnDemand = "ondemand"
)

// FPM bounds. max_children is the one that decides how much memory a site can
// take: each worker holds up to memory_limit, so 500 × 256M is 128G and the node
// is gone. The ceiling is a guard rail, not a recommendation.
const (
	MinChildren     = 1
	MaxChildren     = 500
	MaxRequestsMax  = 100000
	MaxIdleTimeout  = 3600
	MinIdleTimeout  = 1
	DefaultChildren = 10
)

// FPM is a pool's process-manager configuration.
type FPM struct {
	PM              string `json:"pm"`
	MaxChildren     int    `json:"pm_max_children"`
	StartServers    int    `json:"pm_start_servers"`
	MinSpareServers int    `json:"pm_min_spare_servers"`
	MaxSpareServers int    `json:"pm_max_spare_servers"`
	MaxRequests     int    `json:"pm_max_requests"`
	IdleTimeoutSec  int    `json:"pm_idle_timeout_sec"`
}

// DefaultFPM reproduces what the pool template hardcoded before this was
// configurable, so a site that has never been tuned behaves as it always did.
func DefaultFPM() FPM {
	return FPM{
		PM: PMOnDemand, MaxChildren: DefaultChildren, StartServers: 2,
		MinSpareServers: 1, MaxSpareServers: 3, MaxRequests: 500, IdleTimeoutSec: 10,
	}
}

// validateFPM checks the sizing.
//
// This is stricter than it looks because php-fpm does not merely warn about a
// bad pool: with `dynamic` and min_spare > max_spare, or start_servers outside
// that range, the master **refuses to start**. One site's bad numbers would take
// down every site sharing that PHP version. The broker config-tests before
// reloading for the same reason; this is the half that gives the operator a
// useful message instead of "reload failed".
func validateFPM(f *FPM) error {
	switch f.PM {
	case "":
		f.PM = PMOnDemand
	case PMStatic, PMDynamic, PMOnDemand:
	default:
		return errx.Validation("bad_pm", "Process manager must be \"static\", \"dynamic\", or \"ondemand\".",
			errx.Field{Field: "pm", Code: "unsupported", Message: "unknown process manager"})
	}
	if f.MaxChildren == 0 {
		f.MaxChildren = DefaultChildren
	}
	if f.MaxChildren < MinChildren || f.MaxChildren > MaxChildren {
		return errx.Validation("bad_max_children",
			"pm_max_children must be between 1 and "+strconv.Itoa(MaxChildren)+".",
			errx.Field{Field: "pm_max_children", Code: "out_of_range", Message: "out of range"})
	}
	if f.MaxRequests < 0 || f.MaxRequests > MaxRequestsMax {
		return errx.Validation("bad_max_requests",
			"pm_max_requests must be between 0 and "+strconv.Itoa(MaxRequestsMax)+".",
			errx.Field{Field: "pm_max_requests", Code: "out_of_range", Message: "out of range"})
	}

	switch f.PM {
	case PMDynamic:
		if f.StartServers == 0 {
			f.StartServers = 2
		}
		if f.MinSpareServers == 0 {
			f.MinSpareServers = 1
		}
		if f.MaxSpareServers == 0 {
			f.MaxSpareServers = 3
		}
		// php-fpm's own rules, enforced here so the operator gets told which
		// number is wrong rather than watching every PHP site on the version die.
		if f.MinSpareServers < 1 {
			return errx.Validation("bad_min_spare", "pm_min_spare_servers must be at least 1 for \"dynamic\".",
				errx.Field{Field: "pm_min_spare_servers", Code: "out_of_range", Message: "must be >= 1"})
		}
		if f.MinSpareServers > f.MaxSpareServers {
			return errx.Validation("bad_spare_range",
				"pm_min_spare_servers cannot exceed pm_max_spare_servers.",
				errx.Field{Field: "pm_min_spare_servers", Code: "out_of_range", Message: "greater than max_spare"})
		}
		if f.MaxSpareServers > f.MaxChildren {
			return errx.Validation("bad_spare_range",
				"pm_max_spare_servers cannot exceed pm_max_children.",
				errx.Field{Field: "pm_max_spare_servers", Code: "out_of_range", Message: "greater than max_children"})
		}
		if f.StartServers < f.MinSpareServers || f.StartServers > f.MaxSpareServers {
			return errx.Validation("bad_start_servers",
				"pm_start_servers must be between pm_min_spare_servers and pm_max_spare_servers.",
				errx.Field{Field: "pm_start_servers", Code: "out_of_range", Message: "outside the spare range"})
		}
	case PMOnDemand:
		if f.IdleTimeoutSec == 0 {
			f.IdleTimeoutSec = 10
		}
		if f.IdleTimeoutSec < MinIdleTimeout || f.IdleTimeoutSec > MaxIdleTimeout {
			return errx.Validation("bad_idle_timeout",
				"pm_idle_timeout_sec must be between 1 and "+strconv.Itoa(MaxIdleTimeout)+".",
				errx.Field{Field: "pm_idle_timeout_sec", Code: "out_of_range", Message: "out of range"})
		}
	}
	return nil
}

// ── OPcache ────────────────────────────────────────────────────────────────

// JIT modes exposed. PHP's opcache.jit takes a four-digit CRTO string; these are
// the two that are worth offering plus off, mapped to the digits in renderINI.
const (
	JITOff      = "off"
	JITTracing  = "tracing"
	JITFunction = "function"
)

// OPcache is the per-site half of OPcache.
//
// Only the half. opcache.enable and opcache.jit are PHP_INI_ALL, so a pool can
// set them per site. memory_consumption, jit_buffer_size and
// max_accelerated_files are PHP_INI_SYSTEM: the FPM master allocates that shared
// memory once, at startup, before any pool exists. They belong to the PHP
// *version*, and offering them per site would be a setting that silently does
// nothing — which is worse than not offering it.
type OPcache struct {
	Enabled bool   `json:"enabled"`
	JIT     string `json:"jit"`
}

// DefaultOPcache enables the cache with JIT off — PHP's own default posture, and
// the right one: OPcache is a straight win, JIT is a workload-dependent bet.
func DefaultOPcache() OPcache { return OPcache{Enabled: true, JIT: JITOff} }

func validateOPcache(o *OPcache) error {
	switch o.JIT {
	case "":
		o.JIT = JITOff
	case JITOff, JITTracing, JITFunction:
	default:
		return errx.Validation("bad_jit", "JIT must be \"off\", \"tracing\", or \"function\".",
			errx.Field{Field: "jit", Code: "unsupported", Message: "unknown JIT mode"})
	}
	return nil
}

// ── php.ini directives ─────────────────────────────────────────────────────

type iniKind int

const (
	iniInt iniKind = iota
	iniSize
	iniOnOff
	iniString
)

type iniSpec struct {
	kind iniKind
	min  int64 // iniInt/iniSize: inclusive bounds (bytes for iniSize)
	max  int64
}

// iniAllowlist is the complete set of directives an operator may set.
//
// It is an allowlist, and that is a security control rather than a matter of
// taste. Every directive here is rendered into the site's FPM pool file, which
// is *also* the file that confines the site: open_basedir, disable_functions and
// the tmp/session paths live there. A "free-text php.ini editor" would let
// whoever holds site.write on one site set open_basedir=/ and read every other
// customer on the box — and disable_functions= to get exec() back. Those
// directives are absent here, and renderINI additionally emits the panel's own
// confinement *after* any override so the last-one-wins rule in a pool file
// cannot be turned against us.
//
// `memory_limit` is deliberately **not** here even though it is the directive
// operators reach for first. It already exists as php_pools.memory_limit_mb, a
// first-class field, and having it settable from two places would mean two
// sources of truth with no answer to "which wins". It is also not really a
// preference in a hosting panel: memory_limit × pm_max_children is the ceiling
// on what one site can take from the node, which makes it a plan dimension in
// the same family as the cgroup limits, not a php.ini tweak.
var iniAllowlist = map[string]iniSpec{
	"max_execution_time":      {kind: iniInt, min: 0, max: 3600},
	"max_input_time":          {kind: iniInt, min: -1, max: 3600},
	"max_input_vars":          {kind: iniInt, min: 100, max: 100000},
	"post_max_size":           {kind: iniSize, min: 0, max: 4 << 30},
	"upload_max_filesize":     {kind: iniSize, min: 0, max: 4 << 30},
	"max_file_uploads":        {kind: iniInt, min: 0, max: 1000},
	"default_socket_timeout":  {kind: iniInt, min: 1, max: 3600},
	"session.gc_maxlifetime":  {kind: iniInt, min: 60, max: 604800},
	"realpath_cache_size":     {kind: iniSize, min: 16 << 10, max: 32 << 20},
	"realpath_cache_ttl":      {kind: iniInt, min: 0, max: 86400},
	"display_errors":          {kind: iniOnOff},
	"display_startup_errors":  {kind: iniOnOff},
	"log_errors":              {kind: iniOnOff},
	"expose_php":              {kind: iniOnOff},
	"allow_url_fopen":         {kind: iniOnOff},
	"zlib.output_compression": {kind: iniOnOff},
	"short_open_tag":          {kind: iniOnOff},
	"date.timezone":           {kind: iniString},
	"error_reporting":         {kind: iniString},
	"default_charset":         {kind: iniString},
}

// AllowedINIKeys returns the settable directives, sorted. The UI renders the
// editor from this rather than hardcoding a list that would drift.
func AllowedINIKeys() []string {
	out := make([]string, 0, len(iniAllowlist))
	for k := range iniAllowlist {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var reSize = func(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	mult := int64(1)
	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M', 'm':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G', 'g':
		mult, s = 1<<30, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * mult, true
}

// validateINIValue checks one directive's value against its spec.
//
// The newline check is the one that matters most, and it applies to every kind.
// A value is written into the pool file as `php_admin_value[key] = value`; a
// value containing a newline does not produce a bad setting, it produces *extra
// pool directives*. `256M\nuser = root` would hand the site's workers to root.
// The allowlist bounds what can be set; this is what stops the value escaping
// the setting entirely.
func validateINIValue(key, value string) error {
	spec, ok := iniAllowlist[key]
	if !ok {
		return errx.Validation("ini_not_allowed",
			"\""+key+"\" is not a directive this panel lets you set.",
			errx.Field{Field: key, Code: "not_allowed", Message: "not in the allowlist"})
	}
	if strings.ContainsAny(value, "\n\r") {
		return errx.Validation("ini_bad_value",
			"A php.ini value cannot contain a line break.",
			errx.Field{Field: key, Code: "invalid", Message: "line break in value"})
	}
	if len(value) > 256 {
		return errx.Validation("ini_bad_value", "A php.ini value cannot exceed 256 characters.",
			errx.Field{Field: key, Code: "too_long", Message: "too long"})
	}
	// A value ending the line early would comment out or hide what follows it.
	if strings.ContainsAny(value, ";\x00") {
		return errx.Validation("ini_bad_value", "A php.ini value cannot contain \";\" or a NUL.",
			errx.Field{Field: key, Code: "invalid", Message: "illegal character"})
	}

	switch spec.kind {
	case iniInt:
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return errx.Validation("ini_bad_value", "\""+key+"\" must be a whole number.",
				errx.Field{Field: key, Code: "invalid", Message: "not an integer"})
		}
		if n < spec.min || n > spec.max {
			return errx.Validation("ini_bad_value",
				"\""+key+"\" must be between "+strconv.FormatInt(spec.min, 10)+" and "+strconv.FormatInt(spec.max, 10)+".",
				errx.Field{Field: key, Code: "out_of_range", Message: "out of range"})
		}
	case iniSize:
		n, ok := reSize(value)
		if !ok {
			return errx.Validation("ini_bad_value",
				"\""+key+"\" must be a size like \"256M\" or \"1G\".",
				errx.Field{Field: key, Code: "invalid", Message: "not a size"})
		}
		if n < spec.min || n > spec.max {
			return errx.Validation("ini_bad_value",
				"\""+key+"\" is outside the range this panel allows.",
				errx.Field{Field: key, Code: "out_of_range", Message: "out of range"})
		}
	case iniOnOff:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "on", "off", "1", "0", "true", "false", "yes", "no":
		default:
			return errx.Validation("ini_bad_value", "\""+key+"\" must be On or Off.",
				errx.Field{Field: key, Code: "invalid", Message: "not a boolean"})
		}
	case iniString:
		// Already newline/;/NUL-checked above. Keep it to characters that can
		// appear in a timezone, a charset, or an error_reporting expression.
		for _, r := range value {
			if r < 0x20 || r > 0x7e {
				return errx.Validation("ini_bad_value", "\""+key+"\" contains an unsupported character.",
					errx.Field{Field: key, Code: "invalid", Message: "non-printable character"})
			}
		}
	}
	return nil
}

// validateINI checks every override.
func validateINI(overrides map[string]string) error {
	if len(overrides) > len(iniAllowlist) {
		return errx.Validation("ini_too_many", "Too many php.ini overrides.")
	}
	for k, v := range overrides {
		if err := validateINIValue(k, v); err != nil {
			return err
		}
	}
	return nil
}

// ── the settings envelope ──────────────────────────────────────────────────

// Memory-limit bounds. The floor is where PHP itself stops being able to boot a
// framework; the ceiling is per worker, so it is multiplied by pm_max_children
// when you ask what a site can cost the node.
const (
	MinMemoryLimitMB     = 16
	MaxMemoryLimitMB     = 4096
	DefaultMemoryLimitMB = 256
)

// Settings is everything about a site's PHP that an operator can change.
//
// It is one envelope and one apply, because it is one file: all of it renders
// into the site's FPM pool, and the pool is written and reloaded atomically. A
// partial apply here would leave a config on disk that no field in the database
// describes.
type Settings struct {
	Version       string            `json:"version"`
	MemoryLimitMB int               `json:"memory_limit_mb"`
	FPM           FPM               `json:"fpm"`
	INI           map[string]string `json:"ini"`
	OPcache       OPcache           `json:"opcache"`
}

// DefaultSettings is what a site gets before anyone tunes it.
func DefaultSettings() Settings {
	return Settings{
		Version:       DefaultVersion,
		MemoryLimitMB: DefaultMemoryLimitMB,
		FPM:           DefaultFPM(),
		INI:           map[string]string{},
		OPcache:       DefaultOPcache(),
	}
}

// Validate normalizes and checks the whole envelope, filling zero values with
// defaults so a caller can send only what it wants to change.
func (s *Settings) Validate() error {
	if s.Version == "" {
		s.Version = DefaultVersion
	}
	if !IsSupported(s.Version) {
		return errx.Validation("unsupported_php_version", "That PHP version is not available.",
			errx.Field{Field: "version", Code: "unsupported", Message: "unsupported version"})
	}
	if s.MemoryLimitMB == 0 {
		s.MemoryLimitMB = DefaultMemoryLimitMB
	}
	if s.MemoryLimitMB < MinMemoryLimitMB || s.MemoryLimitMB > MaxMemoryLimitMB {
		return errx.Validation("bad_memory_limit",
			"memory_limit_mb must be between "+strconv.Itoa(MinMemoryLimitMB)+" and "+strconv.Itoa(MaxMemoryLimitMB)+".",
			errx.Field{Field: "memory_limit_mb", Code: "out_of_range", Message: "out of range"})
	}
	if err := validateFPM(&s.FPM); err != nil {
		return err
	}
	if err := validateOPcache(&s.OPcache); err != nil {
		return err
	}
	if s.INI == nil {
		s.INI = map[string]string{}
	}
	return validateINI(s.INI)
}

// iniPair is one rendered directive. The template takes a sorted slice rather
// than the map so the pool file is byte-identical for identical settings: Go
// randomizes map iteration, and a file that reshuffles itself on every apply
// makes a diff of "what actually changed" impossible to read.
type iniPair struct{ Key, Value string }

func sortedINI(m map[string]string) []iniPair {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]iniPair, 0, len(keys))
	for _, k := range keys {
		out = append(out, iniPair{Key: k, Value: m[k]})
	}
	return out
}

// jitDirective maps a mode onto opcache.jit's CRTO digits, or "" for off.
//
// PHP spells this as four digits and the names are not interchangeable with it:
// "tracing" is 1254 and "function" is 1205. Passing the word through would be
// silently accepted by older PHP and mean nothing.
func jitDirective(mode string) string {
	switch mode {
	case JITTracing:
		return "tracing"
	case JITFunction:
		return "function"
	default:
		return ""
	}
}
