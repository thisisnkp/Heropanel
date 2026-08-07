-- Parked domains: a domain the operator has registered ownership of in the
-- panel, independent of any site (MariaDB).
--
-- Today a domain only exists once it is attached to a site (see `domains` in
-- 0002_sites). This is the missing "registrar" half: park a domain here with no
-- site yet, prove ownership via a DNS TXT challenge, and it becomes a "free"
-- domain offered when creating a website — pick it and it attaches with no
-- warning, because ownership is already proven.
--
-- site_id is nullable and ON DELETE SET NULL, not CASCADE, deliberately: the
-- parked row represents proof-of-ownership work the operator already did
-- (the DNS TXT record they added at their own registrar/DNS host). Deleting the
-- site that used the domain must return it to the free pool, not throw away
-- that verification — the operator should not have to re-prove ownership just
-- because they deleted and want to recreate the site.
--
-- challenge_token is the secret half of the TXT challenge
-- (`_heropanel-challenge.<fqdn>` = `heropanel-verify=<challenge_token>`);
-- verification re-reads live DNS (internal/domain's VerifyParked, mirroring
-- internal/mail's DKIM CheckDNS) rather than trusting anything stored here.
CREATE TABLE parked_domains (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid             CHAR(26) NOT NULL,
    owner_id        BIGINT UNSIGNED NOT NULL,
    fqdn            VARCHAR(253) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'unverified',
    challenge_token VARCHAR(64) NOT NULL,
    verified_at     DATETIME(6) NULL,
    site_id         BIGINT UNSIGNED NULL,
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_parked_domains_uid (uid),
    UNIQUE KEY uq_parked_domains_fqdn (fqdn),
    KEY ix_parked_domains_owner (owner_id),
    KEY ix_parked_domains_site (site_id),
    KEY ix_parked_domains_status (status),
    CONSTRAINT fk_parked_domains_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_parked_domains_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
