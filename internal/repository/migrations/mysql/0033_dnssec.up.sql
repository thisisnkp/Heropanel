-- DNSSEC toggle per zone. When on, named.conf gives the zone a dnssec-policy and
-- BIND inline-signs it; the DS record is read back for the registrar.
ALTER TABLE dns_zones ADD COLUMN dnssec_enabled TINYINT NOT NULL DEFAULT 0;
