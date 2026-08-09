-- Self-update history (MySQL/MariaDB).
--
-- One row per attempt to move this installation to another release. The table
-- exists for two reasons that a log line cannot serve:
--
--   * the swap happens *outside* npd. np-installer runs as a detached systemd
--     unit precisely so it survives npd and the broker being restarted under it
--     (see docs/26), which means the process that started the update is gone
--     before it finishes. The row is how the panel that comes back up knows what
--     was attempted and whether it worked.
--   * an update that rolled back is the most important thing an operator can be
--     told, and it is exactly the case where nobody was watching. `state` keeps
--     that answer until someone reads it.
--
-- from_version/to_version are recorded rather than derived: after a rollback the
-- running binary is the old one again, so "what did we come from" is otherwise
-- unrecoverable.
--
-- No foreign key to users: an update may be started by the auto-check path with
-- no actor at all, and an actor who is later deleted must not take the history
-- of a release change with them. The audit chain holds who pressed the button.
CREATE TABLE panel_updates (
    id           BIGINT NOT NULL AUTO_INCREMENT,
    uid          VARCHAR(64) NOT NULL,
    from_version VARCHAR(64) NOT NULL,
    to_version   VARCHAR(64) NOT NULL,
    channel      VARCHAR(32) NOT NULL DEFAULT '',
    -- staged | applying | succeeded | failed | rolled_back
    state        VARCHAR(16) NOT NULL DEFAULT 'staged',
    error        TEXT NOT NULL,
    started_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at  DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_panel_updates_uid (uid),
    -- The panel asks "is one in flight?" and "what happened last?" on every load
    -- of the updates card, and both are answered by ordering on this.
    KEY idx_panel_updates_started (started_at DESC),
    KEY idx_panel_updates_state (state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
