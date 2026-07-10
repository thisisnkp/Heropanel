# ADR-0006 — SQL Access: sqlx + explicit SQL (not a heavy ORM)

**Status:** Accepted · **Date:** 2026-07-10

## Context
The repository layer needs a data-access approach. Options: a full ORM (GORM/ent), a query builder, or `sqlx` with hand-written SQL. Hot paths (job dispatch, metrics, audit, list endpoints) must be predictable and fast; the code must stay clean-architecture-friendly (repositories implement domain interfaces).

## Decision
Use **`sqlx` with explicit, hand-written SQL** behind repository interfaces. Migrations via **golang-migrate**. A lightweight query builder may be used for a few highly-dynamic filter endpoints; GORM/ent is not adopted for hot paths.

## Rationale
- **Predictable performance and SQL** — no hidden N+1s, no surprise queries, easy to `EXPLAIN` and index deliberately (the schema doc defines covering indexes intentionally).
- **Clean architecture fit** — repositories are thin, explicit, and fully mockable via domain interfaces; no ORM types leaking into the domain.
- **Portability** — writing to a portable SQL subset keeps the SQLite fallback ([ADR-0004](0004-datastore.md)) viable.

## Consequences
- Slightly more boilerplate than an ORM for trivial CRUD; mitigated with small generic helpers/generics.
- Full control over transactions (`WithTx`), batching, and index usage — the right trade for an enterprise, performance-sensitive control plane.
