# HeroPanel — Architecture & Design Documentation

> The fastest modern self-hosted hosting control panel. Go core, React UI, modular by design, low-RAM, multi-arch.

This directory contains the **complete architecture and planning package** produced before any implementation. Nothing in the codebase should be built until the relevant document here has been reviewed and approved.

## Foundational Decisions (locked)

| # | Decision | Choice | ADR |
|---|----------|--------|-----|
| 1 | Module isolation model | **Hybrid**: non-root core (`hpd`) + tiny root broker (`hp-broker`) + on-demand gRPC process modules (`hp-mod-*`) | [ADR-0002](adr/0002-module-isolation-hybrid.md) |
| 2 | HTTP framework | **Chi + net/http** (stdlib-compatible) | [ADR-0001](adr/0001-http-framework.md) |
| 3 | Deployment topology | **Single-node first, multi-node-ready** | [ADR-0003](adr/0003-single-node-first.md) |
| 4 | Primary datastore | **MariaDB** (SQLite embedded fallback for minimal installs) | [ADR-0004](adr/0004-datastore.md) |
| 5 | Cache / queue / realtime bus | **Redis** (Streams for queue, Pub/Sub for realtime) | [ADR-0005](adr/0005-redis.md) |
| 6 | Primary web server | **OpenLiteSpeed** (Nginx/Caddy/Apache pluggable later) | — |

## Document Index

| # | Document | Purpose |
|---|----------|---------|
| 01 | [Software Architecture](01-architecture.md) | Process topology, layers, control/data plane, realtime, concurrency model |
| 02 | [Folder Structure](02-folder-structure.md) | Repository (monorepo) layout **and** runtime on-disk (FHS) layout |
| 03 | [Database Schema](03-database-schema.md) | Full relational schema, migrations strategy, per-domain tables |
| 04 | [API Design](04-api-design.md) | REST contract, envelopes, async jobs, WebSocket channels, internal gRPC |
| 05 | [Security Architecture](05-security-architecture.md) | Privilege separation, broker capability model, site isolation, authn/authz |
| 06 | [Plugin Architecture](06-plugin-architecture.md) | Module manifest, lifecycle, gRPC contract, install/enable/restart |
| 07 | [Installer Architecture](07-installer-architecture.md) | One-command installer, detection, rollback, arch/OS matrix |
| 08 | [Deployment Architecture](08-deployment-architecture.md) | systemd topology, upgrades, delta/signed updates, HA path |
| 09 | [UX Flow](09-ux-flow.md) | Information architecture, key flows, command palette, realtime UX |
| 10 | [Development Roadmap](10-roadmap.md) | Milestones, module sequencing, definition of done |

### Module designs

Written as each module lands; each carries its own scope, security model,
deferred list, and definition of done.

| # | Document | Purpose |
|---|----------|---------|
| 11 | [Git Deployments](11-git-deployments.md) | Release layout, atomic swap, rollback, webhooks, private-repo credentials, Composer |
| 12 | [App Runtimes](12-app-runtimes.md) | Proxy sites, hardened systemd units, reverse-proxy vhost, health checks |
| 13 | [DNS](13-dns.md) | Authoritative BIND9 zones, record CRUD, zone rendering + rollback |
| 14 | [Databases](14-databases.md) | MariaDB CRUD/grants, size, export/import, Adminer hand-off |
| 15 | [Audit Log](15-audit.md) | Hash-chained accountability, the edge auditor, tamper detection |
| 16 | [PHP](16-php.md) | Version selector, FPM sizing, php.ini editor, OPcache/JIT, extensions |

## Product Principles

1. **Modular to the core.** Every capability is a module that can be installed, enabled, disabled, restarted, and updated independently without touching others.
2. **Least privilege always.** The network-facing process never runs as root. All privileged actions cross a narrow, audited broker boundary with an allowlisted command set and zero shell interpolation.
3. **Low RAM is a feature.** Idle footprint target: **core + broker < 80 MB RSS**; modules load only when enabled. Compare: PHP-based panels routinely idle at 300–800 MB.
4. **Multi-arch, never hardcoded.** `arm64`, `amd64`, `x86` (386) are first-class. Every downloaded dependency is arch/OS-resolved at runtime.
5. **Realtime, not polling.** State changes propagate over WebSocket via Redis Pub/Sub. Long operations are async jobs with live progress.
6. **Original UI, hPanel-grade UX.** Inspired by hPanel's *clarity and flow* only. Zero copied markup, CSS, assets, icons, or branding.
7. **Enterprise quality.** Clean architecture, DI, repository pattern, service layer, strong test coverage, documented modules, no shortcuts.

## Naming / Glossary

| Term | Meaning |
|------|---------|
| `hpd` | HeroPanel Daemon — the core control-plane process (API, orchestration, scheduler). Runs as unprivileged `heropanel` user. |
| `hp-broker` | Privileged root helper ("system executor"). Tiny, audited, capability-scoped. The *only* component that runs as root. |
| `hp-mod-<name>` | A module process (e.g. `hp-mod-docker`). Supervised by systemd, speaks gRPC to `hpd` over a Unix socket. |
| `hpctl` | Local admin CLI (talks to `hpd` over its Unix socket; can bootstrap/repair). |
| `Site` | A hosted application/website with its own Linux user, directory, runtime, logs, SSL. |
| `Module` | An independently installable capability unit (see [06](06-plugin-architecture.md)). |
| `Broker capability` | A single named, allowlisted privileged operation the broker will perform (see [05](05-security-architecture.md)). |

---
_Status: **Phases 0–3 — backends done, closing out.** Foundations (`pkg/*`
primitives, the `hp-broker` security spine, the `hpd` core, two-tier cache, data
layer, session auth + RBAC, hash-chained audit, async jobs + WebSocket, embedded
React/Vite/Tailwind UI); Sites + PHP; Domains, SSL (HTTP-01, DNS-01/wildcard,
renewal) and DNS; Databases, Git deployments, and App runtimes. Each module's
primary flow is verified live against real OpenLiteSpeed / MariaDB / BIND9 / sshd
in CI, not mocks._

_The cross-cutting Definition-of-Done gaps that were open after the backend pass
are now **closed**: the **frontend** covers every Phase 1–3 area — a 7-tab site
workspace (Overview, Domains, PHP, Runtime, Git, Logs, Advanced) plus Databases,
DNS, SSL, Audit, and Modules screens, over the shell's command palette, toasts,
and global job drawer (`run-ui.sh` proves the built SPA is embedded and served
and that every page has a live endpoint). **`hp-installer` now performs a real
install** — execute + journal + resume + rollback, verified on fresh Ubuntu (apt)
and Rocky (dnf) images (`run-installer.sh`), closing Phase 0's `curl | bash`
exit criterion (bar the public bootstrap host + artifact signing). **OpenAPI 3.1**
is served at `/api/v1/openapi.json`, generated by walking the live route tree with
a drift test that fails on any undocumented route._

_A second pass then closed the security/verification gaps those phases had left
open: real ACME issuance against Pebble (`run-acme.sh`), the token/HTTPS clone
against a live TLS git server (`run-git-token.sh`), SSH host-key pinning and
GitHub webhook-signature verification, an interactive `/api/docs` viewer, installer
artifact checksum verification, and arm64 proven under qemu (`run-arch-smoke.sh`)._

_Still **not** signed off: the genuinely later-phase items each phase lists below
(e.g. DNS as a true satellite module, cgroup kernel-enforcement of `site_limits`,
PostgreSQL/Mongo engines, OAuth app authorization, cryptographic artifact
**signing** on top of checksums, the public `install.sh` bootstrap host). The
honest per-bullet accounting lives in [10 — Roadmap](10-roadmap.md)._
