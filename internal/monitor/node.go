// Package monitor is hpd's in-core metrics module: it samples the node (and, in
// later slices, sites, containers and services) and pushes the numbers to
// subscribed browsers over the realtime hub.
//
// The design principle is **subscription-gated sampling** (docs/20): the panel
// does no metric work when nobody is watching. A dashboard is the classic source
// of idle load — a page left open in a background tab polling every few seconds
// forever — so here the browser subscribes to a channel and the server samples
// only while at least one client is subscribed, and pushes rather than being
// polled. An unattended panel costs nothing.
//
// Node metrics come straight from /proc and statfs, which are world-readable, so
// hpd reads them itself without crossing the broker. Nothing here is privileged;
// the broker is only involved for things that genuinely need root (a site's
// cgroup accounting, a service's status), which are later slices.
package monitor

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// NodeSample is one snapshot of the host's health.
type NodeSample struct {
	// CPUPercent is busy time across all cores, 0–100. It is a rate, so it needs
	// two /proc/stat reads to compute; the first sample after a cold start reports
	// 0 until there is a previous read to diff against.
	CPUPercent float64 `json:"cpu_percent"`

	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`

	MemTotalKB     int64 `json:"mem_total_kb"`
	MemUsedKB      int64 `json:"mem_used_kb"`
	MemAvailableKB int64 `json:"mem_available_kb"`
	SwapTotalKB    int64 `json:"swap_total_kb"`
	SwapUsedKB     int64 `json:"swap_used_kb"`

	UptimeSec int64 `json:"uptime_sec"`

	Disks []DiskUsage `json:"disks"`
}

// DiskUsage is one mounted filesystem's utilisation.
type DiskUsage struct {
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// cpuTimes holds the jiffies from /proc/stat's aggregate "cpu" line. Idle folds
// in iowait, which is idle-for-CPU-purposes (the CPU had nothing to run while
// waiting on IO), matching what `top` and every other tool report.
type cpuTimes struct {
	total uint64
	idle  uint64
}

// parseCPUStat reads the aggregate "cpu" line from /proc/stat. A zero value with
// ok=false means the line was absent or malformed (a non-Linux host), which the
// caller treats as "no CPU reading" rather than a crash.
func parseCPUStat(data []byte) (cpuTimes, bool) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:] // drop the "cpu" label
		var t cpuTimes
		for i, f := range fields {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuTimes{}, false
			}
			t.total += v
			// Field 3 is idle, field 4 is iowait (0-indexed after the label).
			if i == 3 || i == 4 {
				t.idle += v
			}
		}
		return t, t.total > 0
	}
	return cpuTimes{}, false
}

// cpuPercent computes busy percentage between two /proc/stat reads. A zero or
// negative total delta (identical reads, or a counter reset) reports 0 rather
// than a divide-by-zero or a nonsense spike.
func cpuPercent(prev, cur cpuTimes) float64 {
	totalDelta := int64(cur.total) - int64(prev.total)
	idleDelta := int64(cur.idle) - int64(prev.idle)
	if totalDelta <= 0 {
		return 0
	}
	busy := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	if busy < 0 {
		return 0
	}
	if busy > 100 {
		return 100
	}
	return busy
}

// memInfo is the handful of /proc/meminfo fields a dashboard needs.
type memInfo struct {
	totalKB, availableKB, freeKB int64
	buffersKB, cachedKB          int64
	swapTotalKB, swapFreeKB      int64
	hasAvailable                 bool
}

// parseMeminfo reads /proc/meminfo. MemAvailable (kernel 3.14+) is the honest
// "how much can a new workload use" number; when it is absent the caller falls
// back to free+buffers+cached, which is the pre-MemAvailable approximation.
func parseMeminfo(data []byte) memInfo {
	var m memInfo
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			m.totalKB = kb
		case "MemAvailable":
			m.availableKB = kb
			m.hasAvailable = true
		case "MemFree":
			m.freeKB = kb
		case "Buffers":
			m.buffersKB = kb
		case "Cached":
			m.cachedKB = kb
		case "SwapTotal":
			m.swapTotalKB = kb
		case "SwapFree":
			m.swapFreeKB = kb
		}
	}
	return m
}

// available returns MemAvailable, or the free+buffers+cached fallback.
func (m memInfo) available() int64 {
	if m.hasAvailable {
		return m.availableKB
	}
	return m.freeKB + m.buffersKB + m.cachedKB
}

// parseLoadavg reads the three load figures from /proc/loadavg.
func parseLoadavg(data []byte) (l1, l5, l15 float64) {
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(fields[0], 64)
	l5, _ = strconv.ParseFloat(fields[1], 64)
	l15, _ = strconv.ParseFloat(fields[2], 64)
	return l1, l5, l15
}

// parseUptime reads whole seconds of uptime from /proc/uptime's first field.
func parseUptime(data []byte) int64 {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(fields[0], 64)
	return int64(f)
}
