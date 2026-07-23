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
| 17 | [File Manager](17-file-manager.md) | Baremetal file browser + CodeMirror editor, run-as-site-user, path confinement, chunked I/O |
| 18 | [Web Terminal](18-web-terminal.md) | xterm.js + broker-hosted PTY as the site user, stream upgrade, audited sessions |
| 19 | [Docker](19-docker.md) | Containers, volumes, networks, exec, compose, and the one-click **Apps** catalog — via broker capabilities (never the `docker` group), ownership labels, flag-injection defence |

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

_**Phase 4 is complete**, and complete against the feature list rather than only
the exit criteria. The **File Manager** (in-core, baremetal-only) landed first:
twelve path-confined broker capabilities — browse, read/write, mkdir, remove,
rename, **copy**, chmod, extract, **compress**, **ownership repair**, and
recursive **search** — all run as the site's Linux user, bar `file.chown`, which
must be root to change an owner at all and is constrained so it can only ever
assign the site's own account. On top: the `internal/files` service with the
baremetal gate, chunked streaming, and a conflict policy that never silently
overwrites; `file.read`/`file.write` RBAC with force-audited downloads; and a
**Files** site tab with a right-click menu, **copy/cut/paste and duplicate**,
folder download (the server builds the zip, streams it, and deletes it — nothing
is left in the tree), drag-and-drop upload with a cancellable byte-accurate
progress bar (over `XMLHttpRequest`, since `fetch` has no upload-progress event
at all), sortable columns, a hidden-files
toggle, multi-select with bulk actions, recursive search, **nested**
gitignore-aware listing with git's precedence, and image preview — plus a
**CodeMirror 6** editor, chosen over Monaco for bundle size and strict-CSP
compatibility, code-split to load only on open, with multi-file tabs, a
dependency-free **diff view**, and working shortcuts (`Ctrl/⌘-S` bound inside the
editor so it never hits the browser's save dialog). See
[17 — File Manager](17-file-manager.md)._

_The **Web Terminal** then closed the phase: a real PTY hosted by the root broker
and run as the site's Linux user, bridged to xterm.js over a WebSocket. The
broker connection *upgrades* to a bidirectional stream rather than growing a
second protocol, so the peer-credential check, token handshake, policy gate, and
audit chain all still apply; it carries its own `terminal.use` permission, and a
disconnect kills the whole process group. Its UI handles what a terminal actually
needs: `Ctrl/⌘-Shift-C`/`-V` copy-paste (plain `Ctrl-C` is left alone — it is
still SIGINT), scrollback search, a persisted font size, and fullscreen.
**Sessions are recorded** as asciicast v2 and replayed in-panel through the same
xterm.js the live terminal uses — output *and* keystrokes, kept 30 days, behind
their own read and delete permissions, with downloads force-audited. They get a
**top-level Recordings page** across every site, sitting beside the audit log
rather than inside the terminal: the list first lived only in the Terminal tab,
which is gated on `terminal.use`, so an auditor holding `terminal.recordings.read`
and no shell access could not reach one — the permission was right in the API and
wrong in the navigation, and only a browser test could see that. Passwords
are never stored: input at a password prompt becomes a single `[redacted]`
marker. The rule that makes that work is not "redact while echo is off" — a shell
runs with echo off nearly all the time because readline echoes for itself — but
"echo off **while still canonical**", which is what `read -s`, `sudo` and getpass
actually do. See [18 — Web Terminal](18-web-terminal.md)._

_Both are proven live in CI, not mocked: `run-files.sh` (run-as-user ownership,
binary round-trip, `../../etc/passwd` clamping, copy refusing to clobber and
clamping a traversing destination, a folder download that leaves no temp archive,
baremetal gate) and `run-terminal.sh` (shell runs as the site user and never root,
I/O round-trip, a traversing `cwd` never reaching `/etc`, unauthenticated upgrade
refused, no orphaned processes, session on the hash chain). The PTY layer itself
is unit-tested against a real pseudo-terminal, and the frontend's pure logic — the
gitignore matcher, the differ, the path helpers — now has a `vitest` suite that
runs in CI before the bundle is built._

_Two gaps surfaced **after** Phase 4 was first called done, and both were the
quiet kind: uploads had no progress indicator at all, and the File Manager and
terminal routes were **missing from `docs/openapi.json`** — the spec is generated
by walking a test router that never mounted those services, so about 1500 lines
of the published API were absent while the drift test happily passed. Both are
fixed, and a new **permission drift test** now drives the real router to prove
that the permission each route is documented with is the permission it actually
enforces — a mapping nothing had ever verified._

_**Phase 5 — Docker & One-Click Apps — is complete.** The **Docker** module is reached through **broker capabilities** rather than by putting anything in the `docker` group — a deliberate departure from [06](06-plugin-architecture.md)'s own manifest example, which shows `groups: [docker]`: membership of that group is root by another name and would have made a compromise of the network-facing process a compromise of the host. Two boundaries carry it: an **ownership label** the broker verifies by live inspect before any mutation (without it a stop button is a remote off-switch for every container on the host), and an allowlist that makes a value docker would parse as a **flag** — a container named `--privileged` — unrepresentable, which an argv array alone does not. Unmanaged containers are listed read-only, because hiding them would make the panel lie about the machine it administers. **Creating** a container is where the hardening lives: the caller sends typed fields and the broker builds the argv, so host bind mounts are unrepresentable (named volumes only), ports publish to `127.0.0.1` alone (docker's firewall rules run ahead of the host's), `no-new-privileges` is added, and environment travels by stdin env-file, never argv. A **shell** inside a container reuses the web terminal's PTY and stream upgrade outright, bounded by which container rather than which user. **Compose stacks** are framed honestly as an escape hatch — arbitrary YAML the broker labels and scopes but does not pretend to harden. On top sits the **Apps** catalog: one-click templates deployed as labelled compose stacks, with secrets **generated** (never defaulted, shown once, kept out of the audit log) and a **memory-feasibility** check that refuses a deploy the host cannot run before the OOM killer would. Proven against a **real dockerd** (`run-docker.sh`, 63 assertions in CI): lifecycle verbs refused on a foreign container that is still running afterwards; a created container with no host mounts, loopback-only; host-path volumes refused with nothing created; an env secret never reaching the broker log; a shell in a foreign container refused; and the full app pipeline deployed, loopback-published, and torn down clean. Two bugs no unit test could catch surfaced there — a 30s stop grace against hpd's 30s HTTP write timeout that returned failure for a succeeded action, and a `compose ps` row dropped because a `Publishers` array was modelled as a string. See [19 — Docker](19-docker.md)._

_Still **not** signed off: the genuinely later-phase items each phase lists below
(e.g. DNS as a true satellite module, cgroup kernel-enforcement of `site_limits`,
PostgreSQL/Mongo engines, OAuth app authorization, cryptographic artifact
**signing** on top of checksums, the public `install.sh` bootstrap host), plus the
Phase 4 items deliberately carried forward: terminal session recording, nested
`.gitignore` precedence, server-side archive streaming, and archived multi-file
upload. The honest per-bullet accounting lives in [10 — Roadmap](10-roadmap.md)._
