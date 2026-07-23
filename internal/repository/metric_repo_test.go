package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/monitor"
	"github.com/thisisnkp/heropanel/internal/repository"
)

func TestMetricStoreRollupAndPrune(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewMetricStore(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	// Three raw points inside the 10:00–11:00 hour: CPU 10/20/30 → avg 20.
	for i, cpu := range []float64{10, 20, 30} {
		if err := store.InsertNodeRaw(ctx, base.Add(time.Duration(i*15)*time.Minute), monitor.HistPoint{
			CPUPercent: cpu, MemUsedKB: int64(1000 + i*100), MemTotalKB: 4000, Load1: 1,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	raw, err := store.RangeNode(ctx, "raw", base.Add(-time.Hour))
	if err != nil || len(raw) != 3 {
		t.Fatalf("range raw = %d rows, err=%v; want 3", len(raw), err)
	}
	if raw[0].CPUPercent != 10 || raw[2].CPUPercent != 30 {
		t.Errorf("raw not ordered oldest-first: %+v", raw)
	}

	// Roll the hour up: one hour row at 10:00 with the averages.
	from, to := base, base.Add(time.Hour)
	if err := store.RollupNodeHour(ctx, from, to); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	hours, _ := store.RangeNode(ctx, "hour", base.Add(-time.Hour))
	if len(hours) != 1 {
		t.Fatalf("got %d hour rows, want 1", len(hours))
	}
	if hours[0].CPUPercent != 20 || hours[0].MemUsedKB != 1100 {
		t.Errorf("hour average wrong: %+v (want cpu 20, mem 1100)", hours[0])
	}

	// Idempotent: a second rollup replaces rather than duplicates.
	if err := store.RollupNodeHour(ctx, from, to); err != nil {
		t.Fatalf("rollup #2: %v", err)
	}
	if hours2, _ := store.RangeNode(ctx, "hour", base.Add(-time.Hour)); len(hours2) != 1 {
		t.Fatalf("rollup was not idempotent: %d hour rows", len(hours2))
	}

	// Pruning raw older than 11:00 removes all three (they are at 10:00–10:30).
	if err := store.PruneNode(ctx, "raw", to); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if left, _ := store.RangeNode(ctx, "raw", base.Add(-time.Hour)); len(left) != 0 {
		t.Errorf("prune left %d raw rows, want 0", len(left))
	}
	// The hourly rollup survives — that is the point of keeping it.
	if hours3, _ := store.RangeNode(ctx, "hour", base.Add(-time.Hour)); len(hours3) != 1 {
		t.Errorf("prune removed the hourly rollup")
	}
}

// An empty hour must not insert a NULL-filled row.
func TestMetricStoreRollupEmptyHourIsNoop(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewMetricStore(db)
	ctx := context.Background()
	from := time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC)
	if err := store.RollupNodeHour(ctx, from, from.Add(time.Hour)); err != nil {
		t.Fatalf("rollup of empty hour: %v", err)
	}
	if hours, _ := store.RangeNode(ctx, "hour", from.Add(-time.Hour)); len(hours) != 0 {
		t.Errorf("empty hour produced %d rows, want 0", len(hours))
	}
}
