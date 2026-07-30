-- Outbound event webhooks. A subscription (webhooks) names an endpoint, the
-- resource types it wants ("*" = all), and a signing secret sealed with the
-- panel data key. Each matching audit event produces a webhook_deliveries row,
-- which the background dispatcher POSTs (HMAC-signed) and retries with backoff
-- until delivered or exhausted — the deliveries table is both the queue and the
-- audit trail of what was sent where.
CREATE TABLE webhooks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         TEXT NOT NULL UNIQUE,
    owner_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    secret_enc  TEXT NOT NULL,
    events      TEXT NOT NULL DEFAULT '["*"]',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_webhooks_owner ON webhooks(owner_id);

CREATE TABLE webhook_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    uid             TEXT NOT NULL UNIQUE,
    webhook_id      INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event           TEXT NOT NULL,
    resource_type   TEXT NOT NULL DEFAULT '',
    resource_id     TEXT NOT NULL DEFAULT '',
    payload         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    response_code   INTEGER NOT NULL DEFAULT 0,
    error           TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT NOT NULL DEFAULT (datetime('now')),
    delivered_at    TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_webhook_deliveries_due ON webhook_deliveries(status, next_attempt_at);
CREATE INDEX ix_webhook_deliveries_hook ON webhook_deliveries(webhook_id, id);
