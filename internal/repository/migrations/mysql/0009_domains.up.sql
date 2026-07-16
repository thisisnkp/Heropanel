-- HeroPanel domain management: redirects + force-HTTPS semantics (MariaDB).

ALTER TABLE domains ADD COLUMN redirect_to VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN redirect_code INT NOT NULL DEFAULT 301;

-- Force-HTTPS is opt-in: redirecting to HTTPS before a certificate exists would
-- take the site offline, so existing rows are switched off and the API turns it
-- on explicitly (typically after SSL is issued).
ALTER TABLE domains ALTER COLUMN force_https SET DEFAULT 0;
UPDATE domains SET force_https = 0;
