// Package safe runs background work in supervised goroutines. A panic inside a
// background task (a scheduler, sweeper, sampler, dispatcher) is recovered and
// logged instead of crashing the whole hpd process, and a long-running loop is
// restarted with backoff while its context is alive. This is the backend half of
// fault isolation: one module's background goroutine must never take the panel
// down with it. HTTP handlers are already covered by the recoverer middleware;
// goroutines are not, and an unrecovered panic in any goroutine crashes the
// entire process.
package safe

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

// maxBackoff caps the restart delay so a persistently-panicking task retries at a
// steady, low rate rather than hammering.
const maxBackoff = 30 * time.Second

// Go runs fn in a supervised goroutine. If fn panics it is recovered, logged with
// a stack trace, and — while ctx is alive — restarted after a growing backoff. If
// fn returns normally the supervisor stops: a loop that exits on ctx cancellation
// is a clean shutdown, not something to restart.
func Go(ctx context.Context, log *slog.Logger, name string, fn func(context.Context)) {
	if log == nil {
		log = slog.Default()
	}
	go supervise(ctx, log, name, fn)
}

func supervise(ctx context.Context, log *slog.Logger, name string, fn func(context.Context)) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		panicked := runGuarded(ctx, log, name, fn)
		// A clean return (loop ended, usually on ctx cancel) means we are done.
		// Only a panic warrants a restart.
		if !panicked || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// runGuarded runs fn once, recovering a panic and reporting whether one occurred.
func runGuarded(ctx context.Context, log *slog.Logger, name string, fn func(context.Context)) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			log.Error("supervised goroutine panicked; recovering",
				"name", name, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	fn(ctx)
	return false
}
