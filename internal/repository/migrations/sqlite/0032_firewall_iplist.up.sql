-- Geo/IP allow-block lists enforced as nftables sets in the firewall.
CREATE TABLE firewall_iplist (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    uid        TEXT NOT NULL UNIQUE,
    cidr       TEXT NOT NULL,
    mode       TEXT NOT NULL DEFAULT 'block',
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
