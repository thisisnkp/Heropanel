-- Rotating data-key envelope. See the SQLite copy for the design.
CREATE TABLE data_keys (
    generation INT NOT NULL PRIMARY KEY,
    wrapped    TEXT NOT NULL,
    created_at VARCHAR(32) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
