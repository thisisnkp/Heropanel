-- Ephemeral Adminer/phpMyAdmin sign-on sessions (MariaDB).
--
-- HeroPanel deliberately does not store database user passwords. A hand-off
-- instead mints a throwaway MariaDB account scoped to one database, hands those
-- credentials to Adminer, and drops the account when the session expires. The
-- panel being compromised therefore does not hand over every customer's standing
-- database password.
--
-- Rows here exist so the sweeper knows what to drop; they hold no secret.
CREATE TABLE db_sso_sessions (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid            CHAR(26) NOT NULL,
    db_instance_id BIGINT UNSIGNED NOT NULL,
    username       VARCHAR(80) NOT NULL,
    created_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at     DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_sso_uid (uid),
    UNIQUE KEY uq_db_sso_username (username),
    KEY idx_db_sso_expires (expires_at),
    CONSTRAINT fk_db_sso_instance FOREIGN KEY (db_instance_id) REFERENCES db_instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
