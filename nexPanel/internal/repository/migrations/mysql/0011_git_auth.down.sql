ALTER TABLE git_sources
    DROP COLUMN auth_kind,
    DROP COLUMN auth_username,
    DROP COLUMN credential_enc,
    DROP COLUMN public_key;
