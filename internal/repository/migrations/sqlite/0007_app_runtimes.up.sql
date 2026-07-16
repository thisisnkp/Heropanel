-- HeroPanel app runtimes: a supervised process + reverse-proxy target per site (SQLite).

CREATE TABLE app_runtimes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    site_id    INTEGER NOT NULL UNIQUE REFERENCES sites(id) ON DELETE CASCADE,
    runtime    TEXT NOT NULL DEFAULT 'generic',
    command    TEXT NOT NULL,
    port       INTEGER NOT NULL,
    env        TEXT NOT NULL DEFAULT '{}',
    status     TEXT NOT NULL DEFAULT 'stopped',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
