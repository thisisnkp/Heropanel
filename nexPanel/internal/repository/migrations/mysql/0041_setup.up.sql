-- panel_setup records the operator's first-run infrastructure choices, captured
-- by the post-install setup wizard: which webserver and database engine the
-- panel provisions for hosted sites, and whether DNS and mail are managed from
-- here. It is a single row (id = 1); completed_at IS NULL until the wizard is
-- finished, which is what gates the panel behind the wizard on first run.
CREATE TABLE panel_setup (
    id           INTEGER NOT NULL,
    webserver    VARCHAR(32) NOT NULL DEFAULT '',
    db_engine    VARCHAR(32) NOT NULL DEFAULT '',
    manage_dns   TINYINT(1) NOT NULL DEFAULT 0,
    create_mail  TINYINT(1) NOT NULL DEFAULT 0,
    license_key  VARCHAR(128) NOT NULL DEFAULT '',
    completed_at DATETIME(6) NULL,
    created_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
