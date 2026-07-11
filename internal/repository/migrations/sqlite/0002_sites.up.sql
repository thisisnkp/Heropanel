-- HeroPanel Phase-1 schema: sites, per-site Linux users, and domains (SQLite).

CREATE TABLE sites (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uid           TEXT NOT NULL UNIQUE,
    owner_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    primary_domain TEXT NOT NULL UNIQUE,
    type          TEXT NOT NULL DEFAULT 'static',
    deploy_mode   TEXT NOT NULL DEFAULT 'baremetal',
    status        TEXT NOT NULL DEFAULT 'provisioning',
    webserver     TEXT NOT NULL DEFAULT 'openlitespeed',
    document_root TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    deleted_at    TEXT
);
CREATE INDEX ix_sites_owner ON sites(owner_id);
CREATE INDEX ix_sites_status ON sites(status);

CREATE TABLE site_system_users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id    INTEGER NOT NULL UNIQUE REFERENCES sites(id) ON DELETE CASCADE,
    linux_user TEXT NOT NULL UNIQUE,
    linux_uid  INTEGER NOT NULL,
    home_dir   TEXT NOT NULL,
    shell      TEXT NOT NULL DEFAULT '/usr/sbin/nologin',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE domains (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         TEXT NOT NULL UNIQUE,
    site_id     INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    fqdn        TEXT NOT NULL UNIQUE,
    kind        TEXT NOT NULL DEFAULT 'primary',
    force_https INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_domains_site ON domains(site_id);
