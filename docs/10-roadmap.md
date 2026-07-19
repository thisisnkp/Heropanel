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

**Status:** The skeleton is complete. `pkg/proto` + `pkg/plugin` SDK **skeleton**
(transport-agnostic types + a `Handler` that stamps the API version and enforces
the capability allowlist; gRPC deferred to Phase 9/10, [06](06-plugin-architecture.md)),
`pkg/arch` detection, and the module **registry** ([internal/registry](../internal/registry))
into which in-core features register their capabilities. `hp-broker` with the
capability framework (exec-arg-array, peer-cred + token auth, hash-chained audit).
`hpd`: config loader, Chi edge + middleware chain, MariaDB + migrations, Redis
wiring, job dispatcher + worker pool, realtime hub. **Auth**: login, sessions,
MFA (TOTP), RBAC enforcement, hash-chained **audit** ([15](15-audit.md)) written
through structural middleware so coverage is not per-handler, and scoped **API
keys**. **Frontend shell** ([web](../web)): app skeleton, auth flow, layout,
**command palette** (⌘K), toast notifications, global **job/progress drawer**
(WS-backed), and a theme system — plus the full Phase 1–3 feature slices on top.
**`hp-installer`**: preflight/detect, plan, and the **execute + journal + resume
+ rollback** path ([07](07-installer-architecture.md)).

**Verified live** (Docker, in CI): `run-installer.sh` — on a fresh **`ubuntu:24.04`
(apt)** *and* **`rockylinux:9` (dnf)** image, `hp-installer --execute` installs
packages, creates the service user, renders config + hardened units, migrates a
SQLite store as that user, starts the broker + daemon, and its own verify step
confirms `/healthz` answers; `--resume` is a no-op once everything is done; and
`--rollback` reverses the install (user, files, units removed; panel stops).
`run-ui.sh` — the built SPA is embedded (`//go:embed all:dist`) and served by
`hpd`, deep links fall through to client-side routing, and every page the UI
renders has a live endpoint behind it. The OpenAPI document is served at
`/api/v1/openapi.json` and a drift test fails if any mounted route is
undocumented ([04](04-api-design.md)).

**arm64 is verified** too: `run-arch-smoke.sh` runs the cross-compiled binaries
under qemu on both Ubuntu and Rocky aarch64 images — arch detection reports
`linux/arm64`, the SQLite driver applies every migration, and the broker's
offline self-check passes. The binaries are **checksum-verified** at install time
against a `SHA256SUMS` manifest (a mismatch aborts and rolls back), and the spec
now has an interactive **`/api/docs`** viewer.

**Deferred from this phase:** the public `install.sh` bootstrap + `get.heropanel.io`
hosting, and the **cryptographic signature** (beyond checksum) of fetched
artifacts (§6 of [07](07-installer-architecture.md)); a full *emulated* arm64
apt/dnf install (the install logic is arch-agnostic and proven on amd64, so the
arm64 check smoke-tests the binaries rather than re-running the package manager
under emulation); and the "echo" demo job specifically — the async job + WS
progress path itself is exercised by the Phase 3 runtime suites (`run-nextjs.sh`).

## Phase 1 — Sites Core (the reason the panel exists)
**Goal:** create and serve a real PHP + static site with full isolation.
- **Sites module (in-core)**: create/list/detail/suspend/clone; per-site Linux user/group; site directories; cgroup slice + `site_limits`; OLS vhost generation (validated, tested, reloaded via broker); logs.
- **PHP module (in-core)**: multi-version support, PHP selector, dedicated FPM pool per site, php.ini editor, extension manager, FPM sizing, OPcache/JIT, Composer auto-install.
- **Static + Proxy** site types.
- Frontend: Create-Site wizard, per-site workspace (Overview, Domains, PHP/Runtime, Logs, Advanced).
- **Exit criteria:** create a WordPress-ready PHP site and a static site, each fully isolated (separate user, pool, tmp, logs), reachable over HTTP, PHP version switchable per site.

**This phase is now closed.** The bullets that were open after the first pass —
`php.ini` editor, extension manager, FPM sizing, OPcache/JIT ([16](16-php.md)),
and site **suspend / clone** plus per-site **logs** ([03](03-data-model.md), and
the lifecycle notes below) — all landed with live e2e. Composer auto-install and
the **cgroup slice + `site_limits`** landed alongside Phase 3
([12 §3.2](12-app-runtimes.md)).

**Verified live** (Docker, in CI): `run-php-tuning.sh` — an ini override,
memory_limit and an OPcache toggle observed in a served phpinfo; invalid FPM
sizing rejected by two independent guards while the site keeps serving; a
php.ini value that tries to break out of its directive refused; an extension
disabled then re-enabled for the whole version, each seen in a served phpinfo
after a real FPM restart. `run-lifecycle.sh` — a suspended site returns 503 and,
crucially, its domains do **not** fall through to another customer's site; resume
restores it; clone produces a separate site whose files belong to its own user;
logs (0750, owned by the site user) are read back through the broker.

**Deferred from this phase:** Rocky/Alma PHP layout (the extension manager is
Debian/Ubuntu-only for now); per-version OPcache shared-memory tuning (a
`PHP_INI_SYSTEM` surface distinct from the per-site pool).

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
force-HTTPS toggles. `run-acme.sh` — a **real HTTP-01 order against Pebble** (the
Let's Encrypt team's test ACME server): account registration, a challenge written
to the site webroot and served by OpenLiteSpeed, Pebble's validation authority
fetching it, finalize, and a downloaded leaf **signed by the Pebble CA** — the
full RFC 8555 flow against an actual ACME server, with Pebble's CA installed into
the trust store so the ACME HTTPS calls verify normally.

**Deferred:** DNS as a true *satellite* module (needs the [06](06-plugin-architecture.md)
registry/gRPC — DNS is in-core for now); DNSSEC; zone import/export; ZeroSSL.
(Live ACME against a real CA is **now verified** — see `run-acme.sh` above.)

## Phase 3 — Databases & Git deployments ✅ **DONE**
- **Databases (in-core)**: MariaDB create DB/users/grants, import/export, size; phpMyAdmin/Adminer SSO handoff.
- **Git (in-core)**: sources (GitHub/GitLab/Bitbucket; PAT/deploy key/OAuth), webhook deploys, auto pull/build, deploy history + rollback, SSH/deploy key management.
- **App runtimes (in-core)**: Node/Python/Go site types (systemd-supervised in the site slice), build/start/env/health, process controls.
- **Exit criteria:** deploy a Laravel app (DB + composer) and a Next.js/FastAPI app via Git with auto-deploy + rollback.

**Status:** Databases ([14-databases.md](14-databases.md)) — create/drop, users,
grants **and revoke**, size, gzipped export (streamed, then deleted), streamed
import, and an Adminer hand-off that **mints a throwaway account per session**
rather than storing database passwords. Git ([11-git-deployments.md](11-git-deployments.md))
— **private repos** via HTTPS token or a panel-generated ed25519 **deploy key**,
sealed at rest by [pkg/secrets](../pkg/secrets) (AES-256-GCM, AAD-bound per row);
webhook deploys, history, rollback, auto-restart, **release pruning**, and
**Composer auto-install**. App runtimes ([12-app-runtimes.md](12-app-runtimes.md))
— proxy sites, hardened systemd units, OLS reverse-proxy, process controls, and
**health checks** that make `running` mean the app actually answers.

App runtimes run **in the site slice**: every site gets
`heropanel-site-<user>.slice` at provisioning, the app unit is placed in it, and
`site_limits` (CPU/memory/tasks) are applied to it — closing Phase 1's "cgroup
slice + `site_limits`" bullet as well.

**Exit criteria met, verified live** (Docker, in CI):
- **Laravel app (DB + composer)** — `run-php-app.sh`: deploys from a **private**
  repo, Composer resolves and installs a real dependency with **no build
  command**, and the served page reads a row out of a panel-created MariaDB
  (`composer dependency: loaded` / `db row: hello from mariadb`). Export streams a
  verified gzip; import restores a deleted row.
- **FastAPI app via Git with auto-deploy + rollback** — `run-fastapi.sh`: a real
  `requirements.txt` resolved from PyPI into a real venv, served by real uvicorn;
  then `git push` → **webhook** → `"trigger":"webhook"` → app restarted → v2
  served; then **rollback** → app restarted → v1 served.
- **Next.js app** — `run-nextjs.sh`: real `npm install` + `next build` as the site
  user, served by `next start` (`x-powered-by: Next.js`), over the **async job
  path** (a real build cannot finish inside an HTTP request).
- **Go app** — `run-go.sh`: the build command compiles a real binary as the site
  user; the unit execs it out of the release.
- `run-git-private.sh` — a real `sshd`: the clone fails with
  `Permission denied (publickey)` until the generated deploy key is registered,
  then serves; the private key never appears in the API response, is sealed at
  rest, and leaves nothing on `/run`.
- `run-app.sh` — the Node app probes healthy, while a crash-on-boot runtime
  reports `error`, not green.
- `run-git-token.sh` — the **token/HTTPS** clone path against a real HTTPS git
  server (`git http-backend` behind TLS from a private CA installed into the
  trust store, HTTP Basic auth): the right token clones and serves; a wrong token
  is refused with `Authentication failed`; the token is sealed at rest.
- `run-git-private.sh` also now proves **SSH host-key pinning**: a wrong pinned
  key is refused under strict checking (`host key verification failed`) even
  though the deploy key is valid, and the correct pin clones.
- Webhook **signature** verification is exercised by `run-fastapi.sh`: a valid
  GitHub HMAC-SHA256 signature over the body is accepted, a tampered one is
  denied, and the proof kind is audited.

**Deferred:** OAuth app authorization (paste a token instead); rotating data keys
for sealed credentials; PostgreSQL/Mongo engines; php.ini / extension management
(open from Phase 1's PHP bullet); placing the **php-fpm pool** in the site slice
(a pool is a child of the fpm master, not its own unit); the remaining
`site_limits` columns docs/03 lists (io / disk+inode quota / bandwidth) — they
need per-device IO limits, filesystem quotas, and the Monitor module. (SSH
host-key pinning, the token/HTTPS clone, and provider webhook signatures are
**now done** — see the live suites above.)
**Not verified live:** the e2e container has no systemd, so the slice's
*properties* and the app unit's `Slice=` are asserted, but **kernel enforcement of
the limits is not** — that is real systemd's job.

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
