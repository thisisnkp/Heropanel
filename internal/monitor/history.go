package monitor

import (
	"context"
	"log/slog"
	"time"
)

// Metric history and rollups.
//
// The live channels push the present; history answers "what happened over the
// last day / week". Two things make it cheap to keep forever without the table
// growing forever:
//
//  1. A **persister** samples the node once a minute and writes a 'raw' row —
//     always, regardless of whether anyone is watching, because history has to be
//     continuous or a chart lies by omission. (This is the one part of the module
//     that is NOT subscription-gated, and deliberately so; a once-a-minute /proc
//     read is nothing.)
//  2. An **hourly rollup** averages the last hour of raw into a single 'hour' row
//     and prunes: raw is kept ~48h for recent detail, hourly ~30d for the long
//     tail. A week-long chart then reads ~168 hour rows, not ~10 000 raw ones.

const (
	// persistInterval is how often a raw history point is written.
	persistInterval = time.Minute
	// rollupInterval is how often raw is folded into hourly and old rows pruned.
	rollupInterval = time.Hour
	// rawRetention / hourRetention bound each granularity.
	rawRetention  = 48 * time.Hour
	hourRetention = 30 * 24 * time.Hour
)

// Granularity levels stored in node_metrics.granularity.
const (
	granRaw  = "raw"
	granHour = "hour"
)

// HistPoint is one stored node sample (or an hourly average of many).
type HistPoint struct {
	TS          string  `json:"ts"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsedKB   int64   `json:"mem_used_kb"`
	MemTotalKB  int64   `json:"mem_total_kb"`
	SwapUsedKB  int64   `json:"swap_used_kb"`
	Load1       float64 `json:"load1"`
	RootDiskPct float64 `json:"root_disk_pct"`
}

// MetricRepo persists and reads node history. Implemented by internal/repository.
type MetricRepo interface {
	// InsertNodeRaw appends a raw sample.
	InsertNodeRaw(ctx context.Context, ts time.Time, p HistPoint) error
	// RangeNode returns points of a granularity at or after since, oldest first.
	RangeNode(ctx context.Context, granularity string, since time.Time) ([]HistPoint, error)
	// RollupNodeHour averages raw rows in [from, to) into a single hour row at
	// from, replacing any existing hour row at that timestamp (idempotent).
	RollupNodeHour(ctx context.Context, from, to time.Time) error
	// PruneNode deletes rows of a granularity older than before.
	PruneNode(ctx context.Context, granularity string, before time.Time) error
}

// WithHistory enables persistence and historical reads. Without it the module is
// live-only (no /monitor/history, no persister).
func (s *Service) WithHistory(repo MetricRepo) *Service {
	s.history = repo
	return s
}

// HistoryEnabled reports whether historical reads are available.
func (s *Service) HistoryEnabled() bool { return s != nil && s.history != nil }

// nodeToPoint projects a live sample onto the stored point (dropping the fields
// history does not keep — per-core detail, individual disks beyond root).
func nodeToPoint(n NodeSample) HistPoint {
	var rootPct float64
	for _, d := range n.Disks {
		if d.Path == "/" {
			rootPct = d.UsedPercent
			break
		}
	}
	return HistPoint{
		CPUPercent: n.CPUPercent, MemUsedKB: n.MemUsedKB, MemTotalKB: n.MemTotalKB,
		SwapUsedKB: n.SwapUsedKB, Load1: n.Load1, RootDiskPct: rootPct,
	}
}

// RunPersister writes a raw node point every interval until ctx is cancelled. It
// samples against the persister's own CPU baseline (an interval average),
// independent of the live sampler, and evaluates alert rules against each sample.
// interval <= 0 uses the default (a minute); the e2e shortens it to prove firing
// without waiting a real minute.
func (s *Service) RunPersister(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if s.history == nil {
		return
	}
	if interval <= 0 {
		interval = persistInterval
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
		case now := <-t.C:
			sample := s.sampleNode(&s.persistPrevCPU, &s.persistHasPrev)
			if err := s.history.InsertNodeRaw(ctx, now.UTC(), nodeToPoint(sample)); err != nil {
				log.Debug("monitor: persist sample failed", "err", err)
			}
			// Alert rules are checked against the sample just written, so there is
			// no second sampler and no extra CPU baseline.
			if s.evaluator != nil {
				s.evaluator.Evaluate(ctx, sample, now.UTC())
			}
		}
	}
}

// RunRollup folds raw into hourly and prunes on each rollupInterval. It rolls up
// the hour that has just fully elapsed, so a partial current hour is never
// averaged prematurely.
func (s *Service) RunRollup(ctx context.Context, log *slog.Logger) {
	if s.history == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	t := time.NewTicker(rollupInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.rollupOnce(ctx, time.Now().UTC(), log)
		}
	}
}

// rollupOnce averages the previous whole hour and prunes both granularities.
func (s *Service) rollupOnce(ctx context.Context, now time.Time, log *slog.Logger) {
	hourEnd := now.Truncate(time.Hour)
	hourStart := hourEnd.Add(-time.Hour)
	if err := s.history.RollupNodeHour(ctx, hourStart, hourEnd); err != nil {
		log.Debug("monitor: hourly rollup failed", "err", err)
	}
	if err := s.history.PruneNode(ctx, granRaw, now.Add(-rawRetention)); err != nil {
		log.Debug("monitor: raw prune failed", "err", err)
	}
	if err := s.history.PruneNode(ctx, granHour, now.Add(-hourRetention)); err != nil {
		log.Debug("monitor: hour prune failed", "err", err)
	}
}

// History returns node history over the last dur, at the coarsest granularity
// that still covers it: raw within the raw-retention window, else hourly.
func (s *Service) History(ctx context.Context, dur time.Duration) ([]HistPoint, error) {
	if s.history == nil {
		return []HistPoint{}, nil
	}
	gran := granRaw
	if dur > rawRetention {
		gran = granHour
	}
	return s.history.RangeNode(ctx, gran, time.Now().UTC().Add(-dur))
}
