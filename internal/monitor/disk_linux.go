//go:build linux

package monitor

import "syscall"

// diskUsage reads one filesystem's utilisation via statfs. The percentage uses
// the same arithmetic as `df`: capacity is used ÷ (used + available-to-a-normal-
// user), so reserved root blocks do not make a full disk read as 100 % for a
// tenant who cannot touch them. A path that cannot be stat'd (unmounted, gone)
// returns ok=false and is simply omitted rather than reported as zero.
func diskUsage(path string) (DiskUsage, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, false
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	if total == 0 {
		return DiskUsage{}, false
	}
	usedBlocks := st.Blocks - st.Bfree
	used := usedBlocks * bsize
	var pct float64
	if denom := usedBlocks + st.Bavail; denom > 0 {
		pct = float64(usedBlocks) / float64(denom) * 100
	}
	return DiskUsage{Path: path, TotalBytes: total, UsedBytes: used, UsedPercent: pct}, true
}
