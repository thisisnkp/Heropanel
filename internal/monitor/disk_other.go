//go:build !linux

package monitor

// diskUsage has no portable implementation off Linux (statfs is Linux-specific,
// and the real target is a Linux host). On a developer's machine it reports
// "no reading", so the dashboard shows node CPU/memory/load but an empty disk
// list rather than failing to build. The Linux e2e proves the real thing.
func diskUsage(string) (DiskUsage, bool) { return DiskUsage{}, false }
