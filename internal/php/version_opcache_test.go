package php

import (
	"strings"
	"testing"
)

func TestVersionOPcacheValidateFillsDefaults(t *testing.T) {
	o := VersionOPcache{Version: "8.3"}
	if err := o.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	def := DefaultVersionOPcache("8.3")
	if o.MemoryConsumptionMB != def.MemoryConsumptionMB || o.MaxAcceleratedFiles != def.MaxAcceleratedFiles {
		t.Errorf("zero values were not filled with defaults: %+v", o)
	}
}

func TestVersionOPcacheRejectsOutOfRange(t *testing.T) {
	cases := []VersionOPcache{
		{Version: "8.3", MemoryConsumptionMB: 4097},
		{Version: "8.3", InternedStringsBufferMB: 999},
		{Version: "8.3", MaxAcceleratedFiles: 10},
		{Version: "8.3", RevalidateFreqSec: 99999},
		{Version: "8.3", JITBufferSizeMB: 9999},
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("case %d accepted an out-of-range value: %+v", i, c)
		}
	}
}

func TestVersionOPcacheRejectsUnknownVersion(t *testing.T) {
	o := VersionOPcache{Version: "5.6"}
	if err := o.Validate(); err == nil {
		t.Fatal("accepted an unsupported version")
	}
}

func TestVersionOPcacheRenderParseRoundTrip(t *testing.T) {
	o := VersionOPcache{
		Version: "8.3", MemoryConsumptionMB: 256, InternedStringsBufferMB: 16,
		MaxAcceleratedFiles: 20000, ValidateTimestamps: false, RevalidateFreqSec: 0, JITBufferSizeMB: 64,
	}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	ini, err := renderVersionOPcache(o)
	if err != nil {
		t.Fatal(err)
	}
	// The rendered ini must carry the SYSTEM directives with a jit_buffer_size in
	// PHP's "64M" form.
	for _, want := range []string{"opcache.memory_consumption=256", "opcache.jit_buffer_size=64M", "opcache.validate_timestamps=0"} {
		if !strings.Contains(ini, want) {
			t.Errorf("rendered ini missing %q:\n%s", want, ini)
		}
	}
	got := parseVersionOPcache("8.3", ini)
	if got.MemoryConsumptionMB != 256 || got.InternedStringsBufferMB != 16 || got.MaxAcceleratedFiles != 20000 ||
		got.ValidateTimestamps != false || got.JITBufferSizeMB != 64 {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestParseVersionOPcacheEmptyIsDefaults(t *testing.T) {
	got := parseVersionOPcache("8.3", "")
	def := DefaultVersionOPcache("8.3")
	if got != def {
		t.Errorf("empty ini should yield defaults, got %+v", got)
	}
}
