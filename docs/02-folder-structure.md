# 02 — Folder Structure

Two layouts matter: the **repository** (how we develop) and the **runtime filesystem** (how it lives on a server, FHS-compliant).

## 1. Repository Layout (monorepo)

A single Git monorepo. Go workspace (`go.work`) groups the core, broker, modules, and shared libs so they build together but ship as independent binaries.

```
heropanel/
├── go.work                       # ties core + broker + modules + pkg together
├── Makefile / Taskfile.yml       # build, test, lint, cross-compile, package
├── README.md
├── LICENSE
├── docs/                         # ← this planning package + living docs
│   ├── README.md  01-…10-*.md
│   └── adr/                      # Architecture Decision Records
│
├── cmd/                          # main() entrypoints → one binary each
│   ├── hpd/                      # core control-plane daemon
│   ├── hp-broker/                # privileged root helper
│   ├── hpctl/                    # local admin CLI
│   └── hp-installer/             # Go-based installer core (invoked by bootstrap .sh)
│
├── internal/                     # core (hpd) private code — NOT importable externally
│   ├── bootstrap/                # composition root, DI wiring, lifecycle
│   ├── config/                   # layered config loader
│   ├── http/                     # Chi router, middleware, handlers, DTOs
│   │   ├── middleware/
│   │   ├── handlers/             # one file per resource
│   │   └── openapi/              # generated spec + docs UI
│   ├── ws/                       # realtime hub, channels, auth
│   ├── grpc/                     # gRPC servers hpd exposes (module callbacks)
│   ├── domain/                   # entities, value objects, errors, INTERFACES
│   │   ├── site/  domain models + SiteRepository, etc.
│   │   ├── user/  auth/  dns/  ssl/  mail/  database/  docker/
│   │   ├── backup/  cron/  firewall/  monitor/  git/  module/
│   ├── service/                  # application/use-case layer (per context)
│   ├── repository/               # MariaDB (sqlx) implementations of domain repos
│   │   └── migrations/           # golang-migrate SQL files (NNNN_*.up/down.sql)
│   ├── broker/                   # BrokerGateway client (hpd → hp-broker)
│   ├── modclient/                # ModuleRegistry + per-module gRPC clients
│   ├── job/                      # dispatcher, worker pool, handlers, retry
│   ├── cache/                    # RedisCache(L2) adapter + Pub/Sub invalidation; composes pkg/cache
│   ├── events/                   # domain events + audit emitter
│   └── platform/                 # OS/arch/distro detection, path helpers
│
├── broker/                       # hp-broker private code
│   ├── capabilities/             # one file per allowlisted privileged op
│   ├── exec/                     # safe arg-array exec, no shell
│   ├── audit/                    # broker-side audit chaining
│   └── policy/                   # capability policy + validation
│
├── modules/                      # each module = its own Go module + binary
│   ├── docker/                   # hp-mod-docker
│   │   ├── cmd/                  # main()
│   │   ├── internal/            # module logic
│   │   ├── module.yaml           # manifest (see doc 06)
│   │   └── proto/                # module-specific gRPC (if any beyond core contract)
│   ├── monitor/                  # hp-mod-monitor
│   ├── mail/    dns/    backup/    security/    apps/  (one-click templates)
│   └── _template/                # scaffold for new modules
│
├── pkg/                          # PUBLIC shared libs (importable by modules + broker)
│   ├── proto/                    # ← shared gRPC contracts (broker + module protocol)
│   ├── plugin/                   # module SDK: handshake, health, lifecycle helpers
│   ├── arch/                     # CPU/arch/distro detection + binary resolver
│   ├── cache/                    # cache.Cache iface + LocalCache(L1) + TieredCache (stdlib-only) ✅
│   ├── crypto/                   # argon2, AEAD, signing helpers
│   ├── logx/  errx/  validate/  idgen/            # cross-cutting utilities
│
├── web/                          # frontend (React + TS + Vite)
│   ├── src/
│   │   ├── app/                  # routing, providers, layout shell
│   │   ├── features/             # feature-sliced: sites, dns, ssl, docker, files…
│   │   ├── components/ui/        # shadcn/ui primitives (generated, then owned)
│   │   ├── components/           # shared composite components
│   │   ├── lib/                  # api client, ws client, query keys, utils
│   │   ├── stores/               # Zustand stores
│   │   └── styles/               # Tailwind config, tokens, themes
│   ├── public/
│   ├── index.html   vite.config.ts   tailwind.config.ts
│   └── dist/                     # build output → embedded into hpd via embed.FS
│
├── deploy/
│   ├── systemd/                  # unit templates (see doc 08)
│   ├── installer/               # bootstrap install.sh + stages
│   ├── packaging/               # .deb / .rpm / tarball build specs
│   └── examples/                # sample configs
│
├── test/
│   ├── integration/             # spins real MariaDB/Redis via testcontainers
│   ├── e2e/                      # Playwright against a disposable VM/container
│   └── fixtures/
│
└── tools/                        # dev tooling, codegen scripts, lint config
```

### Repository conventions
- **`internal/` is core-private**; modules and broker must depend only on `pkg/`. This is enforced by Go's `internal` visibility + CI import-linter.
- **Feature-sliced frontend**: each `features/<name>/` owns its components, hooks, api calls, and types. No cross-feature imports except through `lib/` and `components/`.
- **Proto is the source of truth** for broker + module contracts; Go/TS stubs are generated, never hand-edited.
- **One handler file per resource**, one service per bounded context, one migration pair per change — small, reviewable units.

## 2. Runtime Filesystem Layout (on a managed server, FHS-compliant)

```
/opt/heropanel/                         # immutable install root (replaced on upgrade)
├── bin/
│   ├── hpd            hp-broker         hpctl
├── modules/
│   ├── docker/  {hp-mod-docker, module.yaml, assets/}
│   ├── monitor/ mail/ dns/ backup/ …    # only present if installed
├── web/                                 # built SPA (or embedded in hpd; served either way)
└── VERSION                              # channel + semver + signature

/etc/heropanel/                          # configuration (survives upgrades)
├── config.yaml                          # main config (0640 heropanel:heropanel)
├── secrets.env                          # DB pw, keys, broker token (0600 heropanel)
├── modules/<name>.yaml                  # per-module config
├── broker/policy.yaml                   # broker capability policy (0600 root)
└── ssl/                                 # panel's own TLS cert/key

/var/lib/heropanel/                      # mutable state
├── heropanel.db                         # SQLite (only in SQLite-mode installs)
├── modules/<name>/                      # per-module persistent data
├── jobs/                                # job artifacts, temp build outputs
├── acme/                                # ACME account + issued cert store
└── update/                              # staged delta updates, rollback snapshots

/var/log/heropanel/
├── hpd.log        broker-audit.log      access.log
├── jobs/<job-id>.log                    # streamed job logs
└── modules/<name>.log

/run/heropanel/                          # runtime sockets & pids (tmpfs, 0770)
├── hpd.sock                             # hpctl ↔ hpd
├── broker.sock                          # hpd ↔ broker (0600 root:heropanel)
└── modules/<name>.sock                  # hpd ↔ module

/var/backups/heropanel/                  # local backup staging before remote push

# ── Hosted sites (per-site isolation, see doc 05) ──────────────────────────
/srv/heropanel/sites/<site-id>/          # or /home/<sysuser>/ depending on policy
├── public/            # document root (owned by the site's dedicated Linux user)
├── logs/              # per-site access/error logs
├── tmp/  sessions/    # per-site PHP tmp + session dirs (not shared!)
├── ssl/               # per-site cert/key symlinks
└── .heropanel/        # metadata, deploy state (git sites), NOT web-served
```

### Ownership & permissions (summary — full model in [05](05-security-architecture.md))
| Path | Owner | Mode | Notes |
|------|-------|------|-------|
| `/opt/heropanel/bin/hp-broker` | `root:root` | `0750` | setuid **not** used; started as root by systemd |
| `/run/heropanel/broker.sock` | `root:heropanel` | `0660` | only `heropanel` group may talk to broker |
| `/etc/heropanel/secrets.env` | `heropanel:heropanel` | `0600` | never world-readable |
| `/etc/heropanel/broker/policy.yaml` | `root:root` | `0600` | broker reads its own policy |
| `/srv/heropanel/sites/<id>/` | `<siteuser>:<sitegroup>` | `0750` | isolated per site; panel not in group |

### Why this split
- **`/opt` (code)** is atomically swappable on upgrade and can be verified against a signature; nothing user-critical lives there.
- **`/etc` and `/var/lib` (config + state)** persist across upgrades and rollbacks.
- **`/run` (sockets)** is tmpfs, recreated each boot with correct modes — no stale-socket privilege issues.
- **Site data** lives outside the install root entirely, so panel upgrades can never touch customer files.

---
Next: [03 — Database Schema](03-database-schema.md)
