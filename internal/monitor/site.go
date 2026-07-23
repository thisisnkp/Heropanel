package monitor

import (
	"strconv"
	"strings"
	"time"
)

// Per-site metrics, read from the site's cgroup v2 accounting.
//
// Every site's supervised work lives in a systemd slice with accounting turned
// on from the moment it is created (that is *why* Phase 1 turns it on — see
// broker siteslice.go). cgroup v2 exposes that accounting as plain files —
// `memory.current`, `cpu.stat`, `pids.current` — mode 0444, world-readable, so
// hpd reads them directly. No broker: there is no privilege in reading a number
// the kernel publishes to everyone.
//
// A site whose slice has no cgroup yet (nothing has run in it, or the host has no
// systemd) reports Present=false, and the dashboard shows a dash rather than a
// misleading zero.

// cgroupRoot is where cgroup v2 is mounted on a modern Linux host.
const cgroupRoot = "/sys/fs/cgroup"

// SiteRef identifies a site to sample: its vhost (which names the slice) and the
// uid the panel shows it under.
type SiteRef struct {
	VhostName string
	SiteUID   string
}

// SiteSample is one site's live resource usage.
type SiteSample struct {
	VhostName       string  `json:"vhost"`
	SiteUID         string  `json:"site_uid"`
	MemCurrentBytes int64   `json:"mem_current_bytes"`
	CPUPercent      float64 `json:"cpu_percent"`
	Tasks           int64   `json:"tasks"`
	// Present is false when the site has no cgroup accounting yet.
	Present bool `json:"present"`
}

// siteCPUPrev is the previous CPU reading for one slice, so usage can be turned
// into a rate.
type siteCPUPrev struct {
	usageUsec int64
	at        time.Time
}

// siteCgroupDir is the cgroup v2 path for a site's slice. The systemd hierarchy
// heropanel.slice → heropanel-site.slice → heropanel-site-<vhost>.slice mirrors
// straight onto the filesystem. Site vhosts are hps<id>, which need no slice-name
// escaping.
func siteCgroupDir(vhost string) string {
	slice := "heropanel-site-" + vhost + ".slice"
	return cgroupRoot + "/heropanel.slice/heropanel-site.slice/" + slice
}

// parseCPUUsageUsec pulls usage_usec from a cgroup v2 cpu.stat.
func parseCPUUsageUsec(data []byte) (int64, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, " "); ok && k == "usage_usec" {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// parseSingleInt reads a cgroup file holding just an integer (memory.current,
// pids.current). "max" (an unset limit) and blank read as 0.
func parseSingleInt(data []byte) int64 {
	s := strings.TrimSpace(string(data))
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// SiteSamples reads current usage for each site. Missing cgroup files (a site not
// yet run, or a non-systemd host) yield Present=false rather than an error.
func (s *Service) SiteSamples(refs []SiteRef) []SiteSample {
	now := time.Now()
	out := make([]SiteSample, 0, len(refs))
	for _, ref := range refs {
		dir := siteCgroupDir(ref.VhostName)
		sample := SiteSample{VhostName: ref.VhostName, SiteUID: ref.SiteUID}

		mem, memErr := s.readFile(dir + "/memory.current")
		if memErr == nil {
			sample.MemCurrentBytes = parseSingleInt(mem)
			sample.Present = true
		}
		if tasks, err := s.readFile(dir + "/pids.current"); err == nil {
			sample.Tasks = parseSingleInt(tasks)
			sample.Present = true
		}
		if cpu, err := s.readFile(dir + "/cpu.stat"); err == nil {
			if usage, ok := parseCPUUsageUsec(cpu); ok {
				sample.Present = true
				sample.CPUPercent = s.siteCPURate(ref.VhostName, usage, now)
			}
		}
		out = append(out, sample)
	}
	// Drop slices we sampled last time but no longer see, so the prev map does not
	// grow without bound as sites come and go.
	s.pruneSitePrev(refs)
	return out
}

// siteCPURate converts a cumulative usage_usec into a percentage of one wall-clock
// interval across all cores' worth of time. The first read for a slice has no
// baseline and reports 0.
func (s *Service) siteCPURate(vhost string, usageUsec int64, now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prevSite == nil {
		s.prevSite = map[string]siteCPUPrev{}
	}
	prev, ok := s.prevSite[vhost]
	s.prevSite[vhost] = siteCPUPrev{usageUsec: usageUsec, at: now}
	if !ok {
		return 0
	}
	wall := now.Sub(prev.at).Microseconds()
	if wall <= 0 {
		return 0
	}
	pct := float64(usageUsec-prev.usageUsec) / float64(wall) * 100
	if pct < 0 {
		return 0
	}
	return pct
}

// pruneSitePrev forgets CPU baselines for slices no longer in the sample set.
func (s *Service) pruneSitePrev(refs []SiteRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prevSite == nil {
		return
	}
	live := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		live[r.VhostName] = struct{}{}
	}
	for v := range s.prevSite {
		if _, ok := live[v]; !ok {
			delete(s.prevSite, v)
		}
	}
}
