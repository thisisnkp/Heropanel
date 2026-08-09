ALTER TABLE sessions
    DROP FOREIGN KEY fk_sessions_impersonator,
    DROP COLUMN impersonator_user_id;
