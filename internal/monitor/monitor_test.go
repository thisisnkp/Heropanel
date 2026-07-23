package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeFS feeds /proc fixtures so the parsers and sampler can be exercised on any
// OS, no real /proc needed.
type fakeFS map[string]string

func (f fakeFS) read(path string) ([]byte, error) {
	if v, ok := f[path]; ok {
		return []byte(v), nil
	}
	return nil, errors.New("no such file")
}

const stat1 = "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 50 0 50 400 0 0 0 0 0 0\n"

// One second later: 100 more busy jiffies, 100 more idle → 50% busy.
const stat2 = "cpu  150 0 150 900 0 0 0 0 0 0\ncpu0 75 0 75 450 0 0 0 0 0 0\n"

const meminfo = `MemTotal:       8000000 kB
MemFree:         500000 kB
MemAvailable:   6000000 kB
Buffers:         100000 kB
Cached:         1400000 kB
SwapTotal:      2000000 kB
SwapFree:       1500000 kB
`

func TestParseCPUStatAndPercent(t *testing.T) {
	p, ok := parseCPUStat([]byte(stat1))
	if !ok {
		t.Fatal("parseCPUStat failed on a valid line")
	}
	// total = 100+0+100+800 = 1000, idle = idle(800)+iowait(0) = 800.
	if p.total != 1000 || p.idle != 800 {
		t.Fatalf("cpu times = %+v, want total 1000 idle 800", p)
	}
	c, _ := parseCPUStat([]byte(stat2))
	if got := cpuPercent(p, c); got != 50 {
		t.Fatalf("cpu%% = %v, want 50", got)
	}
	// Identical reads must not divide by zero — they report 0, not NaN.
	if got := cpuPercent(c, c); got != 0 {
		t.Fatalf("cpu%% for identical reads = %v, want 0", got)
	}
	// A malformed line is "no reading", not a panic.
	if _, ok := parseCPUStat([]byte("garbage\n")); ok {
		t.Fatal("parseCPUStat accepted a garbage line")
	}
}

func TestParseMeminfoUsesMemAvailable(t *testing.T) {
	m := parseMeminfo([]byte(meminfo))
	if m.totalKB != 8000000 || !m.hasAvailable || m.available() != 6000000 {
		t.Fatalf("meminfo = %+v, want total 8000000 available 6000000", m)
	}
	// Without MemAvailable, fall back to free+buffers+cached.
	m2 := parseMeminfo([]byte("MemTotal: 8000000 kB\nMemFree: 500000 kB\nBuffers: 100000 kB\nCached: 1400000 kB\n"))
	if m2.hasAvailable {
		t.Fatal("hasAvailable should be false when MemAvailable is absent")
	}
	if got := m2.available(); got != 2000000 {
		t.Fatalf("fallback available = %d, want 2000000 (free+buffers+cached)", got)
	}
}

func TestParseLoadavgAndUptime(t *testing.T) {
	l1, l5, l15 := parseLoadavg([]byte("0.50 1.25 2.00 1/234 5678"))
	if l1 != 0.50 || l5 != 1.25 || l15 != 2.00 {
		t.Fatalf("loadavg = %v/%v/%v", l1, l5, l15)
	}
	if got := parseUptime([]byte("123456.78 654321.00")); got != 123456 {
		t.Fatalf("uptime = %d, want 123456", got)
	}
}

func TestSampleAssemblesTheSnapshot(t *testing.T) {
	fs := fakeFS{statPath: stat1, meminfoPath: meminfo, loadavgPath: "0.50 1.25 2.00 1/1 1", uptimePath: "100 0"}
	// Feed the second stat on the warm read so the cold path yields 50%.
	reads := 0
	svc := &Service{
		diskPaths: nil,
		sleep:     func(time.Duration) {},
		readFile: func(p string) ([]byte, error) {
			if p == statPath {
				reads++
				if reads >= 2 {
					return []byte(stat2), nil
				}
			}
			return fs.read(p)
		},
	}
	s := svc.Sample()
	if s.CPUPercent != 50 {
		t.Errorf("cpu = %v, want 50 (cold path takes a second read)", s.CPUPercent)
	}
	if s.MemTotalKB != 8000000 || s.MemUsedKB != 2000000 || s.MemAvailableKB != 6000000 {
		t.Errorf("mem = %+v", s)
	}
	if s.SwapUsedKB != 500000 {
		t.Errorf("swap used = %d, want 500000", s.SwapUsedKB)
	}
	if s.Load1 != 0.50 || s.UptimeSec != 100 {
		t.Errorf("load/uptime = %v/%d", s.Load1, s.UptimeSec)
	}
}

func TestParseCgroupFiles(t *testing.T) {
	usage, ok := parseCPUUsageUsec([]byte("usage_usec 1500000\nuser_usec 1000000\nsystem_usec 500000\n"))
	if !ok || usage != 1500000 {
		t.Fatalf("usage_usec = %d ok=%v, want 1500000", usage, ok)
	}
	if got := parseSingleInt([]byte("  4194304\n")); got != 4194304 {
		t.Fatalf("memory.current = %d, want 4194304", got)
	}
	// "max" (an unset limit) and garbage read as 0, not an error.
	if got := parseSingleInt([]byte("max\n")); got != 0 {
		t.Fatalf("max parsed as %d, want 0", got)
	}
}

func TestSiteSamplesReadCgroupAndDetectAbsence(t *testing.T) {
	// One site has a cgroup, one does not.
	present := siteCgroupDir("hps1")
	fs := fakeFS{
		present + "/memory.current": "8388608",
		present + "/pids.current":   "12",
		present + "/cpu.stat":       "usage_usec 2000000\n",
	}
	svc := &Service{readFile: fs.read, sleep: func(time.Duration) {}}
	samples := svc.SiteSamples([]SiteRef{{VhostName: "hps1", SiteUID: "S1"}, {VhostName: "hps2", SiteUID: "S2"}})
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2", len(samples))
	}
	if !samples[0].Present || samples[0].MemCurrentBytes != 8388608 || samples[0].Tasks != 12 {
		t.Errorf("present site sample wrong: %+v", samples[0])
	}
	if samples[0].CPUPercent != 0 {
		t.Errorf("first CPU read should be 0 (no baseline), got %v", samples[0].CPUPercent)
	}
	if samples[1].Present {
		t.Errorf("a site with no cgroup must report present=false: %+v", samples[1])
	}
}

// fakeBroker answers service.status with a fixed set, or an error.
type fakeBroker struct {
	statuses []map[string]any
	err      error
}

func (f fakeBroker) Invoke(context.Context, string, any) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	rows := make([]any, len(f.statuses))
	for i, s := range f.statuses {
		rows[i] = s
	}
	return map[string]any{"statuses": rows}, nil
}

func TestServiceReaderMapsBrokerStatuses(t *testing.T) {
	b := fakeBroker{statuses: []map[string]any{
		{"service": "mariadb", "state": "active"},
		{"service": "redis", "state": "failed"},
	}}
	reader := NewServiceReader(b, []string{"openlitespeed", "mariadb", "redis"})
	got := reader(context.Background())
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 (one per requested service)", len(got))
	}
	// openlitespeed was requested but not in the broker's answer → unknown, not dropped.
	if got[0].Service != "openlitespeed" || got[0].State != "unknown" {
		t.Errorf("missing service should be unknown, got %+v", got[0])
	}
	if got[1].State != "active" || got[2].State != "failed" {
		t.Errorf("states not mapped: %+v", got)
	}

	// A broker error yields all-unknown rather than dropping the tiles.
	failing := NewServiceReader(fakeBroker{err: errors.New("broker down")}, []string{"mariadb"})
	if u := failing(context.Background()); len(u) != 1 || u[0].State != "unknown" {
		t.Errorf("broker error should be all-unknown, got %+v", u)
	}
}

// fakePub records publishes and controls whether anyone is "subscribed".
type fakePub struct {
	mu         sync.Mutex
	subscribed bool
	published  int
}

func (p *fakePub) HasSubscribers(string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.subscribed
}
func (p *fakePub) Publish(string, []byte) {
	p.mu.Lock()
	p.published++
	p.mu.Unlock()
}
func (p *fakePub) count() int { p.mu.Lock(); defer p.mu.Unlock(); return p.published }

// The whole point of the design: with nobody subscribed, the sampler pushes
// nothing. Once a client subscribes, samples start flowing.
func TestSamplerIsSubscriptionGated(t *testing.T) {
	fs := fakeFS{statPath: stat1, meminfoPath: meminfo, loadavgPath: "0 0 0", uptimePath: "0"}
	svc := &Service{diskPaths: nil, sleep: func(time.Duration) {}, readFile: fs.read}
	pub := &fakePub{subscribed: false}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.RunSampler(ctx, pub, 10*time.Millisecond, nil)

	time.Sleep(60 * time.Millisecond)
	if n := pub.count(); n != 0 {
		t.Fatalf("sampler pushed %d times with no subscribers, want 0", n)
	}

	pub.mu.Lock()
	pub.subscribed = true
	pub.mu.Unlock()

	// Poll for the first push rather than assuming a fixed number of ticks.
	deadline := time.Now().Add(500 * time.Millisecond)
	for pub.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pub.count() == 0 {
		t.Fatal("sampler pushed nothing after a client subscribed")
	}
}
