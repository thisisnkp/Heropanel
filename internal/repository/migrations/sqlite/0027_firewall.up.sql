-- Firewall (SQLite): the desired host ruleset as rows the operator edits, plus
-- a single-row pending-apply state that drives the rollback timer.
--
-- A firewall is host-global (not site-scoped): one ordered list of rules over a
-- default-drop inet table. Rendering, applying and the auto-revert all live in
-- the security module; the broker only ever runs `nft`.
-- action: accept | drop. protocol: tcp | udp | any. port 0 = any.
-- source: an IPv4 address or CIDR, or '' for any.
CREATE TABLE firewall_rules (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    position   INTEGER NOT NULL DEFAULT 0,
    action     TEXT NOT NULL,
    protocol   TEXT NOT NULL DEFAULT 'tcp',
    port       INTEGER NOT NULL DEFAULT 0,
    source     TEXT NOT NULL DEFAULT '',
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- A single row (id=1) holding the pending rollback state. token/deadline are set
-- while an apply awaits confirmation; cleared on confirm or after the revert.
-- The security module's guard reverts when now >= deadline and the row is still
-- pending — so an unconfirmed change that locked the operator out undoes itself.
CREATE TABLE firewall_state (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    pending_token TEXT NOT NULL DEFAULT '',
    deadline      TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO firewall_state (id) VALUES (1);
