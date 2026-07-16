-- HeroPanel authoritative DNS: zones and records (SQLite).

CREATE TABLE dns_zones (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         TEXT NOT NULL UNIQUE,
    owner_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL UNIQUE,
    primary_ns  TEXT NOT NULL,
    admin_email TEXT NOT NULL,
    serial      INTEGER NOT NULL DEFAULT 1,
    refresh     INTEGER NOT NULL DEFAULT 3600,
    retry       INTEGER NOT NULL DEFAULT 900,
    expire      INTEGER NOT NULL DEFAULT 1209600,
    minimum     INTEGER NOT NULL DEFAULT 300,
    ttl         INTEGER NOT NULL DEFAULT 3600,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_dns_zones_owner ON dns_zones(owner_id);

CREATE TABLE dns_records (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    zone_id    INTEGER NOT NULL REFERENCES dns_zones(id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '@',
    type       TEXT NOT NULL,
    content    TEXT NOT NULL,
    ttl        INTEGER NOT NULL DEFAULT 3600,
    priority   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_dns_records_zone ON dns_records(zone_id, id);
