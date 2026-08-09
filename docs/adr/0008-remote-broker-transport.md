# ADR-0008 — Remote broker transport: the same framing over mutual TLS

**Status:** Accepted · **Date:** 2026-08-09 ·
**Extends:** [ADR-0007](0007-broker-transport.md)

## Context
[ADR-0003](0003-single-node-first.md) chose single-node first, and
[ADR-0007](0007-broker-transport.md) gave the broker a hand-written
length-prefixed JSON framing over a Unix socket, explicitly to keep the root
component's parser surface small. Both documents, and doc 06, named **gRPC** as
the eventual transport when the panel grew past one host.

Phase 10 is where that bill comes due. Multi-node means npd on one host driving
`np-broker` on another, and a Unix socket does not cross a network.

Two things about the socket do not survive the move:

1. **`SO_PEERCRED` has no network equivalent.** On one host the kernel reports
   the peer's uid and no process can lie about its own. Across hosts nothing
   attests anything.
2. **The socket's file mode was an access control.** `0660 root:nexpanel` meant
   only one group could even attempt a connection. A TCP port has no such gate.

Adopting gRPC as originally planned would also directly contradict the reason
ADR-0007 exists: it puts an HTTP/2 stack and a protobuf parser inside the
process running as root, which is the surface that ADR deliberately refused.

## Decision
Keep the framing. Replace only the proof of identity.

- The remote endpoint is the **same `brokerwire` framing over `crypto/tls`**,
  TLS 1.3 minimum, with `tls.RequireAndVerifyClientCert`.
- The caller's identity is the **common name of its verified client
  certificate**, read from `VerifiedChains` — never from `PeerCertificates`,
  which is only what the peer sent.
- A verified certificate is **necessary but not sufficient**: the node's name
  must also appear in an operator-configured allowlist. The CA answers "is this
  a real node"; the allowlist answers "may that node drive root here".
- The shared token is **retained** on top of the certificate, so a leaked CA key
  is not by itself enough to take the installation.
- Local and remote are **separate entry points** (`Serve` / `ServeRemote`), not
  one method that inspects the connection.
- `pkg/nodepki` mints the CA and leaves from the standard library. A deployment
  with its own PKI hands the transport PEM files and never touches it.

**gRPC is not adopted for the broker.** It remains the intended transport for
satellite modules (`np-mod-*`), which are unprivileged — exactly the split
ADR-0007 already drew.

## Rationale
- **The root parser surface stays where ADR-0007 put it.** `crypto/tls` is
  standard library and the framing is unchanged, so multi-node adds no new
  dependency to the component running as root. Zero new modules in `go.mod`.
- **Separate entry points make a whole failure mode unrepresentable.** Before
  this change, handing the existing `Serve` a TCP listener would have *silently*
  dropped the uid check — `peerCredSupported` goes false for a non-Unix
  connection and `authorizePeer` returns nil. The endpoint would have been
  protected by the shared token alone, and nothing would have said so. Choosing
  the trust model at the listener rather than per connection removes that.
- **The allowlist is a second, independent decision.** Certificate issuance
  tends to become routine; permission to drive root on a specific host should
  not be a side effect of it.
- **Streaming is not blocked.** The terminal, container-exec and log-follow
  capabilities already take over a connection and stream frames; that mechanism
  is transport-agnostic and works identically over TLS.

## Consequences
- The panel gains a small PKI to operate: a CA key to protect, leaves to renew.
  `pkg/nodepki` has **no revocation** — short TTLs and the allowlist are the
  only ways to retire a node, and that is documented rather than hidden.
- The audit chain now records an attested node alongside the caller-supplied
  correlation id (`capability.Actor.Node`), so a reader can tell which machine
  drove a privileged action without trusting the request's own claims.
- gRPC's ecosystem — reflection, deadlines-on-the-wire, generated clients in
  other languages — is not available for the broker. Nothing consumes the broker
  except npd, which is built from this repository, so nothing needs them today.
  Should a third-party agent ever need to speak this protocol, `pkg/brokerwire`
  is small enough to reimplement, and revisiting this ADR is the alternative.
- **The two-node claim is validated on loopback, not on two machines.** See
  [docs/27](../27-multi-node.md) for exactly what that does and does not prove.
