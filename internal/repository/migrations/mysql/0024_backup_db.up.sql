-- Database-in-backup (MariaDB).
--
-- A site's backup config may name one panel-managed database (db_uid); every
-- backup then carries a FULL dump of it as a second sealed object on the same
-- target (SQL dumps do not do incrementals — each one stands alone). On the
-- backup row, db_key is that object's key ('' = no dump) and db_name records
-- what the database was called, so a restore can offer it even after the
-- original database is gone.
ALTER TABLE backup_configs ADD COLUMN db_uid CHAR(26) NOT NULL DEFAULT '';
ALTER TABLE backups ADD COLUMN db_key VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE backups ADD COLUMN db_name VARCHAR(64) NOT NULL DEFAULT '';
