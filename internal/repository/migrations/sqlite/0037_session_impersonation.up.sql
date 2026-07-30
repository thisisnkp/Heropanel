-- Impersonation sessions. When an administrator "acts as" another user, the
-- panel mints a normal session for the target user but stamps it with the real
-- admin's id here. A NULL means an ordinary self session. The acting identity is
-- still sessions.user_id (so the impersonator genuinely operates with the
-- target's permissions), while impersonator_user_id names the accountable human
-- the audit log attributes every mutation to.
ALTER TABLE sessions ADD COLUMN impersonator_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
