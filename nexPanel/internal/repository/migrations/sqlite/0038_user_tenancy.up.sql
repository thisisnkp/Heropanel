-- Ownership tree for reseller tenancy. A user's parent_user_id is the account
-- that "owns" them: a reseller's clients point at the reseller, forming a tenant
-- subtree. NULL means a top-level account (an admin, or a standalone reseller).
-- A principal's visible scope is itself plus everything below it in this tree;
-- resource isolation is then "the resource's owner is in my visible set".
--
-- ON DELETE SET NULL: deleting a reseller must not cascade-delete their clients'
-- accounts (and thus everyone's sites). The clients are orphaned to top-level
-- instead, to be re-parented or cleaned up deliberately.
ALTER TABLE users ADD COLUMN parent_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX ix_users_parent ON users(parent_user_id);
