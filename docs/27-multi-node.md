# 27 — Multi-node

Driving a broker on another host: what changes, what deliberately does not, and
the HA topology this is the groundwork for.

The whole change is one sentence:

> **The framing is the same; only the proof of who is calling is different.**

Back to [index](README.md). Related: [ADR-0008](adr/0008-remote-broker-transport.md),
[ADR-0007](adr/0007-broker-transport.md), [05 — Security architecture](05-security-architecture.md).

---

## 1. What a network takes away

On one host the broker's authentication is three overlapping things
([ADR-0007](adr/0007-broker-transport.md)): the socket's `0660 root:nexpanel`
file mode, an `SO_PEERCRED` check that the peer's uid is the panel's, and a
shared token.

Across hosts the first two simply do not exist. There is no file mode on a TCP
port, and no kernel to attest a peer that is on a different kernel.

That leaves the token — and a shared secret alone protecting a root-equivalent
endpoint is not a security model. So the remote endpoint gets a real one: a
**verified client certificate**, plus an allowlist, plus the token it already
had.

### The failure this design removes

Before this work, `Server.Serve` took any `net.Listener`. Handed a TCP listener
it would have kept working — and `authorizePeer` would have returned `nil` for
every connection, because `peerCredSupported` is false off a Unix socket. The uid
check would have vanished with no error, no log line and no configuration change.

That is why local and remote are **separate methods**, `Serve` and
`ServeRemote`, rather than one method that decides per connection. Every
per-connection guess about "is this local?" fails open when it guesses wrong.

## 2. Who proves what

```
npd (node A)                                    np-broker (node B)
     │                                                  │
     ├── tls.Dial, TLS 1.3, client cert ───────────────►│
     │                                                  ├─ RequireAndVerifyClientCert
     │                                                  ├─ handshake forced at accept
     │                                                  ├─ CN from VerifiedChains[0][0]
     │                                                  ├─ CN ∈ AllowedNodes?
     │◄── (rejected before any frame is parsed) ────────┤
     │                                                  │
     ├── brokerwire Hello{token} ──────────────────────►│  unchanged framing
     │◄── HelloAck ─────────────────────────────────────┤
     ├── Request{capability, input} ───────────────────►│
     │                                                  ├─ policy, capability, audit
     │◄── Response ─────────────────────────────────────┤     Actor.Node = CN
```

Three details carry most of the weight:

- **`VerifiedChains`, not `PeerCertificates`.** The latter is whatever the peer
  sent; reading a Subject out of it is how mutual TLS gets implemented wrongly.
  Only the chain the TLS stack itself built and checked against the configured
  CA is evidence.
- **The handshake is forced at accept time**, not left to the first read. An
  unauthorized peer is turned away before the framing parser — the code the root
  broker most wants to keep away from strangers — sees a single byte.
- **A valid certificate is not permission.** `AllowedNodes` is a separate
  operator decision. Issuing certificates becomes routine; authority over a
  specific host should not be a side effect of routine.

`ServeRemote` **refuses to start** with an empty allowlist. An unconfigured
remote endpoint is a startup error, never an open one.

## 3. Identity in the audit chain

`capability.Actor` gained one field, `Node`, and it is the only field in that
struct the broker establishes for itself. `UserID`, `IP` and `CorrelationID` are
all what the caller said about itself — fine for correlation, worthless as
evidence. `Node` comes from the certificate the transport verified and is never
read off the wire.

The audit chain records it first:

```
actor: "node:node-b 01J8…"     ← attested node, then the caller's correlation id
actor: "01J8…"                 ← local call: nothing to attest, nothing claimed
```

So reading the chain after an incident answers *which machine drove root here*
without having to trust that machine's own account of itself.

## 4. The PKI

`pkg/nodepki` is deliberately not a CA product: no CRL, no OCSP, no
intermediates, no renewal daemon. It mints one self-signed Ed25519 root per
installation and issues short-lived leaves from it. Ed25519 because the release
chain and the module chain already use it (`pkg/edkey`) — one key algorithm per
installation rather than three.

```
np-broker --init-pki --pki-dir /etc/nexpanel/pki --node-name node-b \
          --server-host 10.0.0.5

  ca.crt / ca.key        the root (key is 0600 and never leaves this host)
  server.crt / .key      the broker's listener identity
  node-b.crt / .key      the calling node's identity
```

Then:

```
np-broker --serve --listen-tls 0.0.0.0:9444 \
  --tls-cert server.crt --tls-key server.key \
  --tls-client-ca ca.crt --allow-node node-b
```

and on node B, npd's config:

```yaml
broker:
  token: <shared token>
  remote:
    addr: 10.0.0.5:9444
    server_name: np-broker     # must match a SAN on server.crt
    ca_file: /etc/nexpanel/pki/ca.crt
    cert_file: /etc/nexpanel/pki/node-b.crt
    key_file: /etc/nexpanel/pki/node-b.key
```

A deployment that already runs a PKI skips all of this: the listener consumes
PEM files and does not care who made them.

**There is no revocation.** Expiry and the allowlist are the only ways to retire
a node — removing a name from `--allow-node` and restarting is the fast path.
That is a real limitation, stated rather than hidden.

`remote` and `socket` are alternatives, not layers. A misconfigured remote broker
is a startup failure; it never falls back to the local socket, because a panel
that silently reverted to the wrong host would create users, write web server
configs and restart services on that host while looking perfectly healthy.

## 5. HA topology

The transport is the piece that had to exist first. The rest of the topology is
standard and remains **implementation-optional** for this phase:

```
                   ┌──────────── load balancer (L4/L7, TLS terminated or passed) ────────────┐
                   │                                                                          │
             ┌─────┴─────┐                    ┌───────────┐                    ┌───────────┐
             │  node A   │                    │  node B   │                    │  node C   │
             │ npd + web │                    │ npd + web │                    │ npd + web │
             └─────┬─────┘                    └─────┬─────┘                    └─────┬─────┘
                   │  np-broker (local socket)      │  np-broker                     │  np-broker
                   │        …or mTLS to a peer      │                                │
                   └────────────────┬───────────────┴────────────────┬───────────────┘
                                    │                                │
                        ┌───────────┴───────────┐        ┌───────────┴───────────┐
                        │ MariaDB Galera (3+)   │        │ Redis + Sentinel (3+) │
                        │ synchronous, no single│        │ quorum failover       │
                        │ writer to lose        │        │                       │
                        └───────────────────────┘        └───────────────────────┘
```

- **Galera, not primary/replica**, because the panel's writes are small and
  correctness-critical (a site row, a certificate, an audit entry) and an
  asynchronous replica can lose an acknowledged write on failover.
- **Sentinel for Redis**, which the panel uses for cache, the job queue and the
  cache-invalidation bus. Losing the queue's leader without failover stalls every
  asynchronous operation.
- **Odd node counts.** Both quorum systems need a majority; two nodes give a
  split brain with extra steps.
- **The broker stays local to the work.** A node's broker manages that node's
  sites. The remote transport exists for a control plane that must reach a node
  it is not running on — not to centralize all privileged work onto one machine,
  which would make that machine the single point of failure the topology exists
  to remove.

## 6. Verified

- **Two-node round trip** (`broker/transport/mtls_test.go`): a real `np-broker`
  on a TLS listener, npd's real client dialling it, a real capability invoked,
  and the audit chain asserted to carry `node:node-b` — then verified as a chain.
- **Refusals**, each asserting the broker recorded **zero** audit entries, i.e.
  the peer never reached the capability layer at all:
  a node **outside the allowlist**; a certificate from a **foreign CA** *carrying
  the allowlisted common name* (the name is a claim, the chain is the proof); a
  peer with **no client certificate**; an **expired** certificate; a **plaintext**
  peer; and `ServeRemote` **refusing to start** with an empty allowlist.
- The allowlist check was **mutation-tested** — forced to always match, the
  refusal test fails — so it is not passing vacuously.
- `pkg/nodepki`: leaves chain to their own CA and not to another; a leaf is not
  a CA and cannot sign; a server certificate without a SAN is refused at issue
  time rather than becoming an opaque handshake failure later.

### Honest limit

**This is validated on loopback, not on two machines.** Every layer that decides
*who gets in* is the shipping code path and is exercised for real. What loopback
cannot exercise is the network itself: latency and timeout interaction, MTU and
fragmentation, a firewall or NAT in the middle, TCP keepalive behaviour across a
partition, and two genuinely independent clocks (which matters, because
certificate validity is time-dependent and `nodepki` backdates `NotBefore` by
five minutes precisely to absorb that).

Proving those needs a two-VM e2e — the same infrastructure
[10 — Roadmap](10-roadmap.md) already records as missing for systemd-dependent
behaviour. Until it exists, the claim here is "the authentication and authorization
path is proven; the network path is not".

## 7. Deferred

- **gRPC for the broker** — refused, see [ADR-0008](adr/0008-remote-broker-transport.md).
  Still the intended transport for unprivileged satellite modules.
- **Certificate revocation and automatic renewal.** Short TTLs plus the
  allowlist are the current answer.
- **Node discovery and enrolment.** Nodes are configured, not discovered. An
  enrolment flow is a panel feature, not a transport one.
- **Actually running Galera/Sentinel.** The topology is documented and the
  transport supports it; standing it up is next-major.
