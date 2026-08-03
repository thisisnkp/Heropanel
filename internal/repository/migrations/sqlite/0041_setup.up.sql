-- panel_setup records the operator's first-run infrastructure choices, captured
-- by the post-install setup wizard: which webserver and database engine the
-- panel provisions for hosted sites, and whether DNS and mail are managed from
-- here. It is a single row (id = 1); completed_at IS NULL until the wizard is
-- finished, which is what gates the panel behind the wizard on first run.
CREATE TABLE panel_setup (
    id           INTEGER PRIMARY KEY,
    webserver    TEXT NOT NULL DEFAULT '',
    db_engine    TEXT NOT NULL DEFAULT '',
    manage_dns   INTEGER NOT NULL DEFAULT 0,
    create_mail  INTEGER NOT NULL DEFAULT 0,
    license_key  TEXT NOT NULL DEFAULT '',
    completed_at TEXT NULL,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
