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

## Consequences
- Redis is a required service (installer provisions it). For extreme-minimal installs, an in-process fallback queue could be offered later, but v1 assumes Redis.
- We rely on Redis persistence (AOF) configured by the installer so queued jobs survive restarts; critical job state is additionally in MariaDB.
- Not using Redis Cluster in v1 (single-node); multi-node HA would add Sentinel/Cluster (future major).
