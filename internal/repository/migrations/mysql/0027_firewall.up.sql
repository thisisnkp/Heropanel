-- Firewall (MariaDB): the desired host ruleset as rows the operator edits, plus
-- a single-row pending-apply state that drives the rollback timer.
--
-- A firewall is host-global (not site-scoped): one ordered list of rules over a
-- default-drop inet table. Rendering, applying and the auto-revert all live in
-- the security module; the broker only ever runs `nft`.
CREATE TABLE firewall_rules (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    position   INT NOT NULL DEFAULT 0,
    action     VARCHAR(8) NOT NULL,
    protocol   VARCHAR(8) NOT NULL DEFAULT 'tcp',
    port       INT UNSIGNED NOT NULL DEFAULT 0,
    source     VARCHAR(64) NOT NULL DEFAULT '',
    comment    VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_firewall_rules_uid (uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE firewall_state (
    id            TINYINT UNSIGNED NOT NULL,
    pending_token VARCHAR(64) NOT NULL DEFAULT '',
    deadline      VARCHAR(32) NOT NULL DEFAULT '',
    updated_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT ck_firewall_state_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO firewall_state (id) VALUES (1);
