-- Scheduled jobs (MariaDB). Each row is a site-scoped cron job rendered into a
-- systemd .timer + oneshot .service by the broker. systemd owns the schedule and
-- the run state; this table is the definition the panel renders from.
CREATE TABLE cron_jobs (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    site_id    BIGINT UNSIGNED NOT NULL,
    name       VARCHAR(191) NOT NULL,
    command    VARCHAR(2000) NOT NULL,
    schedule   VARCHAR(128) NOT NULL,
    enabled    TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_cron_jobs_uid (uid),
    KEY ix_cron_jobs_site (site_id),
    CONSTRAINT fk_cron_jobs_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
