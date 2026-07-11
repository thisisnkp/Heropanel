# ADR-0007 — Broker Transport: length-prefixed JSON over a Unix socket

**Status:** Accepted · **Date:** 2026-07-11

## Context
`hpd` (unprivileged) must call `hp-broker` (root) to perform privileged
operations. [ADR-0002](0002-module-isolation-hybrid.md) and doc 06 originally
named gRPC for both the broker and modules. When implementing the broker
transport, two properties dominate:

1. **The broker is root.** Every line of code it runs — including its wire
   parser — is high-value attack surface. Minimizing it is a security goal
   (doc 05: "tiny, audited broker").
2. The broker's contract is tiny: essentially one `Invoke(capability, input) ->
   result` call. It does not (yet) need streaming.

## Decision
Use a **minimal length-prefixed JSON framing over a Unix domain socket** for the
`hpd` ↔ `hp-broker` transport:

- 4-byte big-endian length prefix + JSON payload, capped at 1 MiB per frame.
- Per-connection handshake: client sends a shared secret **token**; the server
  verifies it (constant-time) **and** the peer's OS credentials.
- Request/response frames after the handshake.

Authentication is **defense in depth**: (a) socket file mode `0660 root:heropanel`
so only the `heropanel` group can connect, (b) `SO_PEERCRED` check that the peer
uid matches the configured caller (Linux), and (c) the token.

gRPC remains the intended transport for **modules** (`hp-mod-*`), which have
richer, streaming interfaces and run unprivileged.

## Rationale
- **Smaller root attack surface** — the broker depends only on the standard
  library (`net`, `encoding/json`), not on `google.golang.org/grpc` +
  `protobuf`. Less code, fewer CVEs, easier to audit.
- **Zero new dependencies**, consistent with the project's lean-deps stance.
- **Testable now** — the connection handler is driven over `net.Pipe` in tests,
  cross-platform; `SO_PEERCRED` is Linux-only and build-tagged (verified in CI).
- **Sufficient** — the broker is not a hot path; a dial-per-call request/response
  is fine. Connection pooling and streaming (for long ops that emit progress)
  can be added later without changing the security model.

## Consequences
- A small hand-written wire package (`pkg/brokerwire`) is maintained instead of
  generated stubs. It is deliberately tiny.
- If the broker later needs server-streaming progress, we either extend the
  framing (multiple response frames per request id) or revisit gRPC for the
  broker specifically. The capability/audit core is transport-agnostic, so this
  is an additive change.
- The transport is network-agnostic (operates on a `net.Conn`), so the future
  multi-node agent split ([ADR-0003](0003-single-node-first.md)) can swap the
  Unix socket for mTLS/TCP without touching the capability layer.
