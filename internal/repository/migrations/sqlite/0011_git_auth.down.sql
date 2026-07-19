-- SQLite < 3.35 cannot DROP COLUMN; blank the credentials instead so rolling
-- back does not leave sealed secrets behind in an unused column.
UPDATE git_sources SET auth_kind = 'none', auth_username = '', credential_enc = '', public_key = '';
