-- Parked domains: a domain the operator has registered ownership of in the
-- panel, independent of any site (SQLite).
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
-- (`_nexpanel-challenge.<fqdn>` = `nexpanel-verify=<challenge_token>`);
-- verification re-reads live DNS (internal/domain's VerifyParked, mirroring
-- internal/mail's DKIM CheckDNS) rather than trusting anything stored here.
CREATE TABLE parked_domains (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    uid             TEXT NOT NULL UNIQUE,
    owner_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fqdn            TEXT NOT NULL UNIQUE,
    status          TEXT NOT NULL DEFAULT 'unverified',
    challenge_token TEXT NOT NULL,
    verified_at     TEXT,
    site_id         INTEGER REFERENCES sites(id) ON DELETE SET NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_parked_domains_owner ON parked_domains(owner_id);
CREATE INDEX ix_parked_domains_site ON parked_domains(site_id);
CREATE INDEX ix_parked_domains_status ON parked_domains(status);
