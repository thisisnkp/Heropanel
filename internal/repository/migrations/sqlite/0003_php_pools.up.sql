-- HeroPanel PHP module: per-site PHP-FPM pools (SQLite).
CREATE TABLE php_pools (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id         INTEGER NOT NULL UNIQUE REFERENCES sites(id) ON DELETE CASCADE,
    php_version     TEXT NOT NULL,
    pm              TEXT NOT NULL DEFAULT 'ondemand',
    pm_max_children INTEGER NOT NULL DEFAULT 10,
    memory_limit_mb INTEGER NOT NULL DEFAULT 256,
    socket_path     TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
