-- Mail module (SQLite): virtual domains, accounts, aliases/forwarders.
--
-- The panel is the source of truth; Postfix and Dovecot read rendered flat
-- maps (not this database), so mail keeps flowing when the panel is down.
-- password_hash is a Dovecot-scheme hash ({BLF-CRYPT}), write-only in the API.
-- dkim_private is SEALED with the panel data key (write-only); dkim_public is
-- the displayable TXT record value.
CREATE TABLE mail_domains (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uid           TEXT NOT NULL UNIQUE,
    owner_id      INTEGER NOT NULL REFERENCES users(id),
    domain        TEXT NOT NULL UNIQUE,
    dkim_selector TEXT NOT NULL DEFAULT 'hp1',
    dkim_private  TEXT NOT NULL DEFAULT '',
    dkim_public   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE mail_accounts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uid           TEXT NOT NULL UNIQUE,
    domain_id     INTEGER NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
    local_part    TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    quota_mb      INTEGER NOT NULL DEFAULT 1024,
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (domain_id, local_part)
);

-- An alias points source@domain at a destination address. An internal
-- destination is an alias; an external one is a forwarder — same mechanism
-- (postfix virtual_alias_maps), one table.
CREATE TABLE mail_aliases (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    domain_id  INTEGER NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
    source     TEXT NOT NULL,
    destination TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (domain_id, source, destination)
);
