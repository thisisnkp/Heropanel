# 28 — Enterprise hardening

A review of what actually confines each NexPanel process, what was tightened,
what cannot be tightened and why, and the budgets that are now measured instead
of asserted.

Back to [index](README.md). Related: [05 — Security architecture](05-security-architecture.md),
[24 — Security suite](24-security.md), [27 — Multi-node](27-multi-node.md).

---

## 1. The systemd audit

The panel writes four kinds of unit from three packages. Before this pass each
had grown its own subset of sandboxing directives:

| Unit | Written by | Had | Gap |
|---|---|---|---|
| `npd.service` | `internal/installer` | `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `ReadWritePaths` | no capability bound, no kernel protections, no syscall filter, no address-family restriction |
| `np-broker.service` | `internal/installer` | *nothing* — `User=root`, `NoNewPrivileges=false` | the root component was entirely unconfined |
| `nexpanel-app-*.service` | `broker/capabilities` | `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `UMask`, `Slice` | no capability bound, no namespace restriction, no syscall filter |
| `nexpanel-cron-*.service` | `broker/capabilities` | same as app units | same |

The gap that mattered most is the second row. The one process running as root
had no `CapabilityBoundingSet` at all, which meant a compromise of the broker
inherited the full capability set — including rebooting the host, loading kernel
modules and moving the clock out from under the audit log.

A directive present in three units and missing from the fourth is not a
consistency problem. It is the one unit an attacker gets to use. The directives
now live in one place, `pkg/unitharden`, as three profiles.

## 2. The three profiles

**`Daemon`** — npd. Unprivileged Go, speaks HTTP, reaches a datastore and the
broker, reads four global files from `/proc`. It takes everything: an empty
capability bounding set, `NoNewPrivileges`, the full `Protect*` family,
`RestrictNamespaces`, `RestrictSUIDSGID`, `SystemCallFilter=@system-service`,
`MemoryDenyWriteExecute` and `ProtectProc=invisible`.

**`SiteWorkload`** — app units and cron units. The same, minus W^X. A cron job is
the same trust level as the app: the site user's own code, bounded to exactly
what that user can already do.

**`RootBroker`** — np-broker. Almost none of the filesystem sandboxing applies,
so what it takes instead is a **deny list** of capabilities, plus
`RestrictAddressFamilies`, `RestrictRealtime`, `LockPersonality` and
`SystemCallArchitectures=native`.

### Why a deny list for the broker

The broker execs `useradd`, `runuser`, `nft`, `systemctl`, `docker`, `tar` and
the host's package manager. Enumerating what root *needs* there is a guess, and
the first wrong guess is a panel that cannot provision a site — discovered on a
customer's host, not in CI.

Enumerating what it demonstrably never needs is a claim that can be checked by
reading the list. What it buys: a compromised broker cannot reboot the host, load
or unload kernel modules, move the clock, silence the kernel audit subsystem, do
raw port or memory I/O, or rewrite MAC policy.

```
CapabilityBoundingSet=~CAP_SYS_BOOT CAP_SYS_MODULE CAP_SYS_TIME CAP_SYS_RAWIO
                       CAP_WAKE_ALARM CAP_AUDIT_CONTROL CAP_AUDIT_READ
                       CAP_MAC_ADMIN CAP_MAC_OVERRIDE CAP_BLOCK_SUSPEND CAP_LEASE
```

## 3. What is deliberately absent

Four directives look like obvious wins and are each excluded on purpose. They
are the most likely thing for a future reader to "fix", so each is pinned by a
test that fails with the reason in it.

| Directive | Where it is absent | What it would break |
|---|---|---|
| `ProcSubset=pid` | `Daemon` | Hides `/proc/meminfo`, `/proc/stat`, `/proc/loadavg`, `/proc/uptime` — the exact four files npd reads to report host metrics. `ProtectProc=invisible` gives the process-tree hiding without this cost, and is used. |
| `MemoryDenyWriteExecute` | `SiteWorkload` | Every JIT. V8 is the obvious one, and an app unit is precisely where a customer's Node runtime runs. |
| `ProtectKernelModules` | `RootBroker` | `nft` autoloading `nf_tables`, which is how the firewall comes up on a freshly booted host. |
| `ProtectSystem` / `ProtectHome` / `PrivateTmp` / `NoNewPrivileges=true` | `RootBroker` | The broker's job: writing `/etc/postfix`, `/etc/dovecot`, `/usr/local/lsws`, and the home directories `useradd` creates. |

### The honest conclusion about the broker

**A root broker cannot be sandboxed into safety.** Its real containment is the
capability allowlist, the policy checks and the hash-chained audit log
([ADR-0007](adr/0007-broker-transport.md), [05](05-security-architecture.md)).
The directives above narrow the blast radius of a compromise; they do not contain
it. That is the argument for keeping the broker small and its capability list
short, and it is unchanged by this pass.

## 4. Threat-model review

What changed in this phase, and what it did to the trust boundaries.

| Boundary | Before | After |
|---|---|---|
| Browser → npd | Session cookie + CSRF, rate limits, RBAC | unchanged |
| npd → broker (same host) | Socket mode, `SO_PEERCRED`, shared token | unchanged |
| npd → broker (**across hosts**) | *did not exist* | TLS 1.3, verified client certificate, node allowlist, shared token — and the caller's identity is now recorded in the audit chain as attested rather than claimed ([27](27-multi-node.md)) |
| Site code → host | Site user, cgroup slice, `ProtectSystem=strict` | plus no capabilities, no new privileges, no namespaces, no setuid creation, `@system-service` syscall filter |
| Broker compromise → host | Full root | root minus the deny list above |
| Release artifacts → binaries | Signed, verified twice | unchanged ([26](26-self-update.md)) |

Two findings worth stating plainly:

1. **The remote endpoint would have failed open.** Handing the pre-existing
   `Serve` a TCP listener silently disabled the uid check, because
   `peerCredSupported` is false off a Unix socket. Nothing would have logged it.
   Fixed structurally by splitting `Serve` and `ServeRemote`, so the trust model
   is chosen at the listener rather than guessed per connection.
2. **The audit chain's actor was entirely caller-supplied.** It recorded
   `Actor.CorrelationID`, which npd sends. That is fine for correlating a
   privileged action to a request, and worthless as evidence about *who* acted.
   The attested node now leads the field where there is one.

## 5. Budgets, measured

`docs/10` has carried "idle RAM (`npd`+broker < 80 MB) and cold-start < 1.5 s as
CI budgets" as a cross-cutting workstream since Phase 0. Nothing measured either
one — the only budget actually enforced was the frontend bundle.

`deploy/docker/e2e/run-budget.sh` now measures both against real processes on
every CI run: process launch to `/readyz` for cold start, and `VmRSS` for both
daemons after the runtime settles. Exceeding either fails the build.

The measurement is of the processes themselves, not of them under systemd —
there is no systemd in the e2e container. That is where regressions come from
anyway; a directive-induced regression would need the VM harness below.

## 6. Proven, not just written

A `CapabilityBoundingSet` nobody enforces is a comment. Three layers now check
this work, and only the third is evidence:

1. **Profile invariants** (`pkg/unitharden`) — the four deliberate absences in
   §3 each fail a test naming what they would break.
2. **Wiring** (`internal/installer`, `broker/capabilities`) — every unit the
   panel writes is asserted to carry its profile, and to carry it inside
   `[Service]` where systemd will read it rather than after `[Install]` where it
   would be ignored.
3. **Enforcement** (`deploy/docker/e2e/run-systemd.sh`) — an image where PID 1
   is real systemd. The installer runs for real, both services are asserted
   active, `systemd-analyze verify` rejects any directive that does not parse,
   and then **`CapBnd` is read out of `/proc`**: npd is shown to hold no
   capabilities whatsoever, and the root broker to have genuinely lost every
   capability the deny list names.

The third layer also checks the deny list is not *too* broad — `CAP_CHOWN`,
`CAP_SETUID`, `CAP_SETGID` and `CAP_NET_ADMIN` must survive, because a broker
without them cannot create a site user or apply a firewall rule. That failure
would otherwise be discovered on a customer's host.

The same suite closes two limits recorded elsewhere: the self-update transient
unit ([26](26-self-update.md)) is shown to outlive its creator and be reaped by
`--collect`, and per-site cgroup limits are read back from the kernel rather
than assumed.

## 7. Not done

- **AppArmor / SELinux profiles.** Deferred by decision. Both would have to be
  written blind: there is no Linux host in this development environment and no
  `checkmodule`/`semodule` to compile an SELinux policy against. A MAC profile
  that has never been loaded is not a security control, it is a file — and
  shipping one implies a guarantee that has not been tested. The systemd
  confinement above is distro-agnostic and takes effect today.
- **Penetration test.** An external exercise, not a code change. The threat-model
  review above is the internal half of that roadmap item; it does not substitute
  for the external half.
- **A real VM.** The systemd image above is `--privileged` with real cgroups,
  which covers unit validity, capability enforcement, transient units and cgroup
  limits — but it shares the host kernel. MAC enforcement and kernel-level
  isolation still need a virtual machine, and neither is claimed here. The
  two-node *network* path ([27](27-multi-node.md)) also remains loopback-only.
- **Whether `@system-service` is right for every customer runtime.** The filter
  is applied and proven to load; whether some language runtime needs a syscall
  outside it will surface from real workloads, not from CI.
