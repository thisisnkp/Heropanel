# 25 — Resilience & Recovery

NexPanel is a **single-node control plane** by design (`v0 · single-node`). This
document is the honest answer to "what happens when things break?" — the
resilience model, why full HA/clustering is deliberately *not* the goal, and the
concrete recovery procedures that replace it.

## The one idea that reframes everything: panel ≠ sites

npd (the control plane) does **not** serve customer traffic. Websites, mail, and
DNS are served by **independent OS services** — OpenLiteSpeed/Nginx/Apache,
Postfix + Dovecot, BIND, MariaDB/PostgreSQL — which npd only *configures* (via the
broker) and then leaves running.

**Consequence:** if npd (or np-broker) crashes or restarts, **customer sites,
mail, and DNS keep working.** Only the *management UI/API* is briefly
unavailable. This is the same model cPanel, DirectAdmin, Plesk, and 1Panel use,
and it is why control-plane HA is far less critical than it first appears.

So the resilience strategy is not "never let npd go down"; it is **"bring npd
back fast, and be able to rebuild the node."**

## Tier 1 — Self-healing single node (in place)

The control plane recovers from transient faults on its own, at three levels:

1. **In-process fault isolation** (no restart needed):
   - **Backend:** every background goroutine (schedulers, sweepers, samplers,
     dispatchers, ws hub) runs under `internal/safe.Go`, which recovers panics,
     logs a stack trace, and restarts the task with backoff. One module's
     background crash can no longer take the whole process down. HTTP handlers
     were already covered by the `recoverer` middleware.
   - **Frontend:** layered React error boundaries (root, per-route around the
     page, per-widget) contain a render crash to its own area instead of
     white-screening the SPA. The sidebar/nav stay usable; navigating away
     auto-recovers.

2. **Broker fault containment** (bulkhead + circuit breaker): every service
   reaches the privileged broker through a resilient wrapper (`internal/broker`,
   `Resilient`). A **bulkhead** bounds how many privileged calls may be in flight
   at once, so a hung broker cannot let request goroutines pile up until npd runs
   out of them — excess callers are shed fast with an `unavailable` error, and a
   caller whose own request is cancelled is released immediately. A **circuit
   breaker** trips after a run of connectivity failures and then fails fast for a
   short cooldown instead of paying a full dial+timeout on every request; it
   self-heals by letting one probe through after the cooldown and closing again on
   success. (Only connectivity failures count — a capability returning a
   validation/conflict error means the broker is healthy and never trips it.) A
   sick broker degrades the features that need it, not the whole panel.

3. **Process auto-restart** (systemd): the installed units use
   `Restart=on-failure`, `RestartSec=2s`, and `StartLimitIntervalSec=60` /
   `StartLimitBurst=5`. A crashed npd or np-broker is back within seconds.

4. **Hang detection** (systemd watchdog): npd runs as `Type=notify` with
   `WatchdogSec=30s`. It sends `READY=1` on startup and pets the watchdog every
   ~15s (`internal/systemd`). A *hung* npd — alive but not responding, which a
   plain restart policy cannot catch — stops petting and is killed + restarted by
   systemd.

Together these give high effective control-plane availability with **zero
clustering complexity**, and the data plane is unaffected throughout.

## Tier 2 — Disaster recovery (node lost)

For "the node is gone", recovery is restore-based, not cluster-based.

### What to back up (already automated)
- **Panel self-backup:** a sealed snapshot of npd's own datastore on a schedule
  (`backup.panel.*`), to local disk and/or an off-box target.
- **Site backups:** per-site, zstd-compressed and sealed (AES-256-GCM), to
  local + S3/SFTP/rclone (3-2-1). See [22-backup.md](22-backup.md).
- **The master key** (`NP_SECRET_KEY`): store it in a secrets manager. **Without
  it the sealed backups cannot be opened** — back it up separately from the data.

### Verified backups (a backup you cannot restore is not a backup)
Every panel self-backup is **round-tripped before it is recorded**: after sealing
and storing, npd fetches the object back from its target, decrypts it with the
live key, and confirms it unpacks to a complete archive (manifest + database). If
that fails, the object is discarded and the backup is reported as failed rather
than being silently recorded as a success you discover is unusable at recovery
time. The newest snapshot is also re-verified on startup, so a key change or
on-disk bit-rot surfaces while there is still time to act
(`internal/backup`, `VerifyPanelBackup` / `VerifyLatestPanelBackup`).

### Restore to a new node
1. Provision a fresh host and run the installer (same channel/version).
2. Restore `NP_SECRET_KEY` (and DB credentials if MariaDB) into
   `/etc/nexpanel/secrets.env`.
3. Restore the panel datastore from the latest sealed snapshot
   (`npd decrypt` + import — see [22-backup.md](22-backup.md) §7).
4. Start npd/np-broker; re-apply web/DNS/mail config (npd renders desired state
   from the datastore on demand).
5. Point DNS / the floating IP at the new node.

RTO is minutes-to-tens-of-minutes; RPO is the backup interval.

### Warm standby (optional "poor-man's HA")
If a faster failover is required without true clustering:
- Run the control-plane on **MariaDB** (not SQLite) and enable **replication** to
  a standby node.
- Keep `/etc/nexpanel` config in sync.
- On failure, promote the standby and move a **VIP (keepalived)** or update DNS.

This needs no leader election or quorum because the control plane runs
**active/standby**, not active/active — dramatically simpler and safer (no
split-brain) than clustering npd.

## Tier 3 — True HA is a data-plane concern, not a panel concern

If the requirement is "customer sites survive a node failure", that is **hosting
HA**, built from multiple web nodes behind a load balancer, replicated storage,
MariaDB Galera/replication, BIND secondaries, and backup MX. NexPanel's role is
to **manage** such a topology (the multi-node / satellite direction on the
roadmap) — npd itself does **not** need to be clustered for the data plane to be
highly available.

## What is deliberately *not* done, and why

| Not done | Why |
| --- | --- |
| Active/active npd clustering | Split-brain risk, quorum needs 3 nodes, high complexity — and panel downtime doesn't take sites down, so the payoff is low. |
| Distributed tracing | Single process; structured logs + request IDs suffice at this scale. |

## Summary

Resilience here is **self-healing + recoverable**, not **clustered**:
fault isolation (in-process) → fast auto-restart → hang-detecting watchdog →
sealed backups → restore/standby. This matches how production hosting panels
actually run, keeps the single-node simplicity, and keeps customer workloads up
even while the control plane is restarting.
