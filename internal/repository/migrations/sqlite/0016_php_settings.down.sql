-- SQLite gained ALTER TABLE ... DROP COLUMN in 3.35; modernc.org/sqlite is well
-- past that, so these are plain drops rather than the old table-rebuild dance.
ALTER TABLE php_pools DROP COLUMN opcache_jit;
ALTER TABLE php_pools DROP COLUMN opcache_enabled;
ALTER TABLE php_pools DROP COLUMN ini_overrides;
ALTER TABLE php_pools DROP COLUMN pm_idle_timeout_sec;
ALTER TABLE php_pools DROP COLUMN pm_max_requests;
ALTER TABLE php_pools DROP COLUMN pm_max_spare_servers;
ALTER TABLE php_pools DROP COLUMN pm_min_spare_servers;
ALTER TABLE php_pools DROP COLUMN pm_start_servers;
