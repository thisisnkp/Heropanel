-- HeroPanel domain management: redirects + force-HTTPS semantics (SQLite).

ALTER TABLE domains ADD COLUMN redirect_to TEXT NOT NULL DEFAULT '';
ALTER TABLE domains ADD COLUMN redirect_code INTEGER NOT NULL DEFAULT 301;

-- Force-HTTPS is opt-in: redirecting to HTTPS before a certificate exists would
-- take the site offline, so existing rows are switched off and the API turns it
-- on explicitly (typically after SSL is issued).
UPDATE domains SET force_https = 0;
