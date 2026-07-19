-- Private Git repositories: per-source credentials at rest (MariaDB).
--
-- auth_kind selects how the clone authenticates:
--   none    -- public https repo (the only slice before this migration)
--   token   -- HTTPS basic auth; auth_username + credential_enc holds the PAT
--   ssh_key -- SSH; credential_enc holds the panel-generated private key and
--              public_key is the half the operator adds as a repo deploy key
--
-- credential_enc is AES-256-GCM sealed by pkg/secrets, bound to
-- ("git_sources", id, "credential_enc"), so a ciphertext cannot be moved
-- between rows. It is never returned by the API.
ALTER TABLE git_sources
    ADD COLUMN auth_kind      VARCHAR(16)   NOT NULL DEFAULT 'none',
    ADD COLUMN auth_username  VARCHAR(190)  NOT NULL DEFAULT '',
    ADD COLUMN credential_enc TEXT          NOT NULL,
    ADD COLUMN public_key     TEXT          NOT NULL;
