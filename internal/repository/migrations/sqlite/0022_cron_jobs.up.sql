-- Scheduled jobs (SQLite). A site-scoped cron job the broker renders into a
-- systemd .timer + oneshot .service; systemd owns the schedule, this is the
-- definition the panel renders from.
CREATE TABLE cron_jobs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    site_id    INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    command    TEXT NOT NULL,
    schedule   TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_cron_jobs_site ON cron_jobs (site_id);
