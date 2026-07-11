-- HeroPanel Phase-1 schema: sites, per-site Linux users, and domains (MariaDB).

CREATE TABLE sites (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid            CHAR(26) NOT NULL,
    owner_id       BIGINT UNSIGNED NOT NULL,
    name           VARCHAR(191) NOT NULL,
    primary_domain VARCHAR(253) NOT NULL,
    type           VARCHAR(20) NOT NULL DEFAULT 'static',
    deploy_mode    VARCHAR(20) NOT NULL DEFAULT 'baremetal',
    status         VARCHAR(20) NOT NULL DEFAULT 'provisioning',
    webserver      VARCHAR(20) NOT NULL DEFAULT 'openlitespeed',
    document_root  VARCHAR(512) NOT NULL DEFAULT '',
    created_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at     DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_sites_uid (uid),
    UNIQUE KEY uq_sites_primary_domain (primary_domain),
    KEY ix_sites_owner (owner_id),
    KEY ix_sites_status (status),
    CONSTRAINT fk_sites_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE site_system_users (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    site_id    BIGINT UNSIGNED NOT NULL,
    linux_user VARCHAR(32) NOT NULL,
    linux_uid  INT UNSIGNED NOT NULL,
    home_dir   VARCHAR(512) NOT NULL,
    shell      VARCHAR(128) NOT NULL DEFAULT '/usr/sbin/nologin',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_ssu_site (site_id),
    UNIQUE KEY uq_ssu_linux_user (linux_user),
    CONSTRAINT fk_ssu_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE domains (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    site_id     BIGINT UNSIGNED NOT NULL,
    fqdn        VARCHAR(253) NOT NULL,
    kind        VARCHAR(20) NOT NULL DEFAULT 'primary',
    force_https TINYINT(1) NOT NULL DEFAULT 1,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_domains_uid (uid),
    UNIQUE KEY uq_domains_fqdn (fqdn),
    KEY ix_domains_site (site_id),
    CONSTRAINT fk_domains_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
