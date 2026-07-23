package repository

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/thisisnkp/heropanel/internal/monitor"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// MetricStore implements monitor.MetricRepo over the datastore.
type MetricStore struct {
	db *DB
}

// NewMetricStore constructs a MetricStore.
func NewMetricStore(db *DB) *MetricStore { return &MetricStore{db: db} }

var _ monitor.MetricRepo = (*MetricStore)(nil)

// pointRow is the scan target; the columns match node_metrics.
type pointRow struct {
	TS          string  `db:"ts"`
	CPUPercent  float64 `db:"cpu_percent"`
	MemUsedKB   int64   `db:"mem_used_kb"`
	MemTotalKB  int64   `db:"mem_total_kb"`
	SwapUsedKB  int64   `db:"swap_used_kb"`
	Load1       float64 `db:"load1"`
	RootDiskPct float64 `db:"root_disk_pct"`
}

func (r pointRow) toPoint() monitor.HistPoint {
	return monitor.HistPoint{
		TS: r.TS, CPUPercent: r.CPUPercent, MemUsedKB: r.MemUsedKB, MemTotalKB: r.MemTotalKB,
		SwapUsedKB: r.SwapUsedKB, Load1: r.Load1, RootDiskPct: r.RootDiskPct,
	}
}

// InsertNodeRaw appends a raw sample.
func (s *MetricStore) InsertNodeRaw(ctx context.Context, ts time.Time, p monitor.HistPoint) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO node_metrics (ts, granularity, cpu_percent, mem_used_kb, mem_total_kb, swap_used_kb, load1, root_disk_pct)
		 VALUES (?, 'raw', ?, ?, ?, ?, ?, ?)`,
		fmtTS(ts), p.CPUPercent, p.MemUsedKB, p.MemTotalKB, p.SwapUsedKB, p.Load1, p.RootDiskPct)
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// RangeNode returns points of a granularity at or after since, oldest first.
func (s *MetricStore) RangeNode(ctx context.Context, granularity string, since time.Time) ([]monitor.HistPoint, error) {
	var rows []pointRow
	err := s.db.SelectContext(ctx, &rows,
		`SELECT ts, cpu_percent, mem_used_kb, mem_total_kb, swap_used_kb, load1, root_disk_pct
		   FROM node_metrics WHERE granularity = ? AND ts >= ? ORDER BY ts ASC`,
		granularity, fmtTS(since))
	if err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]monitor.HistPoint, len(rows))
	for i := range rows {
		out[i] = rows[i].toPoint()
	}
	return out, nil
}

// RollupNodeHour averages the raw rows in [from, to) into one hour row at from.
//
// The averaging is done in Go rather than SQL AVG so it is identical on both
// dialects (SQLite and MariaDB round and cast integers differently). It replaces
// any existing hour row at that timestamp, so a re-run is idempotent.
func (s *MetricStore) RollupNodeHour(ctx context.Context, from, to time.Time) error {
	var rows []pointRow
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT cpu_percent, mem_used_kb, mem_total_kb, swap_used_kb, load1, root_disk_pct
		   FROM node_metrics WHERE granularity = 'raw' AND ts >= ? AND ts < ?`,
		fmtTS(from), fmtTS(to)); err != nil {
		return errx.Internal(err)
	}
	if len(rows) == 0 {
		return nil // nothing to roll up for this hour
	}

	var avg monitor.HistPoint
	var sumMemUsed, sumMemTotal, sumSwap int64
	for _, r := range rows {
		avg.CPUPercent += r.CPUPercent
		avg.Load1 += r.Load1
		avg.RootDiskPct += r.RootDiskPct
		sumMemUsed += r.MemUsedKB
		sumMemTotal += r.MemTotalKB
		sumSwap += r.SwapUsedKB
	}
	n := int64(len(rows))
	nf := float64(len(rows))
	avg.CPUPercent /= nf
	avg.Load1 /= nf
	avg.RootDiskPct /= nf
	avg.MemUsedKB = sumMemUsed / n
	avg.MemTotalKB = sumMemTotal / n
	avg.SwapUsedKB = sumSwap / n

	ts := fmtTS(from)
	return s.db.WithTx(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM node_metrics WHERE granularity = 'hour' AND ts = ?`, ts); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO node_metrics (ts, granularity, cpu_percent, mem_used_kb, mem_total_kb, swap_used_kb, load1, root_disk_pct)
			 VALUES (?, 'hour', ?, ?, ?, ?, ?, ?)`,
			ts, avg.CPUPercent, avg.MemUsedKB, avg.MemTotalKB, avg.SwapUsedKB, avg.Load1, avg.RootDiskPct)
		return err
	})
}

// PruneNode deletes rows of a granularity older than before.
func (s *MetricStore) PruneNode(ctx context.Context, granularity string, before time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM node_metrics WHERE granularity = ? AND ts < ?`, granularity, fmtTS(before))
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}
