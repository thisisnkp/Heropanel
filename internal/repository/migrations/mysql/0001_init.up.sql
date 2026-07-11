-- HeroPanel Phase-0 schema (MariaDB/MySQL dialect).
-- Identity, RBAC, sessions, API keys, audit, settings, jobs.

CREATE TABLE users (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid           CHAR(26) NOT NULL,
    email         VARCHAR(255) NOT NULL,
    username      VARCHAR(64) NOT NULL,
    display_name  VARCHAR(255) NOT NULL DEFAULT '',
    password_hash VARCHAR(255) NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    totp_secret_enc VARBINARY(255) NULL,
    totp_enabled  TINYINT(1) NOT NULL DEFAULT 0,
    failed_logins INT NOT NULL DEFAULT 0,
    locked_until  DATETIME(6) NULL,
    last_login_at DATETIME(6) NULL,
    last_login_ip VARCHAR(45) NOT NULL DEFAULT '',
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleted_at    DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_uid (uid),
    UNIQUE KEY uq_users_email (email),
    UNIQUE KEY uq_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE roles (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid         CHAR(26) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    slug        VARCHAR(128) NOT NULL,
    is_system   TINYINT(1) NOT NULL DEFAULT 0,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_roles_uid (uid),
    UNIQUE KEY uq_roles_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE permissions (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    slug        VARCHAR(128) NOT NULL,
    resource    VARCHAR(64) NOT NULL,
    action      VARCHAR(64) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    UNIQUE KEY uq_permissions_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE role_permissions (
    role_id       BIGINT UNSIGNED NOT NULL,
    permission_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    CONSTRAINT fk_rp_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    CONSTRAINT fk_rp_perm FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE user_roles (
    user_id    BIGINT UNSIGNED NOT NULL,
    role_id    BIGINT UNSIGNED NOT NULL,
    scope_type VARCHAR(20) NOT NULL DEFAULT 'global',
    scope_id   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, role_id, scope_type, scope_id),
    CONSTRAINT fk_ur_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_ur_role FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE sessions (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid          CHAR(26) NOT NULL,
    user_id      BIGINT UNSIGNED NOT NULL,
    token_hash   CHAR(64) NOT NULL,
    ip           VARCHAR(45) NOT NULL DEFAULT '',
    user_agent   VARCHAR(512) NOT NULL DEFAULT '',
    device_label VARCHAR(128) NOT NULL DEFAULT '',
    created_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_seen_at DATETIME(6) NULL,
    expires_at   DATETIME(6) NOT NULL,
    revoked_at   DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_sessions_uid (uid),
    UNIQUE KEY uq_sessions_token (token_hash),
    KEY ix_sessions_user (user_id),
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE api_keys (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid          CHAR(26) NOT NULL,
    user_id      BIGINT UNSIGNED NOT NULL,
    name         VARCHAR(128) NOT NULL,
    prefix       VARCHAR(32) NOT NULL,
    key_hash     CHAR(64) NOT NULL,
    scopes       JSON NOT NULL,
    last_used_at DATETIME(6) NULL,
    expires_at   DATETIME(6) NULL,
    revoked_at   DATETIME(6) NULL,
    created_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_api_keys_uid (uid),
    UNIQUE KEY uq_api_keys_prefix (prefix),
    KEY ix_api_keys_user (user_id),
    CONSTRAINT fk_api_keys_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE audit_log (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid           CHAR(26) NOT NULL,
    actor_user_id BIGINT UNSIGNED NULL,
    actor_ip      VARCHAR(45) NOT NULL DEFAULT '',
    actor_kind    VARCHAR(20) NOT NULL DEFAULT 'user',
    action        VARCHAR(128) NOT NULL,
    resource_type VARCHAR(64) NOT NULL DEFAULT '',
    resource_id   VARCHAR(64) NOT NULL DEFAULT '',
    outcome       VARCHAR(20) NOT NULL DEFAULT 'success',
    detail        JSON NULL,
    prev_hash     CHAR(64) NOT NULL DEFAULT '',
    row_hash      CHAR(64) NOT NULL DEFAULT '',
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_audit_uid (uid),
    KEY ix_audit_actor (actor_user_id, created_at),
    KEY ix_audit_resource (resource_type, resource_id),
    CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE settings (
    `key`      VARCHAR(191) NOT NULL,
    value      JSON NOT NULL,
    category   VARCHAR(64) NOT NULL DEFAULT '',
    updated_by VARCHAR(64) NOT NULL DEFAULT '',
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE jobs (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid           CHAR(26) NOT NULL,
    type          VARCHAR(128) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'queued',
    owner_user_id BIGINT UNSIGNED NULL,
    resource_type VARCHAR(64) NOT NULL DEFAULT '',
    resource_id   VARCHAR(64) NOT NULL DEFAULT '',
    progress      TINYINT UNSIGNED NOT NULL DEFAULT 0,
    payload       JSON NOT NULL,
    result        JSON NULL,
    error         TEXT NULL,
    attempts      INT NOT NULL DEFAULT 0,
    started_at    DATETIME(6) NULL,
    finished_at   DATETIME(6) NULL,
    log_ref       VARCHAR(255) NOT NULL DEFAULT '',
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_jobs_uid (uid),
    KEY ix_jobs_status (status),
    KEY ix_jobs_owner (owner_user_id, created_at),
    CONSTRAINT fk_jobs_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
