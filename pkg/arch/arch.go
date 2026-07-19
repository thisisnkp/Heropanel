// Package arch is HeroPanel's single source of truth for CPU architecture and OS
// detection, and for turning them into the names used to fetch per-arch
// artifacts (the installer's own binary, satellite-module binaries, PHP builds).
//
// It exists because "which build do I download" is asked in several places — the
// bootstrap installer, the module installer, the updater — and each answering it
// slightly differently is how a node ends up trying to run an amd64 binary on
// arm64. There is one normalization here and everyone uses it.
//
// Nothing here shells out or reads the network. Detection is from the running
// binary's own GOOS/GOARCH, which is correct precisely because HeroPanel ships a
// native build per arch: the binary that asks "what am I" is already the right
// one to answer from.
package arch

import (
	"runtime"
	"sort"
)

// Arch is a supported CPU architecture, in HeroPanel's own naming (which happens
// to match Go's for the three it supports).
type Arch string

const (
	AMD64 Arch = "amd64"
	ARM64 Arch = "arm64"
	I386  Arch = "386"
)

// OS is a supported operating system family. HeroPanel targets Linux; the type
// exists so a manifest or an artifact path can name an OS without a bare string,
// and so a non-Linux host (a developer's machine) is a recognizable value rather
// than an assumption.
type OS string

const (
	Linux OS = "linux"
)

// supported is the canonical set, with the human label the UI shows.
var supported = map[Arch]string{
	AMD64: "x86-64 (amd64)",
	ARM64: "ARM64 (aarch64)",
	I386:  "x86 (32-bit)",
}

// Supported reports whether a is an architecture HeroPanel builds for.
func (a Arch) Supported() bool { _, ok := supported[a]; return ok }

// Label returns a human-readable name, or the raw value if unknown.
func (a Arch) Label() string {
	if l, ok := supported[a]; ok {
		return l
	}
	return string(a)
}

// String implements fmt.Stringer.
func (a Arch) String() string { return string(a) }

// Supported lists every architecture HeroPanel builds for, in a stable order so
// installer output and the release matrix agree.
func Supported() []Arch {
	out := make([]Arch, 0, len(supported))
	for a := range supported {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Normalize maps a Go GOARCH (or an equivalent alias) onto a HeroPanel Arch.
//
// The aliases are the ones that actually turn up: `uname -m` and package
// managers say "x86_64", "aarch64", "i686"; Go says "amd64", "arm64", "386". A
// value this does not recognize is returned unchanged, so an unsupported arch is
// a visible, reportable string rather than a silent fallback to amd64 — which
// would "work" right up until the binary faults.
func Normalize(s string) Arch {
	switch s {
	case "amd64", "x86_64", "x64":
		return AMD64
	case "arm64", "aarch64":
		return ARM64
	case "386", "i386", "i686", "x86":
		return I386
	default:
		return Arch(s)
	}
}

// Current is the architecture of the running binary. Because HeroPanel ships a
// native build per arch, this is authoritative: no `uname`, no guessing.
func Current() Arch { return Normalize(runtime.GOARCH) }

// CurrentOS is the OS of the running binary.
func CurrentOS() OS { return OS(runtime.GOOS) }

// ArtifactName builds the canonical download name for a component: e.g.
// ArtifactName("hp-installer", Linux, ARM64) -> "hp-installer-linux-arm64".
//
// This is the one place the naming scheme lives. The installer's bootstrap
// script, the module installer, and the release build all have to agree on it,
// and they agree by calling this rather than each formatting the string.
func ArtifactName(component string, os OS, a Arch) string {
	return component + "-" + string(os) + "-" + string(a)
}

// CurrentArtifactName is ArtifactName for the running host.
func CurrentArtifactName(component string) string {
	return ArtifactName(component, CurrentOS(), Current())
}
