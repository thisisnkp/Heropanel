# ADR-0005 — Redis for Cache, Queue, and Realtime Bus

**Status:** Accepted · **Date:** 2026-07-10

## Context
We need (a) a cache, (b) a durable job/worker queue with retries and dead-lettering, and (c) a realtime fan-out bus for WebSocket events. We want to minimize distinct dependencies for low-RAM, single-node installs.

## Decision
Use **Redis** for all three: **cache** (key/value + rate-limit buckets), **queue** (Redis **Streams** with consumer groups), and **realtime bus** (Redis **Pub/Sub**).

## Rationale
- One well-understood, low-footprint dependency covers three needs.
- **Streams > Lists** for the queue: consumer groups give at-least-once delivery, per-consumer acks, pending-entry inspection, replay, and dead-lettering — the semantics a job system needs, with durability across restarts.
- **Pub/Sub** fans out realtime events across processes today and across nodes tomorrow (aligns with [ADR-0003](0003-single-node-first.md)).
- Jobs are also mirrored to a `jobs` table for queryable history and UI; Redis is the transport, MariaDB is the record of truth for history.

## Note — caching is two-tier
Redis is the **L2** (shared/distributed) cache. In front of it sits an **L1 in-process "normal" cache** inside each `hpd` (sharded LRU + TTL) for nanosecond hot reads with no network hop. Coherence is maintained by publishing invalidations on a Redis Pub/Sub `cache:invalidate` channel so every process drops stale L1 entries. Both tiers sit behind one `cache.Cache` interface (`TieredCache{L1,L2}`); minimal/SQLite installs can run **L1-only** without Redis. See [01 §3.4](../01-architecture.md).

## Consequences
- Redis is a required service for the queue/bus (installer provisions it). Caching can degrade to L1-only if Redis is absent (minimal mode); the queue/realtime bus still assume Redis in v1.
- We rely on Redis persistence (AOF) configured by the installer so queued jobs survive restarts; critical job state is additionally in MariaDB.
- Not using Redis Cluster in v1 (single-node); multi-node HA would add Sentinel/Cluster (future major).
