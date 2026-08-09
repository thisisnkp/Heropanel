-- Per-site WAF (ModSecurity + OWASP CRS) toggle.
ALTER TABLE sites ADD COLUMN waf_enabled TINYINT NOT NULL DEFAULT 0;
