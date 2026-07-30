ALTER TABLE users
    DROP FOREIGN KEY fk_users_parent,
    DROP INDEX ix_users_parent,
    DROP COLUMN parent_user_id;
