-- Panel self-backup (MariaDB): a row per sealed snapshot of the panel's own
-- database. Every snapshot is full and stands alone — no chains. Restore is
-- deliberately out-of-band (`hpd decrypt` + documented manual steps): a panel
-- that needs its database back cannot be trusted to serve that request.
CREATE TABLE panel_backups (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    target     VARCHAR(16) NOT NULL DEFAULT 'local',
    remote_key VARCHAR(512) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_panel_backups_uid (uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
