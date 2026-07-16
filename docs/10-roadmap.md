# 10 — Development Roadmap

Build order optimized so that **each phase produces something runnable and demoable**, the security boundary exists from day one, and modules layer on without rework. We implement **one module at a time**, each to a strict Definition of Done.

## Definition of Done (applies to every module)
- [ ] Domain interfaces + service layer + repository implemented (clean architecture respected)
- [ ] Broker capabilities (if any) added to the allowlist with validation + config-test + rollback
- [ ] REST endpoints + OpenAPI annotations; async ops return jobs with WS progress
- [ ] Frontend feature slice (list/detail/create flows, realtime, empty/error states)
- [ ] Unit tests (services, validation, broker capabilities) + integration tests (real MariaDB/Redis) + e2e for the primary flow
- [ ] RBAC scopes + audit coverage for every mutation
- [ ] Docs: module README + API reference + user-facing help
- [ ] Passes the module contract tests (satellite modules) and systemd hardening review

---

## Phase 0 — Foundations (the skeleton that everything hangs on)
**Goal:** a running, secure, empty panel you can log into.
- Repo/monorepo scaffold, `go.work`, Make/Task, lint, CI (build + test matrix, multi-arch cross-compile).
- `pkg/proto` base contracts; `pkg/plugin` SDK skeleton; `pkg/arch` detection.
- **`hp-broker`** with the capability framework (exec-arg-array, peer-cred + token auth, audit chain) and the *first* capabilities (system user, service restart, file-op-in-site-root).
- **`hpd`**: config loader, Chi edge + middleware chain, MariaDB + migrations (identity/RBAC/audit/settings/jobs tables), Redis wiring, job dispatcher + worker pool, realtime hub, module registry skeleton.
- **Auth**: login, sessions+JWT, Argon2id, RBAC enforcement, audit log (hash-chained), API keys.
- **Frontend shell**: app skeleton, auth flow, layout, command palette, notifications, job/progress drawer, theme system.
- **`hp-installer`** MVP: preflight/detect, install core+broker+MariaDB+Redis+OLS(panel vhost), journal + rollback; `install.sh` bootstrap.
- **Exit criteria:** `curl | bash` installs on Ubuntu + Rocky (amd64 + arm64); browser login works; a trivial async "echo" job streams progress end-to-end; rollback verified.

## Phase 1 — Sites Core (the reason the panel exists)
**Goal:** create and serve a real PHP + static site with full isolation.
- **Sites module (in-core)**: create/list/detail/suspend/clone; per-site Linux user/group; site directories; cgroup slice + `site_limits`; OLS vhost generation (validated, tested, reloaded via broker); logs.
- **PHP module (in-core)**: multi-version support, PHP selector, dedicated FPM pool per site, php.ini editor, extension manager, FPM sizing, OPcache/JIT, Composer auto-install.
- **Static + Proxy** site types.
- Frontend: Create-Site wizard, per-site workspace (Overview, Domains, PHP/Runtime, Logs, Advanced).
- **Exit criteria:** create a WordPress-ready PHP site and a static site, each fully isolated (separate user, pool, tmp, logs), reachable over HTTP, PHP version switchable per site.

## Phase 2 — Domains, SSL, DNS ✅ **DONE**
- **SSL (in-core)**: Let's Encrypt (HTTP-01 + DNS-01/wildcard), ZeroSSL, custom upload, auto-renewal scheduler, per-domain status.
- **Domains**: aliases, subdomains, redirects, force-HTTPS.
- **DNS module (satellite)**: authoritative zones (PowerDNS/BIND backend), record CRUD, import/export, DNSSEC.
- **Exit criteria:** point a domain, auto-issue + auto-renew a cert (incl. wildcard via DNS-01), manage its zone in-panel.

**Status:** Domains ([internal/domain](../internal/domain)) — aliases/subdomains map onto
the vhost, redirect domains 301 to an absolute target, force-HTTPS is opt-in
(enabling it before a cert exists would take the site offline). DNS
([13-dns.md](13-dns.md)) — authoritative zones + record CRUD on a real BIND9
backend. SSL — HTTP-01, self-signed, custom upload, **DNS-01 incl. wildcard**
(publishing `_acme-challenge` TXT into a managed zone), and a **renewal sweeper**
([internal/ssl/renew.go](../internal/ssl/renew.go)) that repeats whichever flow
issued each cert.

**Verified live** (Docker, in CI): `run-dns.sh` — `dig` returns authoritative
answers for API-created records; `run-domains.sh` — alias serves, redirect 301s,
force-HTTPS toggles.

**Deferred:** DNS as a true *satellite* module (needs the [06](06-plugin-architecture.md)
registry/gRPC — DNS is in-core for now); DNSSEC; zone import/export; ZeroSSL;
**live ACME verification against a staging CA (Pebble)** — the ACME code paths are
unit-tested against a fake issuer but have never been exercised against a real CA.

## Phase 3 — Databases & Git deployments ✅ **DONE**
_Databases, Git deploys (webhook + rollback + auto-restart), and App runtimes
(proxy sites, systemd-supervised, OLS reverse-proxy) are implemented and verified
live in CI — see [11-git-deployments.md](11-git-deployments.md) and
[12-app-runtimes.md](12-app-runtimes.md). Composer auto-install and php.ini/
extension management remain open from Phase 1's PHP bullet._
- **Databases (in-core)**: MariaDB create DB/users/grants, import/export, size; phpMyAdmin/Adminer SSO handoff.
- **Git (in-core)**: sources (GitHub/GitLab/Bitbucket; PAT/deploy key/OAuth), webhook deploys, auto pull/build, deploy history + rollback, SSH/deploy key management.
- **App runtimes (in-core)**: Node/Python/Go site types (systemd-supervised in the site slice), build/start/env/health, process controls.
- **Exit criteria:** deploy a Laravel app (DB + composer) and a Next.js/FastAPI app via Git with auto-deploy + rollback.

## Phase 4 — Files, Editor, Terminal
- **File Manager (in-core, baremetal-only)**: browse/upload/download/zip/extract/permissions/ownership/search/drag-drop/image preview/diff, gitignore-aware; hard-blocked for git/docker sites.
- **Monaco editor** integration (tabs, find/replace, multi-cursor, themes).
- **Web terminal (xterm.js)**: per-user isolation via the site's Linux user, audit-logged sessions.
- **Exit criteria:** edit files, extract an archive, and open an audited terminal scoped to a single site user.

## Phase 5 — Docker & One-Click Apps
- **Docker module (satellite)**: containers/compose/images/volumes/networks, logs, exec/shell, stats, restart policies.
- **Apps module**: curated one-click templates (n8n, OpenWebUI, Supabase, Appwrite, Directus, PocketBase, Ghost, Uptime Kuma, Redis, Postgres, Mongo, RabbitMQ, MinIO, Grafana, Prometheus, …) with RAM feasibility checks + secure secret handling.
- **Exit criteria:** one-click deploy Ghost + Uptime Kuma, view live logs/stats, restart, and tear down cleanly.

## Phase 6 — Monitoring & Backups
- **Monitor module (satellite)**: realtime node/site/container/service/DB metrics (subscription-gated sampling), rollups + history, alerts (email/webhook/telegram), systemd service health.
- **Backup module (satellite)**: full + incremental, compression (zstd), encryption, remote targets (S3/R2/B2/GDrive/OneDrive/Dropbox/SFTP), scheduling, **restore wizard**; plus panel self-backup.
- **Scheduler (in-core)**: cron jobs as real system crontab/timer entries with logs and overlap policy.
- **Exit criteria:** live dashboards with no idle polling; scheduled encrypted incremental backup to S3 + successful restore into a new site.

## Phase 7 — Email
- **Mail module (satellite)**: Postfix + Dovecot, domains/accounts/aliases/forwarders, DKIM/SPF/DMARC generation + DNS wiring, quotas, mail-queue view.
- **Exit criteria:** provision a mail domain with passing DKIM/SPF/DMARC and send/receive.

## Phase 8 — Security suite
- **Security module (satellite)**: firewall (nftables/ufw abstraction, CSF-compatible) with rollback-timer applies; Fail2Ban + optional CrowdSec; ModSecurity + OWASP CRS per site; ClamAV/maldet/rkhunter/lynis scans + quarantine; FIM; auto security updates; SSH hardening; geo/IP allow-block.
- **Panel auth hardening**: WebAuthn/passkeys GA, session management UI, IP allowlist for the panel, brute-force + rate-limit tuning.
- **Exit criteria:** malware scan quarantines a test EICAR file; a firewall change auto-reverts if not confirmed; WebAuthn login works.

## Phase 9 — Multi-user, API, Plugin marketplace, Polish
- **Multi-user/RBAC GA**: admin/reseller/developer/client + custom roles, granular permissions, reseller tenant scoping, impersonation (audited).
- **Public REST API GA** + OpenAPI docs site + API-key management + webhooks.
- **Module marketplace** (signed, third-party) built on the existing manifest/gRPC contract.
- **UX polish**: accessibility pass, i18n, performance budgets enforced, docs/help center.
- **Exit criteria:** a reseller manages an isolated tenant; a third-party signed module installs from the catalog; API + docs are complete.

## Phase 10 — Self-update, HA path, hardening & GA
- **Self-update** GA: channels (stable/beta/nightly), delta + signed updates, atomic swap, health-gated auto-rollback; independent module updates.
- **Multi-node readiness**: validate the agent-transport swap (broker/module gRPC over mTLS) on a two-node prototype; document the HA topology (Galera + Redis Sentinel + LB) — implementation optional/next-major.
- **Enterprise hardening**: full audit of systemd profiles, AppArmor/SELinux profiles, threat-model review, penetration test, load/perf tuning to hit the RAM/startup budgets.
- **Exit criteria:** in-place upgrade stable→stable with auto-rollback proven; all budgets met; security review passed → **1.0 GA**.

---

## Cross-cutting workstreams (continuous, every phase)
- **Testing**: unit + integration (testcontainers) + e2e (Playwright) + installer matrix; coverage gates.
- **Security**: threat-model deltas per module; broker capability review is mandatory for any new privileged op.
- **Performance**: track idle RAM (`hpd`+broker < 80 MB) and cold-start (< 1.5 s) as CI budgets.
- **Docs**: every module documented on merge; ADRs for every significant decision.
- **Multi-arch**: every release built + smoke-tested on amd64 + arm64 (and 386 where feasible).

## Suggested sequencing note
Phases are ordered by dependency and demo value, not rigid time. Phases 0–1 are the critical path; after Phase 1 the panel is genuinely useful and later modules are largely parallelizable by capability team, since each is an independent module behind the registry.

---
Back to [index](README.md).
