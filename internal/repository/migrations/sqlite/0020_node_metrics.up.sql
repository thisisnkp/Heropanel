-- Node metrics history (SQLite).
--
-- One wide row per sample: the dashboard reads every series for a time range at
-- once, so a wide row is one scan, not a join. granularity is the downsample
-- level — the persister writes 'raw' each minute, an hourly rollup averages the
-- last hour into one 'hour' row and prunes (raw ~48h, hour ~30d), so the table
-- stays bounded while a week-long chart stays cheap.
CREATE TABLE node_metrics (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            TEXT NOT NULL,
    granularity   TEXT NOT NULL DEFAULT 'raw',
    cpu_percent   REAL NOT NULL DEFAULT 0,
    mem_used_kb   INTEGER NOT NULL DEFAULT 0,
    mem_total_kb  INTEGER NOT NULL DEFAULT 0,
    swap_used_kb  INTEGER NOT NULL DEFAULT 0,
    load1         REAL NOT NULL DEFAULT 0,
    root_disk_pct REAL NOT NULL DEFAULT 0
);
CREATE INDEX ix_node_metrics_gran_ts ON node_metrics (granularity, ts);
