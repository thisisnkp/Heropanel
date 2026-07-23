package monitor

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// The files node metrics come from. All world-readable, so hpd reads them
// directly rather than crossing the broker for a number that is not privileged.
const (
	statPath    = "/proc/stat"
	meminfoPath = "/proc/meminfo"
	loadavgPath = "/proc/loadavg"
	uptimePath  = "/proc/uptime"
)

// NodeChannel is the realtime hub channel node samples are pushed on.
const NodeChannel = "monitor:node"

// defaultDiskPaths are the filesystems a dashboard cares about: the root, and
// the tree every site lives under. Duplicates and unmounted paths are dropped
// when sampled.
var defaultDiskPaths = []string{"/", "/srv/heropanel"}

// cpuWarmDelay is how long the first (cold) CPU sample waits between its two
// /proc/stat reads. CPU is a rate and needs two points; without a previous read
// the sampler takes a second one so a one-shot request still returns a real
// figure instead of a 0. The ticker path never pays this after its first tick.
const cpuWarmDelay = 150 * time.Millisecond

// DefaultInterval is how often the sampler pushes while someone is watching.
const DefaultInterval = 3 * time.Second

// Publisher is the slice of the realtime hub the sampler needs: ask whether
// anyone is watching, and fan a payload out to them. Kept as an interface so the
// monitor does not depend on the whole ws package and tests can drive it.
type Publisher interface {
	HasSubscribers(channel string) bool
	Publish(channel string, payload []byte)
}

// Service samples the node's health. It holds the previous CPU reading so each
// sample is a rate rather than a meaningless cumulative counter.
type Service struct {
	mu      sync.Mutex
	prevCPU cpuTimes
	hasPrev bool

	diskPaths []string
	// readFile and sleep are indirections so a test can feed fixtures and skip the
	// warm-up delay without touching /proc or the clock.
	readFile func(string) ([]byte, error)
	sleep    func(time.Duration)
}

// New constructs the Service. Extra disk paths may be supplied; the defaults
// (root and the sites tree) are used when none are.
func New(diskPaths ...string) *Service {
	paths := diskPaths
	if len(paths) == 0 {
		paths = defaultDiskPaths
	}
	return &Service{diskPaths: dedup(paths), readFile: os.ReadFile, sleep: time.Sleep}
}

// Sample gathers one snapshot of node health. It never errors: a metric it
// cannot read (a file absent on a non-Linux dev host) is left at its zero value
// rather than failing the whole sample, because a partial dashboard beats none.
func (s *Service) Sample() NodeSample {
	var out NodeSample

	if data, err := s.readFile(loadavgPath); err == nil {
		out.Load1, out.Load5, out.Load15 = parseLoadavg(data)
	}
	if data, err := s.readFile(uptimePath); err == nil {
		out.UptimeSec = parseUptime(data)
	}
	if data, err := s.readFile(meminfoPath); err == nil {
		m := parseMeminfo(data)
		avail := m.available()
		out.MemTotalKB = m.totalKB
		out.MemAvailableKB = avail
		out.MemUsedKB = m.totalKB - avail
		out.SwapTotalKB = m.swapTotalKB
		out.SwapUsedKB = m.swapTotalKB - m.swapFreeKB
	}
	out.CPUPercent = s.sampleCPU()

	for _, p := range s.diskPaths {
		if d, ok := diskUsage(p); ok {
			out.Disks = append(out.Disks, d)
		}
	}
	return out
}

// readCPU reads and parses the aggregate CPU line.
func (s *Service) readCPU() (cpuTimes, bool) {
	data, err := s.readFile(statPath)
	if err != nil {
		return cpuTimes{}, false
	}
	return parseCPUStat(data)
}

// sampleCPU returns busy percentage since the previous read, taking a second
// read on the cold path so the first sample is real.
func (s *Service) sampleCPU() float64 {
	cur, ok := s.readCPU()
	if !ok {
		return 0
	}
	s.mu.Lock()
	prev, hasPrev := s.prevCPU, s.hasPrev
	s.prevCPU, s.hasPrev = cur, true
	s.mu.Unlock()
	if hasPrev {
		return cpuPercent(prev, cur)
	}
	// Cold start: nothing to diff against yet.
	s.sleep(cpuWarmDelay)
	cur2, ok := s.readCPU()
	if !ok {
		return 0
	}
	s.mu.Lock()
	s.prevCPU = cur2
	s.mu.Unlock()
	return cpuPercent(cur, cur2)
}

// RunSampler pushes a node sample to the hub on each tick — but only while at
// least one client is subscribed. This is the subscription gate: an unwatched
// panel does no metric work at all, which is the whole reason the dashboard
// pushes instead of being polled. Returns when ctx is cancelled.
func (s *Service) RunSampler(ctx context.Context, pub Publisher, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if log == nil {
		log = slog.Default()
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !pub.HasSubscribers(NodeChannel) {
				continue // nobody watching — sample nothing
			}
			b, err := json.Marshal(s.Sample())
			if err != nil {
				log.Debug("monitor: sample marshal failed", "err", err)
				continue
			}
			pub.Publish(NodeChannel, b)
		}
	}
}

// dedup removes duplicate disk paths while preserving order.
func dedup(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
