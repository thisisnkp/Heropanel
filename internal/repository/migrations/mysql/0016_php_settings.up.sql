-- Per-site PHP tuning: FPM sizing, php.ini overrides, OPcache (MariaDB).
--
-- All of it lives on php_pools because all of it is rendered into the one file
-- that already exists per site: /etc/php/<v>/fpm/pool.d/<user>.conf. A pool is
-- the only place PHP settings can be made per-site at all — see the notes on
-- scope below, and docs/16.
--
-- FPM sizing. `pm` picks the process manager and decides which of the rest even
-- apply: `dynamic` reads start/min_spare/max_spare, `ondemand` reads
-- process_idle_timeout, `static` reads neither. The defaults reproduce exactly
-- what the pool template hardcoded before this table existed (ondemand, 10
-- children), so an existing site's behaviour does not change on migration.
--
-- ini_overrides is a JSON object of allowlisted php.ini directives. It is not a
-- free-text php.ini: the pool file is what confines a site (open_basedir,
-- disable_functions), and a directive editor that could reach those would let a
-- tenant read every other site on the box. The allowlist lives in
-- internal/php/settings.go.
--
-- OPcache is split deliberately. opcache_enabled and opcache_jit are per-pool
-- because those directives are PHP_INI_ALL. The rest of OPcache's knobs
-- (memory_consumption, jit_buffer_size, max_accelerated_files) are PHP_INI_SYSTEM:
-- the FPM master allocates that shared memory once at startup, so they are a
-- property of the *version*, not of a site, and are deliberately not columns here.
--
-- ini_overrides is VARCHAR, not TEXT or JSON, for two reasons. A TEXT column
-- cannot carry a DEFAULT on every MariaDB this panel supports, and an ALTER that
-- adds one without a default leaves existing rows holding '' — which is not the
-- valid JSON the column is supposed to contain. VARCHAR takes the default
-- everywhere, and the content is a small allowlisted map, never prose.
ALTER TABLE php_pools
    ADD COLUMN pm_start_servers     INT UNSIGNED NOT NULL DEFAULT 2,
    ADD COLUMN pm_min_spare_servers INT UNSIGNED NOT NULL DEFAULT 1,
    ADD COLUMN pm_max_spare_servers INT UNSIGNED NOT NULL DEFAULT 3,
    ADD COLUMN pm_max_requests      INT UNSIGNED NOT NULL DEFAULT 500,
    ADD COLUMN pm_idle_timeout_sec  INT UNSIGNED NOT NULL DEFAULT 10,
    ADD COLUMN ini_overrides        VARCHAR(4096) NOT NULL DEFAULT '{}',
    ADD COLUMN opcache_enabled      TINYINT(1) NOT NULL DEFAULT 1,
    ADD COLUMN opcache_jit          VARCHAR(16) NOT NULL DEFAULT 'off';
