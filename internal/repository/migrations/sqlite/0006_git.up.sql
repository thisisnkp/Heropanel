-- HeroPanel Git deployments: one source per site + append-only deploy history (SQLite).

CREATE TABLE git_sources (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    uid            TEXT NOT NULL UNIQUE,
    site_id        INTEGER NOT NULL UNIQUE REFERENCES sites(id) ON DELETE CASCADE,
    repo_url       TEXT NOT NULL,
    branch         TEXT NOT NULL DEFAULT 'main',
    build_command  TEXT NOT NULL DEFAULT '',
    web_root       TEXT NOT NULL DEFAULT '',
    webhook_secret TEXT NOT NULL,
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE git_deployments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uid         TEXT NOT NULL UNIQUE,
    site_id     INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    source_id   INTEGER NOT NULL REFERENCES git_sources(id) ON DELETE CASCADE,
    commit_sha   TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending',
    trigger_kind TEXT NOT NULL DEFAULT 'manual',
    release_dir TEXT NOT NULL DEFAULT '',
    log         TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at TEXT
);
CREATE INDEX ix_git_deployments_site ON git_deployments(site_id, id DESC);
