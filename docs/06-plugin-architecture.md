# 06 — Plugin / Module Architecture

The whole product is composed of **modules**. Some are compiled into the core (cheap, always-on essentials); heavy/optional ones are **independent processes** installed and supervised on demand. This document defines the contract that makes "install / enable / disable / restart / update independently" real.

## 1. Two module tiers

| Tier | Where it runs | Examples | Rationale |
|------|---------------|----------|-----------|
| **In-core packages** | Inside `hpd` (Go packages behind interfaces, feature-flagged) | sites, PHP selector, git, SSL, cron, files, users/RBAC, firewall-config | Tiny, always needed, cheap; still isolated behind service interfaces and independently *enable/disable*-able via feature flags |
| **Satellite modules** | Separate `hp-mod-*` process, gRPC over Unix socket, own systemd unit | docker, monitor, mail, dns (bind/powerdns), backup engine, security scanners, one-click app catalog | Heavy, optional, or needing a different privilege/user; must install/restart without touching core |

Both tiers implement the **same logical contract** (`Capability` + lifecycle), so the UI and services treat them uniformly: they ask the **Module Registry** "is capability X available?" and route calls the same way. Only the transport differs (in-proc call vs gRPC).

> This satisfies the requirement literally: *every* feature is a module and can be toggled; the *satellite* ones can additionally be installed as separate binaries and restarted as separate processes without affecting anything else.

## 2. Module Manifest (`module.yaml`)

Every satellite module ships a signed manifest:
```yaml
apiVersion: heropanel.io/v1
kind: Module
metadata:
  slug: docker
  name: Docker Manager
  version: 1.4.2
  description: Manage containers, compose stacks, images, volumes, one-click apps.
  category: infrastructure
  icon: box                      # icon *key*, resolved by UI icon set (no copied assets)
spec:
  binary: hp-mod-docker          # relative to module dir; per-arch resolved
  socket: /run/heropanel/modules/docker.sock
  runAs:                         # least privilege
    user: heropanel              # or a dedicated module user; docker needs docker group
    groups: [docker]
  capabilities:                  # what this module provides (queried by registry)
    - docker.container
    - docker.compose
    - docker.image
    - app.template.deploy
  requiresBroker:                # privileged ops it will request via hpd→broker
    - Docker.Compose
    - Module.SystemctlStartStop
  dependencies:                  # other modules/services it needs
    services: [docker]           # host service that must exist (installer can provision)
    modules: []
  arch: [amd64, arm64, 386]      # supported; installer picks the matching binary
  resources:                     # advisory limits → systemd unit
    memoryMax: 128M
    cpuQuota: 50%
  config:                        # schema for /etc/heropanel/modules/docker.yaml
    schema: config.schema.json
  health:
    endpoint: grpc:Health        # standard health RPC
    intervalSec: 15
  signing:
    checksum: sha256:…
    signature: …                 # verified against pinned key on install
```

## 3. The gRPC Module Contract (`pkg/proto/module.proto`)

Every satellite module implements a **base lifecycle service**; capability-specific RPCs live in per-module services.

```proto
service ModuleLifecycle {
  rpc Handshake (HandshakeRequest) returns (HandshakeResponse);  // version, capabilities, api compat
  rpc Health    (HealthRequest)    returns (HealthResponse);     // SERVING | NOT_SERVING | DEGRADED
  rpc Configure (ConfigureRequest) returns (ConfigureResponse);  // push validated config, hot-reload
  rpc Shutdown  (ShutdownRequest)  returns (ShutdownResponse);   // graceful drain
  rpc Events    (EventRequest)     returns (stream Event);       // module → hpd realtime (stats, logs)
}
```
Reverse channel: modules that need privileged actions or to persist state **call back into `hpd`** (which owns the DB and the broker) via a `CoreServices` gRPC exposed on `hpd.sock` — modules never touch the DB or broker directly. This keeps the security boundary intact: a compromised module still can't reach root except through the same audited broker capabilities, mediated by core policy.

```proto
service CoreServices {                 // hpd exposes; modules consume (authenticated by peer creds)
  rpc RequestBroker (BrokerCall) returns (BrokerResult);   // hpd validates against module's requiresBroker
  rpc Persist       (PersistOp)  returns (PersistResult);  // scoped state writes
  rpc Emit          (EmitEvent)  returns (EmitAck);        // publish to realtime hub / notifications
  rpc EnqueueJob    (JobSpec)    returns (JobRef);
}
```

### Handshake & compatibility
- On start, module dials nothing — it **listens** on its socket; `hpd` dials in and calls `Handshake`.
- `hpd` checks `apiVersion` compatibility (semver range). Incompatible → module marked `error`, not enabled, operator alerted.
- Magic-cookie handshake (HashiCorp go-plugin style) prevents accidental cross-wiring; peer-cred check authenticates the process.

## 4. Lifecycle & State Machine

```
        install            enable            (running, healthy)
none ─────────────► installed ─────────► enabled ──────────► RUNNING ⇄ DEGRADED
  ▲                    │  ▲                  │  ▲                 │
  │ uninstall          │  │ disable          │  │ restart         │ crash
  └────────────────────┘  └──────────────────┘  └─────────────────┘ (systemd Restart=on-failure,
                                                                      backoff; N crashes → ERROR + alert)
```

| Action | What happens |
|--------|--------------|
| **install** | Download arch-correct binary + manifest → verify signature/checksum → place in `/opt/heropanel/modules/<slug>/` → render systemd unit `heropanel-mod@<slug>.service` → write default config → row in `modules` (`installed`). No process yet. |
| **enable** | Broker `systemctl enable --now heropanel-mod@<slug>` → `hpd` dials socket, `Handshake` + `Configure` → state `enabled/running`. Registry now advertises its capabilities; UI unlocks the feature. |
| **disable** | Graceful `Shutdown` RPC → broker stops+disables unit → registry withdraws capabilities → UI greys out feature. Data retained. |
| **restart** | `Shutdown` → broker restart → re-`Handshake`. Independent of all other modules and of `hpd`. |
| **update** | Stage new signed binary → verify → `Shutdown` old → swap → start new → `Handshake` (version bump) → on failure, **rollback** to previous binary automatically. |
| **uninstall** | Disable → remove unit + files → optionally purge module data → delete `modules` row. |

Each of these is itself an **async job** with progress over WS. Failures are atomic (staged, then swapped) so a failed install/update never leaves a half-broken module.

## 5. Module SDK (`pkg/plugin`)
To make new modules trivial and consistent, an SDK provides:
- `plugin.Serve(cfg, impl)` — sets up the Unix listener, handshake, health, graceful shutdown, structured logging, and panic recovery. A module's `main()` is ~15 lines.
- Generated gRPC stubs + a `CoreClient` for calling back into `hpd`.
- A `_template/` module and `hpctl module scaffold <slug>` generator.
- Contract tests every module must pass (handshake, health, config round-trip, clean shutdown, capability advertisement).

## 6. Capability Discovery & Feature Gating
- `hpd` maintains a live `CapabilitySet`. Services check `registry.Has("docker.compose")` before offering an action; the UI receives the capability set at login and renders/greys features accordingly.
- Removing a module can't break core: dependent features degrade gracefully with a clear "Docker module not installed — Install" call to action.

## 7. Dependency & Conflict Resolution
- Manifests declare `dependencies.services` and `dependencies.modules`. Installing a module that needs the `docker` host service triggers an installer sub-flow (with consent) to provision it.
- Version constraints between modules and core API are semver-checked; conflicts block enable with a precise message.

## 8. Security posture of modules (recap; full detail in [05](05-security-architecture.md))
- Modules run **least-privilege** (dedicated user where possible; only the groups they truly need).
- Modules **cannot** touch the DB, broker, or filesystem outside their scope directly — all mediated by `hpd`'s `CoreServices`, which enforces the module's declared `requiresBroker` allowlist.
- Module packages are signature-verified before install; unsigned/unknown-key packages are refused unless the operator explicitly enables a dev/unsafe channel.
- Each module unit is systemd-hardened per §8 of the security doc.

## 9. The initial module catalog (build order in [10](10-roadmap.md))
| Module | Tier | Provides |
|--------|------|----------|
| sites / php / git / ssl / cron / files / users | in-core | the baseline panel |
| firewall (config) | in-core | rule modeling; apply via broker |
| **monitor** | satellite | node/site/container/service metrics, alerts (subscription-gated sampling) |
| **docker** | satellite | containers, compose, images, volumes, one-click app templates |
| **dns** | satellite | authoritative DNS (PowerDNS/BIND backend), DNSSEC |
| **mail** | satellite | Postfix/Dovecot, DKIM/SPF/DMARC, queue |
| **backup** | satellite | full/incremental, compression, encryption, remote targets, restore wizard |
| **security-scan** | satellite | ClamAV/maldet/rkhunter/lynis, quarantine, FIM |
| **apps** | satellite | curated one-click template catalog (extends docker) |

Third parties can ship modules following the manifest + gRPC contract; a future signed **module marketplace** is an additive layer, not a redesign.

---
Next: [07 — Installer Architecture](07-installer-architecture.md)
