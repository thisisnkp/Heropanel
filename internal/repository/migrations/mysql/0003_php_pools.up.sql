-- HeroPanel PHP module: per-site PHP-FPM pools (MariaDB).
CREATE TABLE php_pools (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    site_id         BIGINT UNSIGNED NOT NULL,
    php_version     VARCHAR(8) NOT NULL,
    pm              VARCHAR(16) NOT NULL DEFAULT 'ondemand',
    pm_max_children INT NOT NULL DEFAULT 10,
    memory_limit_mb INT NOT NULL DEFAULT 256,
    socket_path     VARCHAR(255) NOT NULL,
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_php_pools_site (site_id),
    CONSTRAINT fk_php_pools_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
