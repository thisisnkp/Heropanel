-- Outbound event webhooks. A subscription (webhooks) names an endpoint, the
-- resource types it wants ("*" = all), and a signing secret sealed with the
-- panel data key. Each matching audit event produces a webhook_deliveries row,
-- which the background dispatcher POSTs (HMAC-signed) and retries with backoff
-- until delivered or exhausted — the deliveries table is both the queue and the
-- audit trail of what was sent where.
CREATE TABLE webhooks (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    owner_id    BIGINT UNSIGNED NOT NULL,
    url         VARCHAR(2048) NOT NULL,
    secret_enc  TEXT NOT NULL,
    events      TEXT NOT NULL,
    active      TINYINT NOT NULL DEFAULT 1,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_webhooks_uid (uid),
    KEY ix_webhooks_owner (owner_id),
    CONSTRAINT fk_webhooks_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE webhook_deliveries (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid             CHAR(26) NOT NULL,
    webhook_id      BIGINT UNSIGNED NOT NULL,
    event           VARCHAR(255) NOT NULL,
    resource_type   VARCHAR(64) NOT NULL DEFAULT '',
    resource_id     VARCHAR(64) NOT NULL DEFAULT '',
    payload         MEDIUMTEXT NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts        INT NOT NULL DEFAULT 0,
    response_code   INT NOT NULL DEFAULT 0,
    error           TEXT NOT NULL,
    next_attempt_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    delivered_at    DATETIME(6) NULL,
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_webhook_deliveries_uid (uid),
    KEY ix_webhook_deliveries_due (status, next_attempt_at),
    KEY ix_webhook_deliveries_hook (webhook_id, id),
    CONSTRAINT fk_webhook_deliveries_hook FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
