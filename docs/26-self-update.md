# 26 — Self-update

Moving a running installation to another release: channels, signed artifacts,
an atomic swap, and a health gate that restores the previous version when the
new one does not come up.

The contract the whole design serves is one sentence:

> **Either the new version is answering, or the old one is.**

Back to [index](README.md). Related: [07 — Installer](07-installer-architecture.md),
[05 — Security architecture](05-security-architecture.md).

---

## 1. The problem that shapes everything

A panel update is not an ordinary privileged operation. It has to replace
**`np-broker`** — the root component that performs privileged operations — and
the HTTP request asking for it is served by **`npd`**, which is also being
replaced. Anything that swapped the binaries inline would be killing itself
halfway through: the restart tears down the connection before a reply, leaving
the caller unable to tell whether the update happened, and no live broker to
undo it if it did not.

Three things in `broker/policy/policy.go` say so plainly. `PathRoots` is
`/srv/nexpanel/sites`, so nothing may write `/opt/nexpanel/bin`. The
`Services` allowlist contains web/db/mail units and deliberately **not** `npd`
or `np-broker`. And the broker has no re-exec hook.

None of that is loosened. Instead the work is moved somewhere that outlives the
processes involved.

## 2. Who does what

```
npd                     broker              systemd                np-installer
 │                        │                    │                        │
 ├─ fetch channels.json   │                    │                        │
 ├─ verify signature      │                    │                        │
 ├─ download artifacts    │                    │                        │
 ├─ verify SHA256SUMS     │                    │                        │
 ├─ stage to DataDir      │                    │                        │
 ├─ panel DB snapshot     │                    │                        │
 ├─ panel.update ────────►│                    │                        │
 │                        ├─ systemd-run ─────►│                        │
 │                        │                    ├─ transient unit ──────►│
 │◄─ 202 Accepted ────────┤                    │                        │
 │                                                                      │
 │ (npd and np-broker are restarted by the installer — both die here)   │
 X                        X                    │                        ├─ re-verify
                                               │                        ├─ snapshot binaries
                                               │                        ├─ swap (atomic)
                                               │                        ├─ restart both
                                               │                        ├─ health gate
                                               │                        └─ rollback on failure
```

- **npd** does everything unprivileged: discovery, download, verification,
  staging, and the pre-update database snapshot. It never touches
  `/opt/nexpanel/bin`.
- **The broker** contributes exactly one narrow verb, `panel.update`. It takes
  no command, no unit body and no destination — only a staged directory it
  re-validates — so the capability's blast radius is "start the installer", not
  "run something as root".
- **systemd** owns the transient unit (`--unit=np-selfupdate --collect`). This
  is the load-bearing part: the unit is not a child of the broker, so it
  survives the broker being restarted underneath it.
- **np-installer** performs the swap, the restarts, the health gate and the
  rollback. It is the component whose entire job already was "place verified
  binaries, write units, verify health, roll back"; self-update gives it one
  more mode rather than a second implementation of that logic.

Staging lands in `<DataDir>/updates/<version>/`, which npd's own systemd unit
already grants `ReadWritePaths` on — so **no broker path policy is widened** to
make this work.

## 3. Trust: two signed documents, one key

```
<base>/channels.json          {"channels":{"stable":{"version":"1.2.3",…}}}
<base>/channels.json.sig      base64 ed25519 over channels.json
<base>/1.2.3/npd
<base>/1.2.3/np-broker
<base>/1.2.3/np-installer
<base>/1.2.3/SHA256SUMS
<base>/1.2.3/SHA256SUMS.sig   base64 ed25519 over SHA256SUMS
```

`channels.json` answers *which version*; `SHA256SUMS` answers *what bytes*.
Both are signed by the **same release key** the operator already pins as
`NP_RELEASE_PUBKEY` — the anchor `install.sh` forwards and `np-installer`
verifies at install time. There is no second trust root, and the artifact chain
is byte-for-byte the one Phase 0 shipped.

Two details matter more than they look:

- **Signature before parse.** `ParseManifest` verifies the detached signature
  over the bytes *as received*, then decodes. Parsing first would run a JSON
  decoder over attacker-controlled input and verify a re-encoding of it, which
  is how signature-bypass bugs get built.
- **Verified twice.** npd verifies before staging; np-installer verifies again
  before swapping. The component that overwrites root's binaries does not take
  another process's word for what is safe.

An unsigned release is a *warning* at install time — the operator is standing at
the machine having fetched the artifacts themselves. Over the network,
unattended, it is a **refusal**: with no key pinned, `--update` stops before
touching anything.

Key parsing for both this chain and the marketplace's publisher keys lives in
one place, `pkg/edkey`. Two copies of "how do we read a trust anchor" is
precisely where a trust chain rots.

## 4. The health gate

`systemctl restart` returning 0 proves nothing — a panel that starts and then
dies on a bad migration passes it. The gate is two claims:

1. **`/readyz` answers.** Not `/healthz`, which is satisfied by a listener
   being up. `/readyz` checks the datastore, Redis and the **broker socket**, so
   it is the only probe that proves the newly swapped broker is alive too.
2. **`GET /api/v1/system/info` reports the target version.** Readiness alone
   would be satisfied by a restart that silently did not take, leaving the old
   process answering perfectly well. The reported version is what distinguishes
   "the new panel is up" from "a panel is up".

Failing either restores the snapshot taken from the *live* binaries before the
swap, restarts, and records `rolled_back`.

## 5. What cannot be undone

Binaries roll back by copying the old bytes over. **Migrations do not.** A
schema change that has run has run, and a restored older binary may not
understand it.

So a panel database snapshot is taken through the existing self-backup pipeline
([22 — Backups](22-backup.md)) *before* anything is swapped, and a snapshot that
fails **aborts the update** rather than proceeding without one. Restoring it is
deliberately out-of-band (`npd decrypt`), for the same reason panel backups
always were: a panel that needs its database back cannot be trusted to serve
that request.

## 6. Reporting back across the restart

The process that starts an update is destroyed by it. No in-process code path
can observe its own update finishing, so the outcome travels by file:
np-installer writes `<DataDir>/updates/last-result.json`, and npd's `Reconcile`
reads it at startup.

It is a file rather than a database write because np-installer has no panel
configuration, no DSN and no sealed-secret material. Teaching it to open the
panel's database would couple the one component that must work when the panel is
broken to the panel's schema.

When there is no usable result — the box lost power, the unit never started —
npd falls back to the strongest evidence it has: **the version it came back as.**
Running the target means the swap plainly happened; running the previous version
means it did not.

## 7. Configuration

| Setting | Env | Meaning |
|---|---|---|
| `update.channel` | `NP_UPDATE_CHANNEL` | `stable` \| `beta` \| `nightly` (default `stable`) |
| `update.base_url` | `NP_UPDATE_BASE_URL` | Release root serving the layout in §3 |
| `update.pubkey` | `NP_RELEASE_PUBKEY` | The ed25519 release key — the same anchor the installer pins |
| `update.auto_check` | `NP_UPDATE_AUTO_CHECK` | Poll for a newer release. Only ever *checks*; nothing installs itself |

Self-update is **off unless `base_url` and `pubkey` are both set**. A release
source with no anchor would let whoever serves that URL replace the root
component on this host, which is the one thing this design exists to prevent.
The panel reports which half is missing rather than showing an inert button.

## 8. API

| Route | Permission | Notes |
|---|---|---|
| `GET /system/update` | `system.read` | Status; an unreachable release server is a `reason`, not an error |
| `GET /system/updates` | `system.read` | Attempt history — where a rolled-back update is visible after the fact |
| `POST /system/update/check` | `system.write` | Force a signed manifest fetch |
| `POST /system/update/apply` | `system.write` | **202 Accepted**, force-audited |

`apply` answers 202 because that is the honest code: the work is accepted and
handed to a process outside this one. The handler cannot report a result — it is
restarted underneath.

## 9. Verified

Unit and integration coverage, all in CI:

- **Trust** (`internal/update`): a correctly signed release stages; a release
  signed by an **untrusted key** is refused; a **tampered binary** fails its
  checksum; a **tampered manifest** fails its signature even though every
  checksum inside it is internally consistent; a traversing version
  (`../../etc`) is refused. Every refusal is asserted to leave **no staged
  directory and no `.partial`** behind.
- **The rollback contract** (`internal/installer`): swap → gate → success with
  the broker restarted before npd; a panel that **never becomes ready** →
  previous bytes restored; a panel that is ready but **still reports the old
  version** → previous bytes restored (the case readiness alone would pass); a
  **failed restart** → previous bytes restored; a **tampered release** refused
  with nothing restarted; **no pinned key** refused.
- **Version ordering**: semver including pre-release precedence, numeric
  identifier ordering (`rc.10` > `rc.9`), build metadata ignored, the
  `0.0.0-dev` build losing to every release, and an antisymmetry property test.
- **The cross-binary contract**: a test pins that the state strings
  np-installer writes are the ones `internal/update` reads.

### The transient unit, for real

The load-bearing claim in §2 — that a transient unit outlives the process that
created it — is no longer taken on trust. `deploy/docker/e2e/systemd/` runs an
image where **PID 1 is real systemd**, and `run-systemd.sh` starts a
`systemd-run --unit=… --collect` unit from a shell that then exits, then asserts
the unit ran to completion anyway and was reaped afterwards. That is the whole
mechanism this design rests on, exercised rather than described.

### Honest limit

**The full swap is still not driven end to end under systemd.** The transient
hand-off is proven, and the swap/gate/rollback logic is proven against an
injected `Runner` and probes — but no CI job today performs a real release
replacement of running binaries followed by real restarts of both services.
Wiring that on top of the systemd image is the next step, not a new piece of
infrastructure.

## 10. Deferred

- **Delta updates.** Binary diffing needs a bsdiff-class dependency, against the
  project's hand-rolled lean-deps rule (SigV4, SFTP and WebAuthn all went the
  same way). Full artifacts download instead.
- **Independent module updates.** The marketplace already installs and
  uninstalls signed modules ([10 §Phase 9](10-roadmap.md)); "update" is a small
  follow-up on that path, not part of this pass.
- **A release CDN.** `base_url` is operator-configured. The panel's only
  outbound reach is a host the operator named.
