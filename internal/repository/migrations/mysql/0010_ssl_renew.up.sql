-- Remember how a certificate was obtained so the renewer can repeat it (MariaDB).
-- An empty webroot means the cert was issued via DNS-01 (and wildcards always are).
ALTER TABLE ssl_certificates ADD COLUMN webroot VARCHAR(512) NOT NULL DEFAULT '';
