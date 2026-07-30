-- Installed marketplace modules. This is the panel's own record of which
-- third-party modules the operator installed and whether each is enabled —
-- distinct from the runtime registry, which advertises the capabilities of
-- modules that have a live provider. A module is keyed by its slug (its stable
-- identity, unique across the catalog); publisher_key records the trusted key
-- that vouched for the manifest at install time, so provenance survives in the
-- record and not just the log.
CREATE TABLE modules (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    version       TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT '',
    state         TEXT NOT NULL DEFAULT 'installed',
    publisher_key TEXT NOT NULL DEFAULT '',
    installed_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
