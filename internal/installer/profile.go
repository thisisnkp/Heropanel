// Package installer implements HeroPanel's system detection, compatibility
// checks, and install planning (docs/07). The pure functions (parsers, compat,
// plan) are unit-tested; Detect() reads the live system on Linux.
package installer

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Profile is a snapshot of the host relevant to installation.
type Profile struct {
	Arch           string `json:"arch"` // amd64 | arm64 | 386
	OS             string `json:"os"`   // linux
	DistroID       string `json:"distro_id"`
	DistroVersion  string `json:"distro_version"`
	DistroName     string `json:"distro_name"`
	PkgManager     string `json:"pkg_manager"` // apt | dnf | ""
	RAMBytes       uint64 `json:"ram_bytes"`
	CPUCores       int    `json:"cpu_cores"`
	Virtualization string `json:"virtualization"`
	HasSystemd     bool   `json:"has_systemd"`
}

// Detect gathers the host profile from the live system.
func Detect() Profile {
	p := Profile{
		Arch:           resolveArch(runtime.GOARCH),
		OS:             runtime.GOOS,
		CPUCores:       runtime.NumCPU(),
		Virtualization: "unknown",
	}
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		p.DistroID, p.DistroVersion, p.DistroName = parseOSRelease(string(b))
	}
	p.PkgManager = pkgManagerFor(p.DistroID)
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		p.RAMBytes = parseMemInfo(string(b))
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		p.HasSystemd = true
	}
	if out, err := exec.Command("systemd-detect-virt").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			p.Virtualization = v
		}
	}
	return p
}

// resolveArch normalizes Go's arch names to HeroPanel's supported set.
func resolveArch(goarch string) string {
	switch goarch {
	case "amd64", "arm64", "386":
		return goarch
	default:
		return goarch
	}
}

// parseOSRelease extracts ID, VERSION_ID and PRETTY_NAME from an os-release file.
func parseOSRelease(content string) (id, versionID, prettyName string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = strings.ToLower(v)
		case "VERSION_ID":
			versionID = v
		case "PRETTY_NAME":
			prettyName = v
		}
	}
	return id, versionID, prettyName
}

// parseMemInfo returns total RAM in bytes from /proc/meminfo content.
func parseMemInfo(content string) uint64 {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return kb * 1024
			}
		}
	}
	return 0
}

// pkgManagerFor maps a distro ID to its package manager.
func pkgManagerFor(distroID string) string {
	switch distroID {
	case "ubuntu", "debian", "raspbian", "linuxmint", "pop":
		return "apt"
	case "rocky", "almalinux", "rhel", "centos", "ol", "oracle", "fedora":
		return "dnf"
	default:
		return ""
	}
}
