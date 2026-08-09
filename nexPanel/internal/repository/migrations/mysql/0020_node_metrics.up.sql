-- Node metrics history (MariaDB).
--
-- One wide row per sample rather than a tall metric/value table: the dashboard
-- reads all series for a time range at once, so a wide row is one scan instead of
-- a join, and the columns are a fixed, small set.
--
-- granularity is the downsample level. The persister writes 'raw' once a minute;
-- an hourly rollup job averages the last hour of raw into one 'hour' row and then
-- prunes — raw is kept ~48h (recent detail), hour ~30d (the long tail) — so the
-- table never grows without bound while a week-long chart stays cheap.
CREATE TABLE node_metrics (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ts            DATETIME(6) NOT NULL,
    granularity   VARCHAR(8) NOT NULL DEFAULT 'raw',
    cpu_percent   DOUBLE NOT NULL DEFAULT 0,
    mem_used_kb   BIGINT NOT NULL DEFAULT 0,
    mem_total_kb  BIGINT NOT NULL DEFAULT 0,
    swap_used_kb  BIGINT NOT NULL DEFAULT 0,
    load1         DOUBLE NOT NULL DEFAULT 0,
    root_disk_pct DOUBLE NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY ix_node_metrics_gran_ts (granularity, ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
