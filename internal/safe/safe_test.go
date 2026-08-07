package safe_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/safe"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A task that panics is recovered (the process survives) and restarted; the
// second run proceeds normally.
func TestGoRecoversAndRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs int32
	done := make(chan struct{})
	safe.Go(ctx, quietLog(), "flaky", func(ctx context.Context) {
		if atomic.AddInt32(&runs, 1) == 1 {
			panic("boom") // first run panics; must be recovered, not fatal
		}
		close(done) // second (restarted) run reaches here
		<-ctx.Done()
	})

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("supervised goroutine did not restart after a panic")
	}
	if atomic.LoadInt32(&runs) < 2 {
		t.Fatalf("expected at least 2 runs (panic + restart), got %d", atomic.LoadInt32(&runs))
	}
}

// A task that returns cleanly is not restarted (no busy loop).
func TestGoStopsOnCleanReturn(t *testing.T) {
	var runs int32
	safe.Go(context.Background(), quietLog(), "once", func(context.Context) {
		atomic.AddInt32(&runs, 1)
	})
	time.Sleep(300 * time.Millisecond)
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("a clean return should run exactly once, got %d", got)
	}
}

// After ctx is cancelled the supervisor stops restarting even a task that always
// panics.
func TestGoStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs int32
	safe.Go(ctx, quietLog(), "always", func(context.Context) {
		atomic.AddInt32(&runs, 1)
		panic("always")
	})
	time.Sleep(150 * time.Millisecond) // let the first run(s) happen
	cancel()
	settled := atomic.LoadInt32(&runs)
	// Give the (1s) backoff time to elapse; a still-running supervisor would
	// restart and bump the count past `settled`.
	time.Sleep(1500 * time.Millisecond)
	if got := atomic.LoadInt32(&runs); got > settled+1 {
		t.Fatalf("supervisor kept restarting after cancel: settled=%d now=%d", settled, got)
	}
}
