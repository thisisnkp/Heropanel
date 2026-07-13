-- HeroPanel SSL: certificates and ACME accounts (SQLite).
CREATE TABLE ssl_certificates (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         TEXT NOT NULL UNIQUE,
    owner_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider    TEXT NOT NULL DEFAULT 'self_signed',
    common_name TEXT NOT NULL,
    sans        TEXT NOT NULL DEFAULT '[]',
    is_wildcard INTEGER NOT NULL DEFAULT 0,
    cert_pem    TEXT NOT NULL DEFAULT '',
    privkey_enc BLOB,
    issued_at   TEXT,
    expires_at  TEXT,
    auto_renew  INTEGER NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'valid',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX uq_ssl_cn ON ssl_certificates(common_name);
CREATE INDEX ix_ssl_owner ON ssl_certificates(owner_id);

CREATE TABLE acme_accounts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    provider        TEXT NOT NULL DEFAULT 'letsencrypt',
    email           TEXT NOT NULL DEFAULT '',
    account_key_enc BLOB,
    directory_url   TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
