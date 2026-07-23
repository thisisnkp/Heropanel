-- Metric alerts: threshold rules and the events they fire (MariaDB).
--
-- A rule watches one metric (cpu | mem | swap | disk_root | load1), compares it
-- with an operator against a threshold, and fires when the breach has persisted
-- for for_sec seconds — the duration is what stops a one-tick spike from paging
-- anyone. notify_target_enc is the notification target (a webhook URL, a Telegram
-- bot token + chat) sealed with the panel's data key, because a Telegram token is
-- a standing credential; the 'log' kind needs no target and is always available.
CREATE TABLE alert_rules (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid               CHAR(26) NOT NULL,
    name              VARCHAR(191) NOT NULL,
    metric            VARCHAR(32) NOT NULL,
    op                VARCHAR(2) NOT NULL DEFAULT 'gt',
    threshold         DOUBLE NOT NULL,
    for_sec           INT UNSIGNED NOT NULL DEFAULT 0,
    enabled           TINYINT(1) NOT NULL DEFAULT 1,
    notify_kind       VARCHAR(16) NOT NULL DEFAULT 'log',
    notify_target_enc TEXT NULL,
    created_at        DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at        DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_alert_rules_uid (uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE alert_events (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rule_uid   CHAR(26) NOT NULL,
    state      VARCHAR(16) NOT NULL,
    value      DOUBLE NOT NULL DEFAULT 0,
    at         DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY ix_alert_events_at (at),
    KEY ix_alert_events_rule (rule_uid, at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
