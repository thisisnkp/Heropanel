-- Ephemeral Adminer/phpMyAdmin sign-on sessions (SQLite).
--
-- HeroPanel deliberately does not store database user passwords. A hand-off
-- instead mints a throwaway MariaDB account scoped to one database, hands those
-- credentials to Adminer, and drops the account when the session expires. The
-- panel being compromised therefore does not hand over every customer's standing
-- database password.
--
-- Rows here exist so the sweeper knows what to drop; they hold no secret.
CREATE TABLE db_sso_sessions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    uid            TEXT NOT NULL UNIQUE,
    db_instance_id INTEGER NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
    username       TEXT NOT NULL UNIQUE,
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at     TEXT NOT NULL
);

CREATE INDEX idx_db_sso_expires ON db_sso_sessions (expires_at);
