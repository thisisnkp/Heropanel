# 08 — Deployment Architecture

How HeroPanel runs, updates itself safely, and how the single-node design extends to fleets without a rewrite.

## 1. Runtime process topology (single node)

```
systemd
├── heropanel-broker.service     (root, always up, tiny)          Restart=always
├── heropanel.service            (User=heropanel, hpd core)       Restart=always, After=broker,mariadb,redis
├── heropanel-mod@docker.service    ┐
├── heropanel-mod@monitor.service   │ templated units, one per installed module
├── heropanel-mod@mail.service      │ enabled on demand; Restart=on-failure w/ backoff
├── heropanel-mod@dns.service       ┘
│
├── mariadb.service              (state)
├── redis.service                (cache/queue/pubsub)
├── lshttpd.service (OpenLiteSpeed)  ← serves panel :8443 and hosted sites
├── php-fpm pools (per site)     heropanel-site@<id>.slice groups site processes
└── postfix/dovecot/docker/...   (managed services, present if module installed)
```

- **Boot order** via `After=`/`Wants=`: broker + datastores → `hpd` → modules.
- **Socket activation** optional for the broker (start on first use) to shave idle RAM further.
- **cgroup slices**: each hosted site gets `heropanel-site@<id>.slice` carrying its `CPUQuota`, `MemoryMax`, `TasksMax`, `IO*` from `site_limits`. Panel processes live in their own slice, insulated from site load.

## 2. systemd unit design

Templated unit `heropanel-mod@.service` (one file, many instances):
```ini
[Unit]
Description=HeroPanel module %i
After=heropanel.service
BindsTo=heropanel.service           # module stops if core stops
PartOf=heropanel.service

[Service]
Type=notify
ExecStart=/opt/heropanel/modules/%i/hp-mod-%i --config /etc/heropanel/modules/%i.yaml
Restart=on-failure
RestartSec=2
StartLimitBurst=5                   # N crashes → enter failed → alert
# ── hardening (see doc 05 §8) ──
User=heropanel
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/heropanel/modules/%i /var/log/heropanel/modules /run/heropanel/modules
MemoryMax=128M
```
Core (`heropanel.service`) and broker (`heropanel-broker.service`) are separate, individually hardened units — **restarting any one never restarts the others** (satisfies "every service restartable independently").

## 3. Distribution & packaging
- **Native packages** (`.deb`, `.rpm`) per arch for clean install/upgrade/uninstall integration, **plus** a self-contained tarball for the curl-installer path and air-gapped installs.
- Packages are **signed**; repos expose a signed `manifest.json` (see [07](07-installer-architecture.md) §6).
- The React SPA is embedded into `hpd` via `embed.FS` (single binary serves UI + API) — no separate web root to drift; OLS reverse-proxies to it. (An external `/opt/heropanel/web` is also supported for custom branding builds.)

## 4. Self-update system

Requirements: **delta updates, rollback, signed updates, version channels (stable/beta/nightly)**.

```
Check → Download(delta) → Verify(sig) → Stage → Swap(atomic) → Migrate(db) → Health → (Commit | Rollback)
```

| Property | Design |
|----------|--------|
| **Channels** | `stable` / `beta` / `nightly`; operator picks; each has its own signed manifest stream |
| **Delta** | Binary diffs (bsdiff/zstd-dictionary) against the installed version → small downloads; full package fallback if delta missing |
| **Signed** | Every artifact cosign/minisign-signed; verified against a **pinned** public key before staging. Unsigned = refused |
| **Atomic swap** | New binaries staged in `/var/lib/heropanel/update/staged/`; symlink/`rename(2)` swap of `/opt/heropanel/bin` → new; old kept as `previous/` |
| **DB migrations** | Run forward migrations *after* binary swap, inside the update job; migrations are backward-compatible for one version (expand/contract) so rollback is safe |
| **Health gate** | Post-swap `/readyz` + broker handshake + smoke checks. Fail → auto **rollback** to `previous/` and revert migrations' contract step |
| **Rollback** | One command (`hpctl update rollback`) or automatic on health failure; restores `previous/` binaries + prior config snapshot |
| **Module updates** | Independent per module (see [06](06-plugin-architecture.md) §4) — a module can update without updating core, within the compatible API-version range |
| **Coordinated update** | `hpctl update apply` can update core then re-handshake modules, verifying semver compatibility before committing |

Updates are **jobs** with live progress in the UI; the operator sees changelog, size, and can schedule to a maintenance window.

## 5. Backup of the panel itself (distinct from customer backups)
- A built-in job snapshots: panel DB (mysqldump/mariabackup), `/etc/heropanel` (config + secrets, encrypted), module data, and the audit log — to a chosen backup target.
- Enables disaster recovery of the control plane, and safe restore onto a fresh box (`hp-installer restore --from <snapshot>`).

## 6. Observability in deployment
- `/metrics` (Prometheus) on loopback; the **monitor module** scrapes and stores rollups, or an external Prometheus can scrape via an allowlisted bind.
- Journald integration (structured logs) + file logs under `/var/log/heropanel`; logrotate/journald retention configured by the installer.
- Health endpoints (`/healthz` liveness, `/readyz` readiness) for external supervisors/uptime checks.

## 7. Single-node → multi-node path (additive, not a rewrite)

The architecture is deliberately shaped so fleets are an **extension**:

```
 TODAY (single node)                     FUTURE (fleet)
 ┌───────────────────────┐               ┌──────────── CONTROL PLANE ────────────┐
 │ hpd + broker + modules│               │ hpd (control) + MariaDB + Redis        │
 │ + MariaDB + Redis     │               └───────┬───────────────┬───────────────┘
 │ (all on one box)      │                       │ agent gRPC/mTLS│
 └───────────────────────┘                 ┌─────▼─────┐   ┌──────▼─────┐
                                           │ hp-agent  │   │ hp-agent   │  (per managed node:
                                           │ +broker   │   │ +broker    │   runs broker + modules,
                                           │ +modules  │   │ +modules   │   registers to control plane)
                                           └───────────┘   └────────────┘
```

Why it's additive:
- **The broker + module gRPC contract is already a network-agnostic RPC.** Today it's a local Unix socket; a fleet swaps the transport to **mTLS over TCP** to a remote `hp-agent` that hosts the broker + modules on each node. No service-layer change.
- **State is already centralized** in MariaDB/Redis; the control plane just points agents at it via authenticated RPC rather than local sockets.
- **Realtime is already Redis Pub/Sub**, so events from many nodes fan out through the same hub.
- HA of the control plane itself then becomes a standard problem: MariaDB Galera/replication + Redis Sentinel/Cluster + ≥2 stateless `hpd` behind a load balancer (sessions/queue already external). This is explicitly **out of scope for v1** but unobstructed by v1's design.

## 8. Environments & release flow
- **CI**: build (multi-arch via Go cross-compile + `docker buildx`), unit + integration (testcontainers) + installer matrix + e2e (Playwright on disposable VMs) → sign artifacts → publish to channel repo.
- **Channels** gate exposure: nightly (auto from main), beta (release candidates), stable (promoted after soak).
- **Reproducible builds** (pinned toolchain, `-trimpath`, `-buildvcs`) so signatures are meaningful.

---
Next: [09 — UX Flow](09-ux-flow.md)
