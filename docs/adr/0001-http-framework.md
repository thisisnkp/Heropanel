# ADR-0001 — HTTP Framework: Chi + net/http

**Status:** Accepted · **Date:** 2026-07-10

## Context
The core control plane needs an HTTP router. Options considered: Chi, Fiber (fasthttp), Gin. The panel is a control plane (low/moderate QPS), not a high-throughput edge. It must coexist cleanly with WebSocket, gRPC, HTTP/2, standard middleware, and be highly testable and maintainable for years.

## Decision
Use **Chi on top of the standard `net/http`**.

## Rationale
- **Stdlib compatibility.** `net/http` handlers, `httptest`, `context`, gRPC-gateway, and idiomatic WebSocket libraries (coder/websocket) all work without shims.
- **Coexistence.** gRPC (broker/modules) and WebSocket (realtime) integrate without a parallel abstraction layer, unlike fasthttp/Fiber which is intentionally not `net/http`-compatible.
- **Testability & maintainability.** The whole Go ecosystem targets `net/http`; onboarding and long-term maintenance are easier.
- **Performance is a non-issue here.** Fiber's throughput advantage is irrelevant at control-panel QPS; correctness, security middleware, and clarity dominate.

## Consequences
- Marginally lower synthetic RPS than Fiber — acceptable and unnoticeable for this workload.
- Full access to standard middleware and streaming; simpler WS/gRPC coexistence.
- Gin was a viable alternative (also `net/http`), but Chi's composable middleware and lighter surface fit the layered architecture better.
