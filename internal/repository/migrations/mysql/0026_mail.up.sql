-- Mail module (MariaDB): virtual domains, accounts, aliases/forwarders.
--
-- The panel is the source of truth; Postfix and Dovecot read rendered flat
-- maps (not this database), so mail keeps flowing when the panel is down.
-- password_hash is a Dovecot-scheme hash ({BLF-CRYPT}), write-only in the API.
-- dkim_private is SEALED with the panel data key (write-only); dkim_public is
-- the displayable TXT record value.
CREATE TABLE mail_domains (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid           CHAR(26) NOT NULL,
    owner_id      BIGINT UNSIGNED NOT NULL,
    domain        VARCHAR(255) NOT NULL,
    dkim_selector VARCHAR(63) NOT NULL DEFAULT 'hp1',
    dkim_private  TEXT NOT NULL,
    dkim_public   TEXT NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mail_domains_uid (uid),
    UNIQUE KEY uq_mail_domains_domain (domain),
    CONSTRAINT fk_mail_domains_owner FOREIGN KEY (owner_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE mail_accounts (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid           CHAR(26) NOT NULL,
    domain_id     BIGINT UNSIGNED NOT NULL,
    local_part    VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    quota_mb      INT UNSIGNED NOT NULL DEFAULT 1024,
    status        VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mail_accounts_uid (uid),
    UNIQUE KEY uq_mail_accounts_addr (domain_id, local_part),
    CONSTRAINT fk_mail_accounts_domain FOREIGN KEY (domain_id) REFERENCES mail_domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- An alias points source@domain at a destination address. An internal
-- destination is an alias; an external one is a forwarder — same mechanism
-- (postfix virtual_alias_maps), one table.
CREATE TABLE mail_aliases (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    domain_id   BIGINT UNSIGNED NOT NULL,
    source      VARCHAR(64) NOT NULL,
    destination VARCHAR(320) NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_mail_aliases_uid (uid),
    UNIQUE KEY uq_mail_aliases_row (domain_id, source, destination),
    CONSTRAINT fk_mail_aliases_domain FOREIGN KEY (domain_id) REFERENCES mail_domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
