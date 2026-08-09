-- SQLite < 3.35 cannot DROP COLUMN; the added column is additive and harmless.
UPDATE ssl_certificates SET webroot = '';
