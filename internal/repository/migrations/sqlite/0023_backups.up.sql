-- Site backups (SQLite). A row per archive in a site's chain ('full' resets it,
-- 'incr' diffs against the previous); restore replays full..target in order.
-- backup_configs drives the scheduler ticker.
CREATE TABLE backups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    site_id    INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    level      TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'done',
    target     TEXT NOT NULL DEFAULT 'local',
    remote_key TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_backups_site ON backups (site_id, created_at);

CREATE TABLE backup_configs (
    site_id        INTEGER PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
    enabled        INTEGER NOT NULL DEFAULT 0,
    interval_hours INTEGER NOT NULL DEFAULT 24,
    target         TEXT NOT NULL DEFAULT 'local',
    keep_chains    INTEGER NOT NULL DEFAULT 2,
    updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);
