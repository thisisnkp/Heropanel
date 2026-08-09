-- App reverse-proxy wiring (SQLite).
--
-- A proxy site can be backed by a one-click Docker app instead of a systemd app
-- runtime. app_project links the site to the compose stack that serves it; the
-- vhost's upstream is then resolved live at render time from the app's published
-- loopback port, so it can never go stale if the app is redeployed on a new port.
-- NULL means an ordinary site with no app behind it — every existing row.
ALTER TABLE sites ADD COLUMN app_project TEXT NULL;
