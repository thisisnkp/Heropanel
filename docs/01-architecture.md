# 01 — Software Architecture

## 1. Architectural Style

HeroPanel is a **privilege-separated modular monolith with on-demand satellite processes**. It is *not* a microservice mesh (too much RAM/ops for a self-hosted panel) and *not* a single fat binary (cannot satisfy "install/restart modules independently" or the security boundary). It is the pragmatic middle: a small set of long-lived processes plus modules that spawn only when enabled.

```
                          ┌───────────────────────────────────────────────┐
                          │                    OPERATOR                    │
                          │   Browser (React SPA)   •   hpctl CLI   •  API  │
                          └───────────────┬───────────────────────────────┘
                                          │ HTTPS / WSS (:8443)   Unix sock
                                          ▼
        ╔═════════════════════════════════════════════════════════════════════╗
        ║                     hpd  —  CONTROL PLANE (non-root, user: heropanel) ║
        ║                                                                       ║
        ║   HTTP Edge (Chi)  ── Auth ── RBAC ── RateLimit ── Audit ── Validate  ║
        ║        │                                                              ║
        ║   Service Layer  (business logic, orchestration, transactions)        ║
        ║        │                                                              ║
        ║   ┌────┴─────┬───────────────┬────────────────┬───────────────────┐  ║
        ║   Repository  Job Dispatcher   Realtime Hub      Module Registry   │  ║
        ║   (MariaDB)   (Redis Streams)  (Redis Pub/Sub)   (gRPC clients)    │  ║
        ╚════╪══════════════╪══════════════╪══════════════════╪══════════════╝
             │              │              │                  │ gRPC / unix sockets
   ┌─────────▼───┐   ┌──────▼──────┐  ┌────▼────┐    ┌─────────▼───────────────────┐
   │  MariaDB    │   │   Redis     │  │ Workers │    │  MODULES (spawn when enabled) │
   │  (state)    │   │ queue+cache │  │ (pool)  │    │  hp-mod-docker  hp-mod-mail   │
   └─────────────┘   │ +pubsub     │  └─────────┘    │  hp-mod-dns     hp-mod-monitor│
                     └─────────────┘                 │  hp-mod-backup  ...           │
                                                     └───────────────────────────────┘
                                          │
                                          │ privileged ops only (gRPC / unix sock, root)
                                          ▼
        ╔═════════════════════════════════════════════════════════════════════╗
        ║              hp-broker  —  PRIVILEGE BROKER (root, tiny, audited)     ║
        ║   Capability allowlist • arg-array exec (no shell) • per-call audit   ║
        ╚═════════════════════════════════════════════════════════════════════╝
             │ useradd  systemctl  nft  certbot  file-ops  service configs …
             ▼
        ┌─────────────────────────────────────────────────────────────────────┐
        │  MANAGED SYSTEM:  OpenLiteSpeed • PHP-FPM pools • MariaDB • Redis •   │
        │  Postfix/Dovecot • Docker • nftables • systemd units • per-site users │
        └─────────────────────────────────────────────────────────────────────┘
```

### The four process classes

| Class | Process(es) | Runs as | Lifetime | Purpose |
|-------|-------------|---------|----------|---------|
| **Control plane** | `hpd` | `heropanel` (unpriv) | always | API, auth, orchestration, scheduler, realtime, module registry |
| **Privilege broker** | `hp-broker` | `root` | always | Executes the *only* allowlisted privileged operations |
| **Modules** | `hp-mod-*` | dedicated unpriv users (or root where unavoidable, e.g. docker) | on-demand | Heavy/optional capabilities isolated as separate supervised processes |
| **Ephemeral workers** | goroutine pool inside `hpd` + spawned jobs | `heropanel` | per-job | Async work items pulled from Redis Streams |

**Why `hpd` and `hp-broker` are split** is the central security invariant: a compromise of the network-facing code (largest attack surface) does **not** grant root. The attacker can only ask the broker to perform a fixed set of validated operations. See [05 — Security](05-security-architecture.md).

## 2. Clean Architecture Layers (inside `hpd`)

Dependencies point **inward only**. Outer layers depend on inner; inner never imports outer.

```
   ┌──────────────────────────────────────────────────────────────┐
   │ INTERFACE / DELIVERY   http (Chi handlers), ws, grpc, cli      │  <- imports app
   ├──────────────────────────────────────────────────────────────┤
   │ APPLICATION / SERVICE  use-cases, orchestration, tx boundaries │  <- imports domain
   ├──────────────────────────────────────────────────────────────┤
   │ DOMAIN                 entities, value objects, domain errors,  │  <- imports nothing
   │                        repository & gateway INTERFACES          │
   ├──────────────────────────────────────────────────────────────┤
   │ INFRASTRUCTURE         MariaDB repos, Redis, broker client,     │  <- implements domain
   │                        module gRPC clients, filesystem          │     interfaces
   └──────────────────────────────────────────────────────────────┘
```

- **Domain** defines interfaces like `SiteRepository`, `BrokerGateway`, `DNSProvider`. No SQL, no HTTP, no framework types.
- **Application/Service** implements use cases (`CreateSite`, `IssueCertificate`), owns transactions, emits domain events, and enqueues jobs. It depends only on domain interfaces — fully unit-testable with mocks.
- **Infrastructure** provides concrete implementations (sqlx/GORM repos, Redis client, broker gRPC client).
- **Delivery** translates transport ↔ service DTOs. Chi handlers are thin: decode → validate → call service → encode.

**Dependency Injection**: a composition root (`cmd/hpd/main.go` → `internal/bootstrap`) wires concrete infra into services. We use **explicit constructor injection** (no reflection-based container magic) with a small `Container` struct. Optional: `google/wire` for compile-time DI if wiring grows unwieldy — decided per ADR later.

## 3. Component Responsibilities

### 3.1 HTTP Edge (Chi middleware chain)
Order matters. Every request flows through:
```
RequestID → RealIP → Recover(panic) → SecurityHeaders → CORS →
RateLimit(per-IP + per-token) → AccessLog → Authn(session/JWT/APIkey) →
Authz(RBAC scope check) → BodyLimit → Validate → Handler → Audit(mutations)
```
- Serves the embedded React SPA (`embed.FS`) at `/` and the API at `/api/v1`.
- WebSocket upgrade at `/api/v1/ws` (post-auth).
- Health/readiness at `/healthz`, `/readyz` (no auth, localhost-scoped by default).

### 3.2 Service Layer
- One service per bounded context: `SiteService`, `DomainService`, `DatabaseService`, `SSLService`, `DNSService`, `MailService`, `DockerService`, `GitService`, `BackupService`, `CronService`, `FirewallService`, `MonitorService`, `UserService`, `AuthService`.
- Owns **transaction boundaries** (`repo.WithTx(ctx, func...)`).
- Never calls the OS directly — privileged effects go through `BrokerGateway`; module effects go through the `ModuleRegistry` gRPC client.
- Emits **domain events** onto the realtime bus and audit log.

### 3.3 Repository Layer
- Repository pattern over MariaDB via **sqlx** + hand-written SQL (predictable, fast, no ORM surprises) — GORM considered and rejected for hot paths; may be used only for trivial CRUD admin tables. (ADR-0006.)
- Connection pooling tuned (`SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`).
- All queries context-aware and cancellable.

### 3.4 Caching Layer (two-tier: in-process L1 + Redis L2)
Caching is deliberately **two-tier** so hot reads never touch the network and the RAM budget stays intact.

- **L1 — in-process "normal" cache.** A local in-memory cache living inside each `hpd` (and reusable by modules via the SDK). Fast — no network hop, nanosecond reads — TTL + size-bounded **sharded LRU** (sharded to avoid lock contention). Holds hot, small, read-heavy data: resolved RBAC permission sets, session/JWT validation results, the module capability set, `settings`, per-site config, DNS/SSL lookups, and the fast path of rate-limit counters.
- **L2 — Redis.** Shared/distributed cache across processes and (future) nodes; larger, survives an `hpd` restart, and is the coordination point for invalidation.
- **Read path.** L1 → miss → L2 → miss → load from source (DB / broker / module) → populate L2, then L1. Short-TTL **negative caching** ("not found") blunts lookup storms.
- **Coherence & invalidation.** Writes publish an invalidation message on a Redis Pub/Sub `cache:invalidate` channel; every process drops the affected L1 keys, so the in-process cache never serves stale data across the fleet. Each entry also carries a TTL as a backstop.
- **Stampede protection.** `singleflight` around cold loads so a hot key is computed once per process; jittered TTLs avoid synchronized expiry.
- **Safety.** Strict, configurable memory ceiling on L1 (counted in the RAM budget); per-namespace hit/miss metrics; the cache is always an optimization, never a source of truth — a cold cache degrades to correct-but-slower.

**Abstraction.** All access goes through one interface — `cache.Cache` (`Get / Set / Delete / GetOrLoad`) — so call sites don't know which tier served them. The two-tier behavior is composed behind it as `TieredCache{ L1 LocalCache, L2 RedisCache }`; an install can also run **L1-only** (SQLite/minimal mode without Redis) or **L2-only** by swapping the composition at the bootstrap. L1 is a dependency-light sharded LRU-with-TTL (e.g. `hashicorp/golang-lru/v2` or an internal impl) — we avoid heavyweight cache libraries to protect the RAM budget.

### 3.5 Job Dispatcher & Worker Pool
- **Redis Streams** as the durable queue (consumer groups → at-least-once, survives restart, ack/retry/dead-letter). Chosen over Redis Lists for consumer-group semantics and replay.
- Every long/mutating operation (create site, issue cert, run backup, deploy git, build docker image) is a **Job** with a persisted row + stream entry.
- Worker pool: bounded goroutines (`GOMAXPROCS`-aware, configurable), each pulling from the stream, executing an idempotent handler, emitting progress to the realtime hub.
- Job states: `queued → running → (succeeded | failed | cancelled)`, with `progress 0–100`, structured `steps[]`, and streamed log lines.
- Retries with exponential backoff + jitter; poisoned jobs → dead-letter stream + operator alert.

### 3.6 Realtime Hub
- Central WebSocket hub in `hpd`; subscribes to **Redis Pub/Sub** channels so events fan out even across future multi-node control planes.
- Channels are scoped and RBAC-filtered: a client only receives events for resources it may read (e.g. `site:42:*`, `job:*`, `metrics:node`).
- Message envelope: `{ channel, type, resource, data, ts, seq }`. Client reconciles via React Query cache invalidation.
- Backpressure: per-connection bounded send buffer; slow clients dropped and told to refetch (no unbounded memory growth).

### 3.7 Module Registry
- Discovers modules from `/opt/heropanel/modules/*/module.yaml`.
- Maintains each module's state (`installed`, `enabled`, `running`, `version`, `health`).
- Holds a **gRPC client** per running module (dial its Unix socket). Lazy-connects; reconnects with backoff.
- Requests systemd start/stop of module units **via the broker** (start/stop is a privileged op).
- Exposes a stable **capability query**: services ask "is `docker` capability available?" before offering Docker features.

### 3.8 Scheduler
- Internal cron engine (`robfig/cron`-style) for panel-owned periodic tasks: SSL renewal checks, backup schedules, metric rollups, health probes, update checks, log rotation triggers, malware scan windows.
- User-defined cron jobs are **not** run in-process — they are materialized as real system crontab/systemd-timer entries for the site's Linux user via the broker (so they survive panel downtime and run with correct privileges). The panel tracks + shows logs.

## 4. Concurrency & Performance Model

| Concern | Approach |
|---------|----------|
| Startup time | Lazy module load; DB migrations gated; target cold start **< 1.5 s** |
| Idle RAM | `hpd` + `hp-broker` **< 80 MB RSS** idle; modules add ~15–40 MB each *only when enabled* |
| CPU | No busy polling. Metrics via event/subscription; system stats sampled on an interval only while a client is subscribed |
| Blocking OS calls | Never on the request goroutine — dispatched as jobs or to modules |
| Connection reuse | Pooled MariaDB + Redis; persistent gRPC channels to modules & broker |
| Backpressure | Bounded worker pool, bounded WS buffers, Redis Stream length caps with trimming |
| Graceful shutdown | `context` cancellation → drain HTTP → finish/park in-flight jobs → close modules → flush audit |

## 5. Configuration Model
- Layered: **defaults (compiled) → `/etc/heropanel/config.yaml` → env vars (`HP_*`) → runtime settings table (DB)**. Later layers override earlier.
- Secrets (DB password, JWT signing key, broker token, encryption keys) live in `/etc/heropanel/secrets.env` (mode `0600`, owner `heropanel`) or an optional OS keyring / age-encrypted store — never in the world-readable config or the DB in plaintext.
- Hot-reloadable settings (feature flags, rate limits, branding) live in the DB `settings` table and push a `settings.changed` realtime event; structural settings (ports, DB DSN) require restart.

## 6. Observability
- **Structured logging** (`slog`, JSON) with request/job correlation IDs; per-component log levels.
- **Metrics**: internal Prometheus endpoint (`/metrics`, localhost/allowlisted) — process, HTTP, job, DB pool, module health.
- **Tracing** (optional, off by default): OpenTelemetry hooks around service + broker + module calls.
- **Audit log**: append-only, tamper-evident (hash-chained rows), covers every privileged/mutating action — separate from app logs (see [05](05-security-architecture.md)).

## 7. Error Handling Contract
- Domain errors are typed (`ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrForbidden`, `ErrUpstream`) and mapped once, centrally, to HTTP status + stable machine `code` (see [04](04-api-design.md)).
- Broker/module failures never leak raw stderr to the API; they are wrapped, logged with full detail internally, and surfaced as sanitized operator-facing messages + a support/correlation ID.

## 8. Technology Summary

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Language | **Go 1.23+** | Low RAM, static binaries, great concurrency, cross-compilation for all target arches |
| HTTP router | **Chi + net/http** | Stdlib-compatible; WS/HTTP2/gRPC coexist; testable ([ADR-0001](adr/0001-http-framework.md)) |
| Internal RPC | **gRPC** (Unix sockets) | Typed contracts to broker + modules; codegen; streaming for logs/progress |
| DB | **MariaDB** (+ SQLite fallback) | Ubiquitous on target OSes; SQLite for micro installs ([ADR-0004](adr/0004-datastore.md)) |
| DB access | **sqlx** + explicit SQL | Predictable performance, no ORM magic on hot paths |
| Cache | **Two-tier**: in-process L1 (sharded LRU+TTL) → Redis L2 | Hot reads avoid the network; Redis Pub/Sub invalidation keeps L1 coherent |
| Queue/Bus | **Redis** (Streams + Pub/Sub) | One dependency serves durable queue + realtime fanout ([ADR-0005](adr/0005-redis.md)) |
| Migrations | **golang-migrate** | Versioned, reversible, CI-checked |
| Frontend | **React + TS + Vite + Tailwind + shadcn/ui** | See [09 — UX](09-ux-flow.md) |
| Realtime | WebSocket (coder/websocket) | net/http-native, context-aware |
| Auth | Argon2id, sessions + JWT (short-lived), TOTP, WebAuthn | See [05](05-security-architecture.md) |

## 9. Non-Goals (explicitly out of scope for v1)
- Kubernetes orchestration of the panel itself (we manage Docker/Compose, not a k8s control plane).
- Multi-tenant billing/WHMCS-style provisioning (reseller RBAC is in; billing integrations are a later plugin).
- Windows server management (Linux only).

---
Next: [02 — Folder Structure](02-folder-structure.md)
