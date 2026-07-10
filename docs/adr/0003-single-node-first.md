# ADR-0003 — Topology: Single-node first, multi-node-ready

**Status:** Accepted · **Date:** 2026-07-10

## Context
Self-hosted panels overwhelmingly run on a single server, and low RAM is a headline goal. But the expertise brief includes HA and fleet management, so the design must not paint us into a single-node corner.

## Decision
Design and ship for a **single node first**, while keeping every boundary network-agnostic so a **control-plane + remote-agent** split is *additive*, not a rewrite.

## Rationale
- 95% of the target market is single-server; shipping fleet complexity first would delay the first useful release and inflate RAM.
- The pieces that make fleets possible are already in the single-node design for other reasons:
  - **broker + module comms are gRPC** (today over Unix sockets) → later over **mTLS TCP** to a per-node `hp-agent`, with no service-layer change.
  - **State is centralized** in MariaDB/Redis.
  - **Realtime is Redis Pub/Sub**, which already fans out across processes/nodes.

## Consequences
- v1 is lean and fast. Fleet/HA (multiple `hpd` behind an LB, Galera, Redis Sentinel, remote agents) is a later major, unobstructed by v1.
- We commit to keeping the broker/module RPC **transport-agnostic** and avoiding local-only assumptions (e.g. no reliance on shared local filesystem for cross-component state).
