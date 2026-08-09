-- WebAuthn / passkeys (SQLite).
--
-- One row per registered authenticator. credential_id (base64url of the raw
-- id) is unique and is what an unauthenticated login looks a user up by;
-- public_key is the COSE key (base64) the assertion signature is verified
-- against; sign_count is the clone-detection counter, bumped on every login.
CREATE TABLE webauthn_credentials (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uid           TEXT NOT NULL UNIQUE,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    public_key    TEXT NOT NULL,
    sign_count    INTEGER NOT NULL DEFAULT 0,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_webauthn_user ON webauthn_credentials (user_id);
