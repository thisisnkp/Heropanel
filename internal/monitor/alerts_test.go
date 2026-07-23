package monitor

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeAlertRepo returns fixed rules and records the events fired.
type fakeAlertRepo struct {
	mu     sync.Mutex
	rules  []AlertRule
	events []AlertEvent
}

func (f *fakeAlertRepo) ActiveRules(context.Context) ([]AlertRule, error) { return f.rules, nil }
func (f *fakeAlertRepo) RecordEvent(_ context.Context, uid, state string, v float64, _ time.Time) error {
	f.mu.Lock()
	f.events = append(f.events, AlertEvent{RuleUID: uid, State: state, Value: v})
	f.mu.Unlock()
	return nil
}
func (f *fakeAlertRepo) states() []AlertEvent { f.mu.Lock(); defer f.mu.Unlock(); return f.events }

type fakeNotifier struct {
	mu    sync.Mutex
	calls []string
}

func (n *fakeNotifier) Notify(_ context.Context, r AlertRule, _ float64, state string) {
	n.mu.Lock()
	n.calls = append(n.calls, r.UID+":"+state)
	n.mu.Unlock()
}

func sampleCPU(pct float64) NodeSample { return NodeSample{CPUPercent: pct} }

// A rule fires once when breached, not again while still breaching, and resolves
// when it recovers.
func TestEvaluatorFiresOnceAndResolves(t *testing.T) {
	repo := &fakeAlertRepo{rules: []AlertRule{
		{UID: "r1", Name: "cpu high", Metric: MetricCPU, Op: "gt", Threshold: 50, ForSec: 0, Enabled: true},
	}}
	notif := &fakeNotifier{}
	ev := NewEvaluator(repo, notif, nil)
	ctx := context.Background()
	now := time.Now()

	ev.Evaluate(ctx, sampleCPU(60), now)          // breach → fire
	ev.Evaluate(ctx, sampleCPU(70), now.Add(1e9)) // still breaching → no new fire
	ev.Evaluate(ctx, sampleCPU(10), now.Add(2e9)) // recovered → resolve

	events := repo.states()
	if len(events) != 2 {
		t.Fatalf("recorded %d events, want 2 (fire + resolve): %+v", len(events), events)
	}
	if events[0].State != "firing" || events[1].State != "resolved" {
		t.Errorf("event states = %s,%s; want firing,resolved", events[0].State, events[1].State)
	}
	if len(notif.calls) != 2 || notif.calls[0] != "r1:firing" || notif.calls[1] != "r1:resolved" {
		t.Errorf("notifications = %v; want [r1:firing r1:resolved]", notif.calls)
	}
}

// for_sec means the breach must persist: a single spike does not fire.
func TestEvaluatorRespectsDuration(t *testing.T) {
	repo := &fakeAlertRepo{rules: []AlertRule{
		{UID: "r1", Metric: MetricCPU, Op: "gt", Threshold: 50, ForSec: 120, Enabled: true},
	}}
	ev := NewEvaluator(repo, nil, nil)
	ctx := context.Background()
	t0 := time.Now()

	ev.Evaluate(ctx, sampleCPU(90), t0)                     // breach begins — not yet 120s
	ev.Evaluate(ctx, sampleCPU(90), t0.Add(60*time.Second)) // 60s — still not enough
	if n := len(repo.states()); n != 0 {
		t.Fatalf("fired after %ds, want no fire before the duration", 60)
	}
	ev.Evaluate(ctx, sampleCPU(90), t0.Add(121*time.Second)) // past 120s → fire
	if n := len(repo.states()); n != 1 || repo.states()[0].State != "firing" {
		t.Fatalf("did not fire after the duration elapsed: %+v", repo.states())
	}

	// A dip below the threshold resets the clock: the timer starts over.
	ev.Evaluate(ctx, sampleCPU(10), t0.Add(122*time.Second)) // recover → resolve
	ev.Evaluate(ctx, sampleCPU(90), t0.Add(123*time.Second)) // breach again — clock restarts
	ev.Evaluate(ctx, sampleCPU(90), t0.Add(150*time.Second)) // only 27s in → no new fire
	firing := 0
	for _, e := range repo.states() {
		if e.State == "firing" {
			firing++
		}
	}
	if firing != 1 {
		t.Errorf("fired %d times; the duration clock did not restart after recovery", firing)
	}
}

func TestMetricValueSelection(t *testing.T) {
	n := NodeSample{
		CPUPercent: 42, Load1: 1.5, MemUsedKB: 500, MemTotalKB: 1000, SwapUsedKB: 100, SwapTotalKB: 400,
		Disks: []DiskUsage{{Path: "/", UsedPercent: 88}},
	}
	cases := map[string]float64{MetricCPU: 42, MetricLoad1: 1.5, MetricMem: 50, MetricSwap: 25, MetricDiskRoot: 88}
	for metric, want := range cases {
		got, ok := metricValue(n, metric)
		if !ok || got != want {
			t.Errorf("metricValue(%s) = %v ok=%v, want %v", metric, got, ok, want)
		}
	}
	// A metric with no data (no disk) is not readable → skipped, not zero.
	if _, ok := metricValue(NodeSample{}, MetricDiskRoot); ok {
		t.Error("disk_root should be unreadable with no disks")
	}
}
