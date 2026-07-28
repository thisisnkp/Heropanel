-- WebAuthn / passkeys (MariaDB).
--
-- One row per registered authenticator. credential_id (base64url of the raw
-- id) is unique and is what an unauthenticated login looks a user up by;
-- public_key is the COSE key (base64) the assertion signature is verified
-- against; sign_count is the clone-detection counter, bumped on every login.
CREATE TABLE webauthn_credentials (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    uid           CHAR(26) NOT NULL,
    user_id       BIGINT UNSIGNED NOT NULL,
    credential_id VARCHAR(512) NOT NULL,
    public_key    TEXT NOT NULL,
    sign_count    INT UNSIGNED NOT NULL DEFAULT 0,
    name          VARCHAR(64) NOT NULL DEFAULT '',
    created_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_webauthn_uid (uid),
    UNIQUE KEY uq_webauthn_cred (credential_id),
    KEY ix_webauthn_user (user_id),
    CONSTRAINT fk_webauthn_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
