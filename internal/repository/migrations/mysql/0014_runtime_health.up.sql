-- App runtime health checks (MariaDB).
--
-- health_path is an HTTP path on the app's own port (e.g. "/healthz"). When set,
-- the panel probes it after applying or restarting a runtime instead of assuming
-- the unit came up: systemd reporting "started" only means the process was
-- spawned, not that the app is actually serving.
ALTER TABLE app_runtimes ADD COLUMN health_path VARCHAR(255) NOT NULL DEFAULT '';
