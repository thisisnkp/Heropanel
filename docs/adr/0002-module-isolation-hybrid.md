# ADR-0002 — Module Isolation: Hybrid (core + root broker + on-demand process modules)

**Status:** Accepted · **Date:** 2026-07-10

## Context
The product requires every feature to be "installable independently" and "restartable independently" with "no monolithic architecture", while also targeting **low RAM** and **strong security**. Three models were weighed:
1. Single binary with compiled-in modules (lowest RAM, but can't add/restart a module as a process, weak privilege boundary).
2. Every module a separate process (purest independence, highest baseline RAM/IPC overhead).
3. **Hybrid**: non-root core + a tiny always-separate root broker + heavy/optional modules as independent supervised processes; light essentials compiled into core behind interfaces/flags.

## Decision
Adopt the **Hybrid** model.

## Rationale
- **Security first.** Splitting a tiny root **broker** from the large network-facing **core** means an RCE in the API does not yield root — the single most important control-panel security property. This split is mandatory regardless of module packaging.
- **Independence where it matters.** Heavy/optional capabilities (Docker, Monitor, Mail, DNS, Backup, Security) run as separate `hp-mod-*` processes with their own systemd units and gRPC sockets — install/enable/disable/restart/update independently, exactly as required.
- **RAM discipline.** Trivial always-on essentials (sites, PHP, git, SSL, cron, files) stay in-core as packages behind interfaces — no per-feature process tax. Satellite modules load only when enabled.
- **Uniform contract.** Both tiers implement the same logical capability/lifecycle contract, so services and UI treat them identically; only transport differs.

## Consequences
- Slightly more moving parts than a single binary, but each is independently supervised and hardened.
- A clear, testable gRPC contract (`pkg/proto`) is required for broker + modules from day one.
- The same gRPC contract later enables multi-node (swap Unix socket → mTLS TCP) without redesign (see [ADR-0003](0003-single-node-first.md)).
- "In-core" modules are still independently *enable/disable*-able via feature flags, satisfying modularity without process overhead.
