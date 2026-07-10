# ADR-0004 — Primary Datastore: MariaDB (SQLite fallback)

**Status:** Accepted · **Date:** 2026-07-10

## Context
The control plane needs a relational store for identity, RBAC, sites, DNS, SSL, jobs, audit, metrics rollups, etc. The panel also *manages* MariaDB/PostgreSQL for hosted sites — but that is separate from the panel's own state store. Low-RAM installs must remain possible.

## Decision
Use **MariaDB 10.11+** as the primary control-plane datastore, with an **embedded SQLite** mode for minimal/low-RAM installations. PostgreSQL support for the control plane is a possible future option but not v1.

## Rationale
- MariaDB is already a first-class managed service on every target OS and is what the panel provisions for sites — one fewer distinct dependency to operate.
- InnoDB gives the transactional guarantees the service layer needs; JSON columns cover flexible sub-configs.
- SQLite mode lets a 1 GB VPS run the panel with the same logical schema (type-affinity differences handled in the repo layer), aligning with the low-RAM goal.

## Consequences
- The repository layer targets a **portable SQL subset**; MariaDB-specific features (partitioning on `metric_samples`/`audit_log`) are used behind capability checks and degrade gracefully on SQLite.
- Secrets are encrypted at the application layer (envelope encryption), so datastore choice doesn't change the security model.
