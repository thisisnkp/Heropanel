-- One-time Dovecot master-user sessions for passwordless webmail sign-on. Each
-- row is a short-lived master credential (a random per-session master user + a
-- bcrypt hash of its one-time password) bound to one mailbox; the panel renders
-- the complete set of live rows into Dovecot's master passwd-file. The plaintext
-- password is returned to the browser exactly once and never stored.
CREATE TABLE webmail_sso_sessions (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    uid         VARCHAR(32) NOT NULL UNIQUE,
    mailbox     VARCHAR(320) NOT NULL,
    master_user VARCHAR(64) NOT NULL UNIQUE,
    pw_hash     VARCHAR(255) NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at  DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
