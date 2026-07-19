package installer

import "github.com/thisisnkp/heropanel/pkg/arch"

// Verdict is the overall compatibility outcome.
type Verdict string

const (
	VerdictProceed Verdict = "proceed"
	VerdictWarn    Verdict = "warn"
	VerdictBlock   Verdict = "block"
)

// Report is the compatibility result: an overall verdict plus specific reasons.
type Report struct {
	Verdict  Verdict  `json:"verdict"`
	Blocks   []string `json:"blocks,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// minRAM is the floor below which we recommend the minimal/SQLite profile.
const minRAM = 1 << 30 // 1 GiB

// supportedDistro reports whether a distro is fully supported (vs. best-effort).
func supportedDistro(id string) (supported, bestEffort bool) {
	switch id {
	case "ubuntu", "debian", "rocky", "almalinux", "ol", "oracle":
		return true, false
	case "rhel", "centos", "fedora":
		return false, true
	default:
		return false, false
	}
}

// Compatibility evaluates whether the host can run HeroPanel.
func Compatibility(p Profile) Report {
	var r Report

	if p.OS != "linux" {
		r.Blocks = append(r.Blocks, "unsupported operating system: "+p.OS+" (Linux is required)")
	}
	if !arch.Arch(p.Arch).Supported() {
		r.Blocks = append(r.Blocks, "unsupported CPU architecture: "+p.Arch)
	}
	if p.OS == "linux" && !p.HasSystemd {
		r.Blocks = append(r.Blocks, "systemd is required but was not detected")
	}
	if p.OS == "linux" && p.PkgManager == "" {
		r.Blocks = append(r.Blocks, "no supported package manager (apt/dnf) found")
	}

	if p.OS == "linux" {
		supported, bestEffort := supportedDistro(p.DistroID)
		switch {
		case supported:
			// ok
		case bestEffort:
			r.Warnings = append(r.Warnings, "distribution "+p.DistroID+" is best-effort (not officially supported)")
		default:
			r.Blocks = append(r.Blocks, "unsupported distribution: "+display(p.DistroID))
		}
	}

	if p.RAMBytes > 0 && p.RAMBytes < minRAM {
		r.Warnings = append(r.Warnings, "less than 1 GiB RAM detected; the minimal (SQLite) profile is recommended")
	}

	switch {
	case len(r.Blocks) > 0:
		r.Verdict = VerdictBlock
	case len(r.Warnings) > 0:
		r.Verdict = VerdictWarn
	default:
		r.Verdict = VerdictProceed
	}
	return r
}

func display(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
