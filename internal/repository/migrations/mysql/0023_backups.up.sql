-- Site backups (MariaDB). A row per archive in a site's chain: a 'full' resets
-- the chain, each 'incr' diffs against the one before (GNU tar snapshots, kept
-- broker-side). Restore replays full..target in order. remote_key is where the
-- sealed archive lives for its target (a filename for local, an object key for
-- s3). backup_configs drives the scheduler ticker: a site with enabled=1 gets a
-- backup whenever the newest one is older than interval_hours (incr, or full
-- every full_every runs), keeping `keep` chains.
CREATE TABLE backups (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    site_id    BIGINT UNSIGNED NOT NULL,
    level      VARCHAR(8) NOT NULL,
    status     VARCHAR(16) NOT NULL DEFAULT 'done',
    target     VARCHAR(16) NOT NULL DEFAULT 'local',
    remote_key VARCHAR(512) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_backups_uid (uid),
    KEY ix_backups_site (site_id, created_at),
    CONSTRAINT fk_backups_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE backup_configs (
    site_id        BIGINT UNSIGNED NOT NULL,
    enabled        TINYINT(1) NOT NULL DEFAULT 0,
    interval_hours INT UNSIGNED NOT NULL DEFAULT 24,
    target         VARCHAR(16) NOT NULL DEFAULT 'local',
    keep_chains    INT UNSIGNED NOT NULL DEFAULT 2,
    updated_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (site_id),
    CONSTRAINT fk_backup_configs_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
