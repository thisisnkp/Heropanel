-- SSH host-key pinning for git sources (MySQL/MariaDB).
-- See the SQLite copy for the column's meaning. TEXT cannot carry a DEFAULT on
-- MariaDB, so host_key is VARCHAR(4096) — ample for several known_hosts lines.
ALTER TABLE git_sources ADD COLUMN host_key VARCHAR(4096) NOT NULL DEFAULT '';
