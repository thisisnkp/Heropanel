package broker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Resilient wraps a Gateway with two failsafes so a sick broker cannot take the
// whole control plane down with it:
//
//   - a bulkhead — a bound on how many broker calls may be in flight at once. A
//     hung broker would otherwise let request goroutines pile up until npd runs
//     out of them and stops serving even the pages that need no broker at all.
//     Beyond the bound, callers are shed fast with an "unavailable" error instead
//     of blocking, and a caller whose own context expires while queued is
//     released immediately.
//
//   - a circuit breaker — after a run of connectivity failures it stops dialing
//     for a cooldown and fails fast, so a down broker does not cost every request
//     a full dial+timeout. It self-heals: after the cooldown one probe is allowed
//     through, and a success closes the circuit again.
//
// Only connectivity failures (KindUnavailable) count against the breaker. A
// capability that returns a validation or conflict error means the broker is
// perfectly healthy and answered — that must never trip the circuit.
type Resilient struct {
	inner Gateway
	sem   chan struct{}
	log   *slog.Logger
	clock func() time.Time

	maxFailures int
	cooldown    time.Duration

	mu       sync.Mutex
	state    cbState
	failures int
	openedAt time.Time
}

type cbState int

const (
	cbClosed   cbState = iota // normal — calls flow
	cbOpen                    // shedding — broker is down, fail fast
	cbHalfOpen                // one probe in flight to test recovery
)

// Defaults tuned for npd's broker: a handful of concurrent privileged ops is
// plenty, five consecutive connect failures is a clear "it's down", and fifteen
// seconds is long enough to stop hammering yet short enough to recover quickly.
const (
	defaultBulkhead    = 32
	defaultMaxFailures = 5
	defaultCooldown    = 15 * time.Second
)

// NewResilient wraps inner with the default bulkhead and circuit breaker. A nil
// inner returns nil (so it composes with the "no broker configured" path).
func NewResilient(inner Gateway, log *slog.Logger) *Resilient {
	if inner == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return newResilient(inner, log, defaultBulkhead, defaultMaxFailures, defaultCooldown, time.Now)
}

// newResilient is the injectable constructor (tests set small limits and a fake
// clock).
func newResilient(inner Gateway, log *slog.Logger, bulkhead, maxFailures int, cooldown time.Duration, clock func() time.Time) *Resilient {
	if bulkhead < 1 {
		bulkhead = 1
	}
	return &Resilient{
		inner:       inner,
		sem:         make(chan struct{}, bulkhead),
		log:         log,
		clock:       clock,
		maxFailures: maxFailures,
		cooldown:    cooldown,
	}
}

// Invoke runs a capability through the breaker and bulkhead.
func (r *Resilient) Invoke(ctx context.Context, capability string, input any) (map[string]any, error) {
	// Circuit first: an open breaker fails without ever occupying a bulkhead slot
	// or paying for a dial.
	if err := r.enter(); err != nil {
		return nil, err
	}
	// Bulkhead: take a slot or shed. A caller that will time out anyway is
	// released the moment its own context ends rather than waiting on a slot.
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, errx.Wrap(ctx.Err(), errx.KindUnavailable, "broker_saturated",
			"The broker is busy; too many privileged operations are in flight. Please retry.")
	}
	res, err := r.inner.Invoke(ctx, capability, input)
	r.record(err)
	return res, err
}

// Health probes the broker and lets the result heal (or trip) the breaker, so a
// readiness check doubles as a recovery signal.
func (r *Resilient) Health(ctx context.Context) error {
	err := r.inner.Health(ctx)
	r.record(err)
	return err
}

// enter applies the circuit-breaker gate before a call. It returns an error to
// shed the call, or nil to let it through (transitioning to half-open when the
// cooldown has elapsed so exactly one probe gets through).
func (r *Resilient) enter() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.state {
	case cbClosed:
		return nil
	case cbOpen:
		if r.clock().Sub(r.openedAt) >= r.cooldown {
			r.state = cbHalfOpen // let this one call probe recovery
			return nil
		}
		return r.openErr()
	default: // cbHalfOpen — a probe is already out; everyone else waits
		return r.openErr()
	}
}

func (r *Resilient) openErr() error {
	return errx.New(errx.KindUnavailable, "broker_circuit_open",
		"The broker is unreachable; requests are being shed until it recovers.")
}

// record folds a call's outcome into the breaker. Only connectivity failures
// count against it; any answer from the broker (success or a typed capability
// error) proves it is healthy and closes the circuit.
func (r *Resilient) record(err error) {
	connFail := err != nil && errx.IsKind(err, errx.KindUnavailable)
	r.mu.Lock()
	defer r.mu.Unlock()
	if connFail {
		r.failures++
		if r.state == cbHalfOpen || r.failures >= r.maxFailures {
			if r.state != cbOpen {
				r.log.Warn("broker circuit opened", "failures", r.failures, "cooldown", r.cooldown)
			}
			r.state = cbOpen
			r.openedAt = r.clock()
		}
		return
	}
	if r.state != cbClosed {
		r.log.Info("broker circuit closed", "after", r.state)
	}
	r.failures = 0
	r.state = cbClosed
}

// ensure Resilient satisfies Gateway.
var _ Gateway = (*Resilient)(nil)
