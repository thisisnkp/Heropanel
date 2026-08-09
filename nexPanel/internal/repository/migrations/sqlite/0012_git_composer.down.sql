-- SQLite < 3.35 cannot DROP COLUMN; the added column is additive and harmless.
UPDATE git_sources SET auto_composer = 1;
