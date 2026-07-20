-- Recorded terminal sessions (SQLite).
--
-- The panel can host an interactive shell as a site's Linux user, which is the
-- most powerful thing it hands out. A recording is the audit artifact for that:
-- the hash chain already records *that* a session happened and as whom, but not
-- what was done in it.
--
-- Only metadata lives here. The recording itself is an asciicast v2 file on
-- disk, because terminal output is unbounded and a database is the wrong place
-- for it; `path` is relative to the configured recordings directory so the
-- directory can be moved without rewriting rows.
--
-- Recordings capture keystrokes as well as output, so input typed while the
-- terminal had echo disabled — i.e. at a password prompt — is redacted before it
-- is ever written. See internal/terminal/recording.go.
--
-- Rows are deleted by the retention sweeper (default 30 days) together with
-- their file; expires_at is stored rather than computed so changing the
-- retention setting does not retroactively destroy recordings a shorter policy
-- would already have removed.
CREATE TABLE terminal_recordings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uid           TEXT NOT NULL UNIQUE,
    site_id       INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    -- Who opened it, denormalised: the recording must stay attributable after
    -- the account is deleted, which is exactly when it matters most.
    actor_user_id INTEGER NOT NULL DEFAULT 0,
    actor_email   TEXT NOT NULL DEFAULT '',
    actor_ip      TEXT NOT NULL DEFAULT '',
    -- The Linux account the shell actually ran as.
    system_user   TEXT NOT NULL DEFAULT '',
    path          TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    -- The recording hit its size cap or a write error, so it is incomplete. The
    -- UI says so rather than presenting a partial session as the whole thing.
    truncated     INTEGER NOT NULL DEFAULT 0,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    ended_at      TEXT NOT NULL DEFAULT '',
    expires_at    TEXT NOT NULL
);

CREATE INDEX idx_term_rec_site    ON terminal_recordings (site_id, started_at);
CREATE INDEX idx_term_rec_expires ON terminal_recordings (expires_at);
