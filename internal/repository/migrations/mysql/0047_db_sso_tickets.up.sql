-- One-time tickets for the phpMyAdmin hand-off (MySQL/MariaDB).
--
-- A ticket is a capability to open ONE database, once, within a minute. It is
-- not a credential: redeeming it is what mints the throwaway MariaDB account,
-- so nothing here is a password and no password is ever stored. Only the
-- SHA-256 of the ticket is kept, as with session and API-key tokens.
CREATE TABLE db_sso_tickets (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    uid            VARCHAR(64) NOT NULL UNIQUE,
    db_instance_id BIGINT UNSIGNED NOT NULL,
    ticket_hash    VARCHAR(64) NOT NULL UNIQUE,
    actor_user_id  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at     DATETIME NOT NULL,
    redeemed_at    DATETIME NULL,
    KEY idx_db_sso_tickets_expires (expires_at),
    CONSTRAINT fk_db_sso_tickets_db FOREIGN KEY (db_instance_id)
        REFERENCES db_instances (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
