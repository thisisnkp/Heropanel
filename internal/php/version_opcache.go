package php

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// VersionOPcache is the per-**version** OPcache tuning that a per-site pool
// cannot set: the PHP_INI_SYSTEM shared-memory directives the FPM master
// allocates once at startup. It applies to every site on the version, so it is
// server state gated by system.write, not a site setting (see settings.go, which
// documents why the pool only carries opcache.enable and opcache.jit).
type VersionOPcache struct {
	Version                 string `json:"version"`
	MemoryConsumptionMB     int    `json:"memory_consumption_mb"`
	InternedStringsBufferMB int    `json:"interned_strings_buffer_mb"`
	MaxAcceleratedFiles     int    `json:"max_accelerated_files"`
	ValidateTimestamps      bool   `json:"validate_timestamps"`
	RevalidateFreqSec       int    `json:"revalidate_freq_sec"`
	JITBufferSizeMB         int    `json:"jit_buffer_size_mb"`
}

// Bounds. Floors are where the value stops being useful; ceilings guard a single
// version from eating the node's memory (the segment is allocated once, but a
// 4 GB opcache on eight versions is still 32 GB reserved at boot).
const (
	minOpMemMB     = 8
	maxOpMemMB     = 4096
	minInternMB    = 1
	maxInternMB    = 256
	minAccelFiles  = 200
	maxAccelFiles  = 1000000
	maxRevalFreq   = 3600
	maxJITBufferMB = 512
)

// DefaultVersionOPcache is PHP's own posture: a modest segment, timestamps on,
// JIT buffer off (JIT itself is a per-site opt-in). A version never tuned
// behaves exactly as a stock install.
func DefaultVersionOPcache(version string) VersionOPcache {
	return VersionOPcache{
		Version:                 version,
		MemoryConsumptionMB:     128,
		InternedStringsBufferMB: 8,
		MaxAcceleratedFiles:     10000,
		ValidateTimestamps:      true,
		RevalidateFreqSec:       2,
		JITBufferSizeMB:         0,
	}
}

// Validate normalizes zero values to defaults and range-checks the rest.
func (o *VersionOPcache) Validate() error {
	if o.Version == "" {
		o.Version = DefaultVersion
	}
	if !IsSupported(o.Version) {
		return errx.Validation("unsupported_php_version", "That PHP version is not available.",
			errx.Field{Field: "version", Code: "unsupported", Message: "unsupported version"})
	}
	def := DefaultVersionOPcache(o.Version)
	if o.MemoryConsumptionMB == 0 {
		o.MemoryConsumptionMB = def.MemoryConsumptionMB
	}
	if o.InternedStringsBufferMB == 0 {
		o.InternedStringsBufferMB = def.InternedStringsBufferMB
	}
	if o.MaxAcceleratedFiles == 0 {
		o.MaxAcceleratedFiles = def.MaxAcceleratedFiles
	}
	if err := rangeCheck("memory_consumption_mb", o.MemoryConsumptionMB, minOpMemMB, maxOpMemMB); err != nil {
		return err
	}
	if err := rangeCheck("interned_strings_buffer_mb", o.InternedStringsBufferMB, minInternMB, maxInternMB); err != nil {
		return err
	}
	if err := rangeCheck("max_accelerated_files", o.MaxAcceleratedFiles, minAccelFiles, maxAccelFiles); err != nil {
		return err
	}
	if err := rangeCheck("revalidate_freq_sec", o.RevalidateFreqSec, 0, maxRevalFreq); err != nil {
		return err
	}
	if err := rangeCheck("jit_buffer_size_mb", o.JITBufferSizeMB, 0, maxJITBufferMB); err != nil {
		return err
	}
	return nil
}

func rangeCheck(field string, v, lo, hi int) error {
	if v < lo || v > hi {
		return errx.Validation("out_of_range",
			fmt.Sprintf("%s must be between %d and %d.", field, lo, hi),
			errx.Field{Field: field, Code: "out_of_range", Message: "out of range"})
	}
	return nil
}

var versionOpcacheTmpl = template.Must(template.New("opcache").Parse(
	`; Managed by NexPanel — per-version OPcache (PHP_INI_SYSTEM). Do not edit by hand.
opcache.memory_consumption={{.MemoryConsumptionMB}}
opcache.interned_strings_buffer={{.InternedStringsBufferMB}}
opcache.max_accelerated_files={{.MaxAcceleratedFiles}}
opcache.validate_timestamps={{if .ValidateTimestamps}}1{{else}}0{{end}}
opcache.revalidate_freq={{.RevalidateFreqSec}}
opcache.jit_buffer_size={{.JITBufferSizeMB}}M
`))

// renderVersionOPcache produces the ini text for the version-wide file.
func renderVersionOPcache(o VersionOPcache) (string, error) {
	var b bytes.Buffer
	if err := versionOpcacheTmpl.Execute(&b, o); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "opcache_render_failed", "Could not render the OPcache config.")
	}
	return b.String(), nil
}

// parseVersionOPcache reads the ini text back into the struct. Unknown or missing
// lines fall back to defaults, so a partially-written or absent file still yields
// a sensible view rather than an error.
func parseVersionOPcache(version, ini string) VersionOPcache {
	o := DefaultVersionOPcache(version)
	for _, line := range strings.Split(ini, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "opcache.memory_consumption":
			o.MemoryConsumptionMB = atoiOr(v, o.MemoryConsumptionMB)
		case "opcache.interned_strings_buffer":
			o.InternedStringsBufferMB = atoiOr(v, o.InternedStringsBufferMB)
		case "opcache.max_accelerated_files":
			o.MaxAcceleratedFiles = atoiOr(v, o.MaxAcceleratedFiles)
		case "opcache.validate_timestamps":
			o.ValidateTimestamps = v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
		case "opcache.revalidate_freq":
			o.RevalidateFreqSec = atoiOr(v, o.RevalidateFreqSec)
		case "opcache.jit_buffer_size":
			o.JITBufferSizeMB = atoiOr(strings.TrimSuffix(strings.TrimSuffix(v, "M"), "m"), o.JITBufferSizeMB)
		}
	}
	return o
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return fallback
}

// GetVersionOPcache reads the version-wide OPcache config live from the host
// (defaults when nothing has been written).
func (s *Service) GetVersionOPcache(ctx context.Context, version string) (*VersionOPcache, error) {
	if version == "" {
		version = DefaultVersion
	}
	if !IsSupported(version) {
		return nil, errx.Validation("unsupported_php_version", "That PHP version is not available.",
			errx.Field{Field: "version", Code: "unsupported", Message: "unsupported version"})
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; OPcache settings cannot be read.")
	}
	out, err := s.broker.Invoke(ctx, "php.read_opcache", map[string]any{"version": version})
	if err != nil {
		return nil, err
	}
	ini, _ := out["config"].(string)
	o := parseVersionOPcache(version, ini)
	return &o, nil
}

// SetVersionOPcache validates, renders, and applies the version-wide OPcache
// config through the broker (which config-tests and restarts FPM, rolling back
// on rejection).
func (s *Service) SetVersionOPcache(ctx context.Context, o VersionOPcache) (*VersionOPcache, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; OPcache settings cannot be changed.")
	}
	cfg, err := renderVersionOPcache(o)
	if err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, "php.write_opcache", map[string]any{
		"version": o.Version,
		"config":  cfg,
	}); err != nil {
		return nil, err
	}
	return &o, nil
}
