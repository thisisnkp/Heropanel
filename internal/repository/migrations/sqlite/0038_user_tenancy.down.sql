DROP INDEX IF EXISTS ix_users_parent;
ALTER TABLE users DROP COLUMN parent_user_id;
