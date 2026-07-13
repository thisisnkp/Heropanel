-- HeroPanel SSL: certificates and ACME accounts (MariaDB).
CREATE TABLE ssl_certificates (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    owner_id    BIGINT UNSIGNED NOT NULL,
    provider    VARCHAR(20) NOT NULL DEFAULT 'self_signed',
    common_name VARCHAR(253) NOT NULL,
    sans        JSON NOT NULL,
    is_wildcard TINYINT(1) NOT NULL DEFAULT 0,
    cert_pem    MEDIUMTEXT NOT NULL,
    privkey_enc VARBINARY(8192) NULL,
    issued_at   DATETIME(6) NULL,
    expires_at  DATETIME(6) NULL,
    auto_renew  TINYINT(1) NOT NULL DEFAULT 1,
    status      VARCHAR(16) NOT NULL DEFAULT 'valid',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_ssl_uid (uid),
    UNIQUE KEY uq_ssl_cn (common_name),
    KEY ix_ssl_owner (owner_id),
    CONSTRAINT fk_ssl_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE acme_accounts (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    provider        VARCHAR(20) NOT NULL DEFAULT 'letsencrypt',
    email           VARCHAR(255) NOT NULL DEFAULT '',
    account_key_enc VARBINARY(8192) NULL,
    directory_url   VARCHAR(255) NOT NULL DEFAULT '',
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
