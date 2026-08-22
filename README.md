<div align="center">

# ⬢ NexPanel

**The fastest modern self-hosted hosting control panel.**
Go core · React UI · modular to the core · low-RAM · multi-arch.

</div>

---

> **Status: Phase 0 in progress — plan approved, foundations building.**
> The architecture & design package is in [`docs/`](docs/README.md). Implemented so far (all tested): shared primitives, the privilege-broker security spine, the `npd` core (Chi HTTP edge + graceful lifecycle), a two-tier cache (in-process L1 + Redis L2 with an invalidation bus), the data layer (sqlx + hand-rolled migrations, MariaDB/SQLite), **session-based auth + RBAC** (Argon2id, lockout, first-run bootstrap), and a **React + TypeScript + Vite + Tailwind UI** embedded into the single `npd` binary. Build with `make dist`; run with `./bin/npd`.

## What this is
A self-hosted control panel designed to compete with hPanel, CyberPanel, aaPanel, Plesk, cPanel, RunCloud, CloudPanel, Coolify, Railway, Vercel, and Dokploy — built primarily in **Go** for a fraction of the RAM of PHP-based panels, with an original, task-oriented React UI.

## Core ideas
- **Modular to the core** — every capability installs, enables, disables, restarts, and updates independently.
- **Least privilege** — the network-facing daemon never runs as root; all privileged actions cross a tiny, audited, allowlisted **broker**.
- **Low RAM is a feature** — core + broker target **< 80 MB RSS idle**; modules load only when enabled.
- **Multi-arch** — `arm64`, `amd64`, `x86` are first-class; nothing hardcoded.
- **Realtime, not polling** — WebSocket + Redis Pub/Sub; long operations are async jobs with live progress.

## Architecture at a glance
```
Browser / CLI / API
      │ HTTPS / WSS
      ▼
   npd  (control plane, non-root)  ── gRPC ──►  np-broker (root, tiny, audited) ──► OS
      │  Chi · services · repos · jobs · realtime · module registry
      ├── MariaDB (state)   Redis (cache/queue/pubsub)
      └── gRPC ──►  np-mod-* modules (docker, monitor, mail, dns, backup, …)
```

## Foundational decisions (locked)
| Area | Choice |
|------|--------|
| Module model | Hybrid: non-root core + root broker + on-demand gRPC process modules |
| HTTP framework | Chi + net/http |
| Topology | Single-node first, multi-node-ready |
| Datastore | MariaDB (SQLite fallback) |
| Cache/queue/bus | Redis (Streams + Pub/Sub) |
| Web server | OpenLiteSpeed (Nginx/Caddy/Apache pluggable later) |
| Frontend | React + TypeScript + Vite + Tailwind + shadcn/ui |

## Documentation
Start at **[docs/README.md](docs/README.md)**. The planning package:

1. [Software Architecture](docs/01-architecture.md)
2. [Folder Structure](docs/02-folder-structure.md)
3. [Database Schema](docs/03-database-schema.md)
4. [API Design](docs/04-api-design.md)
5. [Security Architecture](docs/05-security-architecture.md)
6. [Plugin Architecture](docs/06-plugin-architecture.md)
7. [Installer Architecture](docs/07-installer-architecture.md)
8. [Deployment Architecture](docs/08-deployment-architecture.md)
9. [UX Flow](docs/09-ux-flow.md)
10. [Development Roadmap](docs/10-roadmap.md)

Decision records: [docs/adr/](docs/adr/).

## Running it in development
Two processes, two terminals. The Vite dev server serves the UI on **:5173** and
proxies `/api`, `/healthz` and `/readyz` to npd on **:8443** — so the UI alone
shows nothing but "the panel is unreachable".

```bash
npm --prefix web run dev:api   # npd on :8443  (SQLite at ./np.db, loopback only)
npm --prefix web run dev       # the UI on :5173  ← open this one
```

`make dev-api` runs the same npd. The SQLite datastore is the point of the
wrapper: npd's default driver is MariaDB with an empty DSN, which boots a panel
with **no datastore**, reports `configured: false`, and refuses every login —
a failure that reads like a broken build rather than a missing setting. Override
any of `NP_SERVER_PORT`, `NP_DATABASE_DRIVER`, `NP_DATABASE_DSN` to point
elsewhere.

The browser-driven suite (`npm --prefix web run test:e2e`) does not use either:
it builds the bundle, embeds it, and runs a real npd on :18780.

## Installation (planned)
```bash
curl -fsSL https://get.nexpanel.io/install.sh | bash
```
Auto-detects CPU/arch/OS/RAM/virtualization/firewall/existing services, resolves conflicts, backs up existing configs, and rolls back automatically on failure. See [installer architecture](docs/07-installer-architecture.md).

## License
TBD.
