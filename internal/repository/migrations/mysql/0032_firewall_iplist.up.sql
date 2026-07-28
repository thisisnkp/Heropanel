-- Geo/IP allow-block lists enforced as nftables sets in the firewall.
CREATE TABLE firewall_iplist (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    uid        VARCHAR(32) NOT NULL UNIQUE,
    cidr       VARCHAR(64) NOT NULL,
    mode       VARCHAR(16) NOT NULL DEFAULT 'block',
    comment    VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
