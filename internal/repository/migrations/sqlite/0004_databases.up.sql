-- HeroPanel managed databases: instances, users, grants (SQLite).
CREATE TABLE db_instances (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    engine     TEXT NOT NULL DEFAULT 'mariadb',
    name       TEXT NOT NULL UNIQUE,
    charset    TEXT NOT NULL DEFAULT 'utf8mb4',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX ix_db_instances_owner ON db_instances(owner_id);

CREATE TABLE db_users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    engine     TEXT NOT NULL DEFAULT 'mariadb',
    username   TEXT NOT NULL,
    host       TEXT NOT NULL DEFAULT 'localhost',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (engine, username, host)
);

CREATE TABLE db_grants (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    db_user_id   INTEGER NOT NULL REFERENCES db_users(id) ON DELETE CASCADE,
    db_instance_id INTEGER NOT NULL REFERENCES db_instances(id) ON DELETE CASCADE,
    privileges   TEXT NOT NULL DEFAULT 'ALL',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (db_user_id, db_instance_id)
);
