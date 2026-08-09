-- Installed marketplace modules. This is the panel's own record of which
-- third-party modules the operator installed and whether each is enabled —
-- distinct from the runtime registry, which advertises the capabilities of
-- modules that have a live provider. A module is keyed by its slug (its stable
-- identity, unique across the catalog); publisher_key records the trusted key
-- that vouched for the manifest at install time, so provenance survives in the
-- record and not just the log.
CREATE TABLE modules (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    slug          VARCHAR(64) NOT NULL,
    name          VARCHAR(255) NOT NULL DEFAULT '',
    version       VARCHAR(64) NOT NULL DEFAULT '',
    category      VARCHAR(64) NOT NULL DEFAULT '',
    state         VARCHAR(16) NOT NULL DEFAULT 'installed',
    publisher_key VARCHAR(64) NOT NULL DEFAULT '',
    installed_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_modules_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
