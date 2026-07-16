-- SQLite < 3.35 cannot DROP COLUMN; recreating the table is out of scope for a
-- down migration here. The added columns are additive and harmless.
UPDATE domains SET redirect_to = '', redirect_code = 301;
