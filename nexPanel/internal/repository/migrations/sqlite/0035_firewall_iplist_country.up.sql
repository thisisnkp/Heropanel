-- Tag geo/IP entries with the ISO country they were bulk-imported from, so a
-- country import can be listed, re-imported (replaced) or removed as a unit.
-- Manually-added entries keep the empty default.
ALTER TABLE firewall_iplist ADD COLUMN country TEXT NOT NULL DEFAULT '';
