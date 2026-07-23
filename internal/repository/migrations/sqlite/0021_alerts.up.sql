-- Metric alerts: threshold rules and the events they fire (SQLite).
--
-- A rule watches one metric, compares it against a threshold, and fires only when
-- the breach has persisted for for_sec seconds (so a one-tick spike pages nobody).
-- notify_target_enc is the notification target sealed with the panel's data key
-- (a Telegram token is a standing credential); the 'log' kind needs no target.
CREATE TABLE alert_rules (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    uid               TEXT NOT NULL UNIQUE,
    name              TEXT NOT NULL,
    metric            TEXT NOT NULL,
    op                TEXT NOT NULL DEFAULT 'gt',
    threshold         REAL NOT NULL,
    for_sec           INTEGER NOT NULL DEFAULT 0,
    enabled           INTEGER NOT NULL DEFAULT 1,
    notify_kind       TEXT NOT NULL DEFAULT 'log',
    notify_target_enc TEXT,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE alert_events (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_uid TEXT NOT NULL,
    state    TEXT NOT NULL,
    value    REAL NOT NULL DEFAULT 0,
    at       TEXT NOT NULL
);
CREATE INDEX ix_alert_events_at ON alert_events (at);
CREATE INDEX ix_alert_events_rule ON alert_events (rule_uid, at);
