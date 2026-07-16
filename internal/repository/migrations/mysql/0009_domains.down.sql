ALTER TABLE domains DROP COLUMN redirect_to;
ALTER TABLE domains DROP COLUMN redirect_code;
ALTER TABLE domains ALTER COLUMN force_https SET DEFAULT 1;
