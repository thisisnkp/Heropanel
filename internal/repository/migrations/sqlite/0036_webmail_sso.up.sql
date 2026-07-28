-- One-time Dovecot master-user sessions for passwordless webmail sign-on. Each
-- row is a short-lived master credential (a random per-session master user + a
-- bcrypt hash of its one-time password) bound to one mailbox; the panel renders
-- the complete set of live rows into Dovecot's master passwd-file. The plaintext
-- password is returned to the browser exactly once and never stored.
CREATE TABLE webmail_sso_sessions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         TEXT NOT NULL UNIQUE,
    mailbox     TEXT NOT NULL,
    master_user TEXT NOT NULL UNIQUE,
    pw_hash     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT NOT NULL
);
