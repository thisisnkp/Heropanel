-- Panel self-backup (SQLite): a row per sealed snapshot of the panel's own
-- database. Every snapshot is full and stands alone — no chains. Restore is
-- deliberately out-of-band (`hpd decrypt` + documented manual steps): a panel
-- that needs its database back cannot be trusted to serve that request.
CREATE TABLE panel_backups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    target     TEXT NOT NULL DEFAULT 'local',
    remote_key TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
