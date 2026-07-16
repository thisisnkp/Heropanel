-- HeroPanel authoritative DNS: zones and records (MariaDB).

CREATE TABLE dns_zones (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    owner_id    BIGINT UNSIGNED NOT NULL,
    name        VARCHAR(253) NOT NULL,
    primary_ns  VARCHAR(253) NOT NULL,
    admin_email VARCHAR(253) NOT NULL,
    serial      BIGINT UNSIGNED NOT NULL DEFAULT 1,
    refresh     INT UNSIGNED NOT NULL DEFAULT 3600,
    retry       INT UNSIGNED NOT NULL DEFAULT 900,
    expire      INT UNSIGNED NOT NULL DEFAULT 1209600,
    minimum     INT UNSIGNED NOT NULL DEFAULT 300,
    ttl         INT UNSIGNED NOT NULL DEFAULT 3600,
    status      VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_dns_zones_uid (uid),
    UNIQUE KEY uq_dns_zones_name (name),
    KEY ix_dns_zones_owner (owner_id),
    CONSTRAINT fk_dns_zones_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE dns_records (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    zone_id    BIGINT UNSIGNED NOT NULL,
    name       VARCHAR(253) NOT NULL DEFAULT '@',
    type       VARCHAR(10) NOT NULL,
    content    VARCHAR(2048) NOT NULL,
    ttl        INT UNSIGNED NOT NULL DEFAULT 3600,
    priority   INT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_dns_records_uid (uid),
    KEY ix_dns_records_zone (zone_id, id),
    CONSTRAINT fk_dns_records_zone FOREIGN KEY (zone_id) REFERENCES dns_zones(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
