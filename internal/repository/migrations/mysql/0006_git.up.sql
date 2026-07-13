-- HeroPanel Git deployments: one source per site + append-only deploy history (MariaDB).

CREATE TABLE git_sources (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid            CHAR(26) NOT NULL,
    site_id        BIGINT UNSIGNED NOT NULL,
    repo_url       VARCHAR(512) NOT NULL,
    branch         VARCHAR(255) NOT NULL DEFAULT 'main',
    build_command  VARCHAR(1024) NOT NULL DEFAULT '',
    web_root       VARCHAR(255) NOT NULL DEFAULT '',
    webhook_secret CHAR(64) NOT NULL,
    created_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_git_sources_uid (uid),
    UNIQUE KEY uq_git_sources_site (site_id),
    CONSTRAINT fk_git_sources_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE git_deployments (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    site_id     BIGINT UNSIGNED NOT NULL,
    source_id   BIGINT UNSIGNED NOT NULL,
    commit_sha   VARCHAR(64) NOT NULL DEFAULT '',
    status       VARCHAR(16) NOT NULL DEFAULT 'pending',
    trigger_kind VARCHAR(16) NOT NULL DEFAULT 'manual',
    release_dir VARCHAR(512) NOT NULL DEFAULT '',
    log         MEDIUMTEXT NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_git_deployments_uid (uid),
    KEY ix_git_deployments_site (site_id, id),
    CONSTRAINT fk_git_deployments_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE,
    CONSTRAINT fk_git_deployments_source FOREIGN KEY (source_id) REFERENCES git_sources(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
