-- Per-site resource limits, enforced by a systemd cgroup slice (MariaDB).
--
-- These three are the ones a slice can actually enforce today:
--   cpu_quota_pct   -> CPUQuota=N%      (N% of ONE core; 200 = two full cores)
--   mem_limit_bytes -> MemoryMax=N      (the cgroup v2 hard limit; OOM-kills above it)
--   pids_max        -> TasksMax=N       (fork-bomb ceiling)
-- 0 means unlimited, which is also the default: a site that has never been given
-- limits must behave exactly as it did before this table existed.
--
-- docs/03 lists more (io_read_bps, io_write_bps, disk_quota_bytes, inode_quota,
-- bandwidth_month_bytes, php_workers_max). They are deliberately absent rather
-- than present-and-ignored: IO limits need a per-device path, disk/inode quotas
-- need filesystem quotas, and bandwidth needs the Monitor module. A column
-- nothing enforces is a promise the panel does not keep.
CREATE TABLE site_limits (
    site_id         BIGINT UNSIGNED NOT NULL,
    cpu_quota_pct   INT UNSIGNED NOT NULL DEFAULT 0,
    mem_limit_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
    pids_max        INT UNSIGNED NOT NULL DEFAULT 0,
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (site_id),
    CONSTRAINT fk_site_limits_site FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
