package broker

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeGateway is a controllable Gateway for the resilience tests.
type fakeGateway struct {
	mu       sync.Mutex
	err      error         // returned by Invoke/Health
	calls    int           // Invoke count
	block    chan struct{} // when non-nil, Invoke blocks on it (to fill the bulkhead)
	inflight chan struct{} // signalled when a blocked call is in flight
}

func (f *fakeGateway) Invoke(ctx context.Context, _ string, _ any) (map[string]any, error) {
	f.mu.Lock()
	f.calls++
	block, inflight, err := f.block, f.inflight, f.err
	f.mu.Unlock()
	if block != nil {
		if inflight != nil {
			inflight <- struct{}{}
		}
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (f *fakeGateway) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *fakeGateway) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *fakeGateway) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func unavailable() error {
	return errx.New(errx.KindUnavailable, "broker_unavailable", "down")
}

// code extracts an *Error's machine code, or "" if err is not an *Error.
func code(err error) string {
	if e, ok := errx.As(err); ok {
		return e.Code
	}
	return ""
}

// The breaker opens after maxFailures connectivity errors and then fails fast
// without touching the inner gateway.
func TestResilient_CircuitOpensAndShedsFast(t *testing.T) {
	fg := &fakeGateway{}
	now := time.Unix(0, 0)
	r := newResilient(fg, discardLog(), 8, 3, 10*time.Second, func() time.Time { return now })
	fg.setErr(unavailable())

	// Three connectivity failures reach the inner gateway and trip the breaker.
	for i := 0; i < 3; i++ {
		if _, err := r.Invoke(context.Background(), "cap", nil); err == nil {
			t.Fatalf("call %d: expected error", i)
		}
	}
	if got := fg.callCount(); got != 3 {
		t.Fatalf("inner calls = %d, want 3", got)
	}
	// Now the circuit is open: the next call is shed without reaching the inner.
	_, err := r.Invoke(context.Background(), "cap", nil)
	if err == nil || code(err) != "broker_circuit_open" {
		t.Fatalf("expected broker_circuit_open, got %v", err)
	}
	if got := fg.callCount(); got != 3 {
		t.Fatalf("inner was called while open: %d, want 3", got)
	}
}

// After the cooldown, one probe is allowed through and a success closes the
// circuit again (self-healing).
func TestResilient_HalfOpenProbeHeals(t *testing.T) {
	fg := &fakeGateway{}
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }
	r := newResilient(fg, discardLog(), 8, 2, 10*time.Second, clock)

	fg.setErr(unavailable())
	for i := 0; i < 2; i++ {
		_, _ = r.Invoke(context.Background(), "cap", nil)
	}
	// Open now; a call before cooldown is shed.
	if _, err := r.Invoke(context.Background(), "cap", nil); code(err) != "broker_circuit_open" {
		t.Fatalf("want shed before cooldown, got %v", err)
	}
	callsBefore := fg.callCount()

	// Advance past the cooldown and let the broker recover.
	now = now.Add(11 * time.Second)
	fg.setErr(nil)
	if _, err := r.Invoke(context.Background(), "cap", nil); err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
	if fg.callCount() != callsBefore+1 {
		t.Fatalf("probe did not reach inner gateway")
	}
	// Circuit closed: subsequent calls flow normally.
	if _, err := r.Invoke(context.Background(), "cap", nil); err != nil {
		t.Fatalf("after heal: %v", err)
	}
}

// A non-connectivity error (e.g. validation) is a healthy answer from the
// broker and must not trip the breaker no matter how often it happens.
func TestResilient_NonConnectivityErrorDoesNotTrip(t *testing.T) {
	fg := &fakeGateway{}
	r := newResilient(fg, discardLog(), 8, 2, 10*time.Second, time.Now)
	fg.setErr(errx.Validation("bad_input", "nope"))

	for i := 0; i < 5; i++ {
		if _, err := r.Invoke(context.Background(), "cap", nil); err == nil {
			t.Fatal("expected the validation error to propagate")
		}
	}
	if fg.callCount() != 5 {
		t.Fatalf("breaker shed a healthy-broker error: calls=%d, want 5", fg.callCount())
	}
}

// The bulkhead sheds a caller whose context ends while all slots are occupied,
// instead of blocking it forever.
func TestResilient_BulkheadShedsWhenFull(t *testing.T) {
	fg := &fakeGateway{block: make(chan struct{}), inflight: make(chan struct{}, 1)}
	r := newResilient(fg, discardLog(), 1, 5, 10*time.Second, time.Now)

	// Occupy the single slot with a call that blocks inside the inner gateway.
	go func() { _, _ = r.Invoke(context.Background(), "cap", nil) }()
	<-fg.inflight // the slot is now held

	// A second caller with a short deadline must be shed, not blocked.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Invoke(ctx, "cap", nil)
	if err == nil || code(err) != "broker_saturated" {
		t.Fatalf("expected broker_saturated, got %v", err)
	}
	close(fg.block) // release the first call
}
