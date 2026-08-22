-- One-time tickets for the phpMyAdmin hand-off (SQLite).
--
-- A ticket is a capability to open ONE database, once, within a minute. It is
-- not a credential: redeeming it is what mints the throwaway MariaDB account,
-- so nothing here is a password and no password is ever stored.
--
-- That ordering is the point. The older hand-off returned live credentials to
-- the browser to be POSTed at a login form; phpMyAdmin's CSRF token makes that
-- unreliable, and it put a working database password in a URL-adjacent place.
-- With a ticket, phpMyAdmin's own signon script redeems it server-side over
-- loopback and the password never leaves the host.
--
-- Only the SHA-256 of the ticket is stored, as with session and API-key tokens:
-- a datastore that leaks must not hand anyone a usable ticket, even for the
-- sixty seconds one lives.
CREATE TABLE db_sso_tickets (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    uid            TEXT NOT NULL UNIQUE,
    db_instance_id INTEGER NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
    ticket_hash    TEXT NOT NULL UNIQUE,
    -- Who asked. A hand-off is a database login; the audit chain records the
    -- request, and this ties the redemption back to the same actor.
    actor_user_id  INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at     TEXT NOT NULL,
    -- NULL until redeemed. Single use is enforced by an UPDATE conditional on
    -- this being NULL, so two concurrent redemptions cannot both succeed.
    redeemed_at    TEXT
);

CREATE INDEX idx_db_sso_tickets_expires ON db_sso_tickets (expires_at);
