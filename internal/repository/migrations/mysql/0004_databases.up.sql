-- HeroPanel managed databases: instances, users, grants (MariaDB).
CREATE TABLE db_instances (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    owner_id   BIGINT UNSIGNED NOT NULL,
    engine     VARCHAR(16) NOT NULL DEFAULT 'mariadb',
    name       VARCHAR(64) NOT NULL,
    charset    VARCHAR(32) NOT NULL DEFAULT 'utf8mb4',
    status     VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_instances_uid (uid),
    UNIQUE KEY uq_db_instances_name (name),
    KEY ix_db_instances_owner (owner_id),
    CONSTRAINT fk_db_instances_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE db_users (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid        CHAR(26) NOT NULL,
    owner_id   BIGINT UNSIGNED NOT NULL,
    engine     VARCHAR(16) NOT NULL DEFAULT 'mariadb',
    username   VARCHAR(64) NOT NULL,
    host       VARCHAR(64) NOT NULL DEFAULT 'localhost',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_users_uid (uid),
    UNIQUE KEY uq_db_users_identity (engine, username, host),
    CONSTRAINT fk_db_users_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE db_grants (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    db_user_id     BIGINT UNSIGNED NOT NULL,
    db_instance_id BIGINT UNSIGNED NOT NULL,
    privileges     VARCHAR(255) NOT NULL DEFAULT 'ALL',
    created_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_db_grants (db_user_id, db_instance_id),
    CONSTRAINT fk_db_grants_user FOREIGN KEY (db_user_id) REFERENCES db_users(id) ON DELETE CASCADE,
    CONSTRAINT fk_db_grants_instance FOREIGN KEY (db_instance_id) REFERENCES db_instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
