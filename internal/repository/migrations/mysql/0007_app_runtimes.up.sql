-- HeroPanel app runtimes: a supervised process + reverse-proxy target per site (MariaDB).

CREATE TABLE app_runtimes (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    site_id    BIGINT UNSIGNED NOT NULL,
    runtime    VARCHAR(16) NOT NULL DEFAULT 'generic',
    command    VARCHAR(1024) NOT NULL,
    port       INT UNSIGNED NOT NULL,
    env        TEXT NOT NULL,
    status     VARCHAR(16) NOT NULL DEFAULT 'stopped',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_app_runtimes_uid (uid),
    UNIQUE KEY uq_app_runtimes_site (site_id),
    CONSTRAINT fk_app_runtimes_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
