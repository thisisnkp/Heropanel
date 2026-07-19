-- SQLite < 3.35 cannot DROP COLUMN; the added column is additive and harmless.
UPDATE app_runtimes SET health_path = '';
