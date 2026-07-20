-- Recorded terminal sessions (MySQL/MariaDB).
-- See the SQLite copy for the table's purpose and the redaction rule; the only
-- differences here are column types and the explicit InnoDB/utf8mb4 clause.
CREATE TABLE terminal_recordings (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    uid           VARCHAR(64) NOT NULL UNIQUE,
    site_id       BIGINT NOT NULL,
    actor_user_id BIGINT NOT NULL DEFAULT 0,
    actor_email   VARCHAR(320) NOT NULL DEFAULT '',
    actor_ip      VARCHAR(64) NOT NULL DEFAULT '',
    system_user   VARCHAR(64) NOT NULL DEFAULT '',
    path          VARCHAR(512) NOT NULL,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    truncated     TINYINT(1) NOT NULL DEFAULT 0,
    started_at    VARCHAR(32) NOT NULL DEFAULT '',
    ended_at      VARCHAR(32) NOT NULL DEFAULT '',
    expires_at    VARCHAR(32) NOT NULL,
    CONSTRAINT fk_term_rec_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE,
    INDEX idx_term_rec_site (site_id, started_at),
    INDEX idx_term_rec_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
