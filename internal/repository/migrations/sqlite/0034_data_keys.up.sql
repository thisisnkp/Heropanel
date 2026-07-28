-- Rotating data-key envelope. Each row is a data key (AES-256) wrapped (sealed)
-- by the panel master key; one generation is active for new seals, older ones
-- keep opening existing blobs. Empty table => legacy single-key (hp1) mode.
CREATE TABLE data_keys (
    generation INTEGER PRIMARY KEY,
    wrapped    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S','now'))
);
