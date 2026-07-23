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

// The realtime hub channels the sampler pushes on, each gated independently: a
// dashboard subscribed only to the node tiles never triggers a per-site sweep.
const (
	NodeChannel     = "monitor:node"
	SitesChannel    = "monitor:sites"
	ServicesChannel = "monitor:services"
)

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

// Service samples node, site and service health. It holds the previous CPU
// readings (node-wide and per-site) so each sample is a rate rather than a
// meaningless cumulative counter.
type Service struct {
	mu sync.Mutex
	// Two independent CPU baselines. The live sampler (3s) and the history
	// persister (60s) each diff against their own previous read; sharing one
	// baseline would mix a 3s window with a 60s window and corrupt both rates.
	prevCPU        cpuTimes
	hasPrev        bool
	persistPrevCPU cpuTimes
	persistHasPrev bool
	prevSite       map[string]siteCPUPrev // per-vhost CPU baseline

	diskPaths []string
	// readFile and sleep are indirections so a test can feed fixtures and skip the
	// warm-up delay without touching /proc or the clock.
	readFile func(string) ([]byte, error)
	sleep    func(time.Duration)

	// Optional providers, wired at construction. When nil, the corresponding
	// channel is simply not sampled — a host without sites, or an install that
	// cannot read service state, degrades to node metrics alone.
	siteLister    func() []SiteRef
	serviceReader func(ctx context.Context) []ServiceHealth
	history       MetricRepo
	evaluator     *Evaluator
	alertAdmin    AlertAdmin
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

// WithSites supplies the current site list so per-site metrics can be sampled.
// The lister is called on each sweep (subscription-gated), so it always reflects
// the sites that exist right now.
func (s *Service) WithSites(lister func() []SiteRef) *Service {
	s.siteLister = lister
	return s
}

// WithServices supplies the service-health reader (broker-backed). Without it the
// services channel is not sampled.
func (s *Service) WithServices(reader func(ctx context.Context) []ServiceHealth) *Service {
	s.serviceReader = reader
	return s
}

// Sample gathers one snapshot of node health for the LIVE view (its CPU rate is
// diffed against the live baseline). It never errors: a metric it cannot read (a
// file absent on a non-Linux dev host) is left at its zero value rather than
// failing the whole sample, because a partial dashboard beats none.
func (s *Service) Sample() NodeSample { return s.sampleNode(&s.prevCPU, &s.hasPrev) }

// sampleNode is the shared body; the caller picks which CPU baseline to diff
// against (live vs. history persister).
func (s *Service) sampleNode(cpuPrev *cpuTimes, cpuHas *bool) NodeSample {
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
	out.CPUPercent = s.cpuDelta(cpuPrev, cpuHas)

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

// cpuDelta returns busy percentage since the previous read stored at *prev,
// taking a second read on the cold path so the first sample is real. The pointer
// pair lets the live sampler and the history persister keep separate baselines.
func (s *Service) cpuDelta(prev *cpuTimes, has *bool) float64 {
	cur, ok := s.readCPU()
	if !ok {
		return 0
	}
	s.mu.Lock()
	p, h := *prev, *has
	*prev, *has = cur, true
	s.mu.Unlock()
	if h {
		return cpuPercent(p, cur)
	}
	// Cold start: nothing to diff against yet.
	s.sleep(cpuWarmDelay)
	cur2, ok := s.readCPU()
	if !ok {
		return 0
	}
	s.mu.Lock()
	*prev = cur2
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
			s.sweep(ctx, pub, log)
		}
	}
}

// sweep pushes each channel that has a subscriber. The gates are independent, so
// a dashboard watching only the node tiles never triggers a per-site cgroup sweep
// or a broker round-trip for service state.
func (s *Service) sweep(ctx context.Context, pub Publisher, log *slog.Logger) {
	if pub.HasSubscribers(NodeChannel) {
		if b, err := json.Marshal(s.Sample()); err == nil {
			pub.Publish(NodeChannel, b)
		} else {
			log.Debug("monitor: node sample marshal failed", "err", err)
		}
	}
	if s.siteLister != nil && pub.HasSubscribers(SitesChannel) {
		if b, err := json.Marshal(map[string]any{"sites": s.SiteSamples(s.siteLister())}); err == nil {
			pub.Publish(SitesChannel, b)
		}
	}
	if s.serviceReader != nil && pub.HasSubscribers(ServicesChannel) {
		if b, err := json.Marshal(map[string]any{"services": s.serviceReader(ctx)}); err == nil {
			pub.Publish(ServicesChannel, b)
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
