package security

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockGW records broker invocations and can fail a named capability.
type mockGW struct {
	mu    sync.Mutex
	calls []string
	fail  string
}

func (g *mockGW) Invoke(_ context.Context, cap string, _ any) (map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, cap)
	if cap == g.fail {
		return nil, errors.New(cap + " failed")
	}
	if cap == "firewall.status" {
		return map[string]any{"ruleset": "table inet nexpanel {}", "running": true}, nil
	}
	return map[string]any{}, nil
}
func (g *mockGW) Health(context.Context) error { return nil }
func (g *mockGW) called(cap string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if c == cap {
			n++
		}
	}
	return n
}

// memFW is an in-memory FirewallRepo.
type memFW struct {
	mu     sync.Mutex
	rules  []RuleRecord
	ipList []IPListEntry
	token  string
	dl     string
	seq    int64
}

func (m *memFW) ListRules(context.Context) ([]RuleRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]RuleRecord(nil), m.rules...), nil
}
func (m *memFW) InsertRule(_ context.Context, r *RuleRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	r.ID = m.seq
	m.rules = append(m.rules, *r)
	return nil
}
func (m *memFW) DeleteRule(_ context.Context, uid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rules {
		if m.rules[i].UID == uid {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memFW) NextPosition(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rules) + 1, nil
}
func (m *memFW) GetState(context.Context) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &State{PendingToken: m.token, Deadline: m.dl}, nil
}
func (m *memFW) SetState(_ context.Context, token, deadline string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token, m.dl = token, deadline
	return nil
}
func (m *memFW) ListIPEntries(context.Context) ([]IPListEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]IPListEntry(nil), m.ipList...), nil
}
func (m *memFW) InsertIPEntry(_ context.Context, e *IPListEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ipList = append(m.ipList, *e)
	return nil
}
func (m *memFW) DeleteIPEntry(_ context.Context, uid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.ipList {
		if m.ipList[i].UID == uid {
			m.ipList = append(m.ipList[:i], m.ipList[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memFW) InsertIPEntries(_ context.Context, entries []*IPListEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range entries {
		m.ipList = append(m.ipList, *e)
	}
	return nil
}
func (m *memFW) DeleteIPEntriesByCountry(_ context.Context, country string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.ipList[:0]
	for _, e := range m.ipList {
		if e.Country != country {
			kept = append(kept, e)
		}
	}
	m.ipList = append([]IPListEntry(nil), kept...)
	return nil
}

func newFW(t *testing.T) (*Firewall, *memFW, *mockGW) {
	t.Helper()
	repo := &memFW{}
	gw := &mockGW{}
	f := NewFirewall(repo, gw).WithWindow(MinWindow)
	return f, repo, gw
}

func TestAddRuleValidates(t *testing.T) {
	f, _, _ := newFW(t)
	// Valid rules, including an IPv6 source and a port range.
	for _, ok := range []RuleInput{
		{Action: "accept", Protocol: "tcp", Port: 22},
		{Action: "accept", Protocol: "tcp", Port: 443, Source: "2001:db8::/32"},
		{Action: "drop", Protocol: "udp", Port: 5000, PortEnd: 5100},
	} {
		if _, err := f.AddRule(t.Context(), ok); err != nil {
			t.Fatalf("valid rule %+v rejected: %v", ok, err)
		}
	}
	for _, bad := range []RuleInput{
		{Action: "reject", Protocol: "tcp", Port: 22},
		{Action: "accept", Protocol: "icmp", Port: 22},
		{Action: "accept", Protocol: "tcp", Port: 70000},
		{Action: "accept", Protocol: "any", Port: 22},                  // port needs explicit proto
		{Action: "accept", Protocol: "tcp", Source: "not-ip"},          // not an address
		{Action: "accept", Protocol: "tcp", Port: 9000, PortEnd: 8000}, // end <= start
		{Action: "accept", Protocol: "tcp", PortEnd: 9000},             // range with no start
		{Action: "accept", Protocol: "any", Port: 100, PortEnd: 200},   // range needs proto
	} {
		if _, err := f.AddRule(t.Context(), bad); err == nil {
			t.Errorf("bad rule accepted: %+v", bad)
		}
	}
}

// Apply invokes the broker and arms a pending change; Confirm with the right
// token makes it permanent and clears the state.
func TestApplyThenConfirm(t *testing.T) {
	f, repo, gw := newFW(t)
	_, _ = f.AddRule(t.Context(), RuleInput{Action: "accept", Protocol: "tcp", Port: 22})

	res, err := f.Apply(t.Context())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Token == "" || gw.called("firewall.apply") != 1 {
		t.Fatalf("apply did not reach the broker: %+v", gw.calls)
	}
	if repo.token != res.Token {
		t.Error("pending token not persisted")
	}

	// A stale token is refused.
	if err := f.Confirm(t.Context(), "wrong"); err == nil {
		t.Error("a stale confirm token was accepted")
	}
	if err := f.Confirm(t.Context(), res.Token); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if gw.called("firewall.confirm") != 1 || repo.token != "" {
		t.Errorf("confirm did not finalise: calls=%v token=%q", gw.calls, repo.token)
	}
}

// A second apply while one is pending is refused.
func TestApplyRefusesWhilePending(t *testing.T) {
	f, _, _ := newFW(t)
	if _, err := f.Apply(t.Context()); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if _, err := f.Apply(t.Context()); err == nil {
		t.Error("a second apply was accepted while one was pending")
	}
}

// The guard reverts an unconfirmed change once its deadline has passed —
// proven deterministically by setting a past deadline and sweeping.
func TestGuardRevertsPastDeadline(t *testing.T) {
	f, repo, gw := newFW(t)
	repo.token = "01ABC"
	repo.dl = time.Now().UTC().Add(-time.Second).Format(tsLayout)

	f.sweep(t.Context(), nil)
	if gw.called("firewall.rollback") != 1 {
		t.Errorf("the guard did not revert an expired change: %v", gw.calls)
	}
	if repo.token != "" {
		t.Error("the guard left the pending state set")
	}
}

// A change still within its window is NOT reverted by the guard.
func TestGuardKeepsWithinWindow(t *testing.T) {
	f, repo, gw := newFW(t)
	repo.token = "01ABC"
	repo.dl = time.Now().UTC().Add(time.Hour).Format(tsLayout)

	f.sweep(t.Context(), nil)
	if gw.called("firewall.rollback") != 0 {
		t.Error("the guard reverted a change that was still within its window")
	}
}

// The in-process timer fires a real auto-revert when nobody confirms.
func TestAutoRevertTimerFires(t *testing.T) {
	repo := &memFW{}
	gw := &mockGW{}
	// A sub-MinWindow window just for this timing test (bypass WithWindow's floor).
	f := NewFirewall(repo, gw)
	f.window = 40 * time.Millisecond

	if _, err := f.Apply(t.Context()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if gw.called("firewall.rollback") == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gw.called("firewall.rollback") != 1 {
		t.Errorf("the unconfirmed change was not auto-reverted: %v", gw.calls)
	}
	if repo.token != "" {
		t.Error("the auto-revert left the pending state set")
	}
}

// Rollback reverts immediately on request.
func TestManualRollback(t *testing.T) {
	f, repo, gw := newFW(t)
	_, _ = f.Apply(t.Context())
	if err := f.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if gw.called("firewall.rollback") != 1 || repo.token != "" {
		t.Errorf("manual rollback did not clear: calls=%v token=%q", gw.calls, repo.token)
	}
}
