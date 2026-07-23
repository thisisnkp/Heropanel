# 20 — Monitoring

The Monitor module answers "how is this host, and everything on it, doing?" —
and it does so **without the panel ever polling itself**. It is in-core today
(a Provider in the module registry, like Docker and DNS) and satellite-ready for
the Phase 9/10 gRPC transport, exactly as those are.

Back to [index](README.md).

## 1. The organising principle: subscription-gated sampling

A dashboard is the classic source of idle load. A page left open in a background
tab, polling every few seconds forever, costs the host real work for a view
nobody is looking at — multiplied by every operator who ever opened it.

So the panel inverts it. The browser **subscribes** to a channel over the
realtime hub and the server **pushes**; and — this is the part that matters — the
server samples *only while at least one client is subscribed*. An open-but-
unwatched tab that has lost its subscription costs nothing. A panel with no
dashboard open does no metric work at all.

The gate is one check in the sampler's loop (`monitor.Service.RunSampler` →
`hub.HasSubscribers`): each tick, if nobody is watching the channel, it samples
nothing and pushes nothing. The live e2e proves the whole path — a one-shot read
returns `cpu_percent: 0` on a cold process (CPU is a rate and needs two reads to
mean anything), then the moment a subscriber connects the pushed sample carries a
real, delta-computed figure. The server started working *because* someone
started watching.

## 2. Where the numbers come from

Node metrics are read straight from `/proc` and `statfs`, which are world-
readable, so **hpd reads them itself and never crosses the broker** — the broker
is for privilege, and there is none here. CPU comes from `/proc/stat` (a rate, so
two reads are diffed; idle folds in iowait, matching `top`); memory from
`/proc/meminfo` (honest `MemAvailable`, with the free+buffers+cached fallback for
ancient kernels); load from `/proc/loadavg`; uptime from `/proc/uptime`; and disk
from `statfs`, using `df`'s own arithmetic so reserved root blocks do not make a
full disk read as 100 % for a tenant who cannot touch them.

Every read is best-effort: a metric that cannot be read (a file absent on a
non-Linux developer host) is left at zero rather than failing the whole sample,
because a partial dashboard beats none. The parsers are pure functions over
fixture bytes, so they are tested on any OS; the real `/proc` is proven by the
Linux e2e.

Later slices reach past the node — a site's cgroup accounting (already on, from
Phase 1's slice), a container's `docker stats` (Phase 5), a service's systemd
status — and those *do* go through the broker, because reading another user's
cgroup or a unit's state is a privileged act. They are §5 below.

## 3. Permissions

Monitoring is host-wide, not scoped to one site, so — like Docker — it carries
its own pair rather than riding on `site.*`:

- **`monitor.read`** — view live and historical metrics. It also gates the
  realtime `monitor:*` channels: metrics are not tied to one resource an operator
  might own, so the permission *is* the access boundary, checked in the hub's
  channel authorizer.
- **`monitor.write`** — configure alert thresholds and notification targets
  (§5, alerts slice).

An unauthenticated read is a 401; a subscriber without `monitor.read` is refused
the channel and sees nothing.

## 4. The one-shot read vs. the live channel

`GET /api/v1/monitor/node` returns a single sample. It exists for the **initial
paint** — the first numbers a page shows before its subscription starts
delivering — and for any client that wants one sample without opening a socket.
It is deliberately *not* something a dashboard hits on a timer; that would be the
idle polling the whole design exists to avoid. The live view is the
`monitor:node` channel, pushed.

The hub's local `Publish` needs no Redis (Redis only bridges cross-process job
events), so the hub — and therefore live monitoring — runs even on a single-node
install with Redis switched off. The e2e asserts exactly that.

## 5. What is built, and what is next

**Slice M1 (done):** live node metrics — CPU, load, memory/swap, uptime, per-
filesystem disk — one-shot read plus subscription-gated push, the Monitoring
dashboard, and the `monitor.read` permission. Proven live in
`deploy/docker/e2e/run-monitor.sh`: the hub comes up without Redis, the node
sample reports sane memory/load/disk, an unauthenticated read is refused, and a
subscriber receives a pushed sample.

**Slice M2 (done):** per-site and service metrics, each on its own
subscription-gated channel (`monitor:sites`, `monitor:services`) so watching one
never triggers the other's work.

- **Per-site** usage — memory, CPU %, task count — is read from each site's
  **cgroup v2 accounting** directly: `memory.current`, `cpu.stat` and
  `pids.current` are mode-0444 files the kernel publishes to everyone, so hpd
  reads them with no broker (there is no privilege in reading a public number).
  This is the payoff of Phase 1 turning accounting on at slice creation. CPU is a
  rate, so a per-slice baseline is kept and diffed; a site whose slice has no
  cgroup yet reports `present:false` and the dashboard shows "idle" rather than a
  misleading zero. Live per-site *values* need real systemd slices, which the
  shim-based e2e container has not, so the cgroup parsing is unit-tested; the
  endpoint shape is proven live.
- **Service health** is systemd's view of a unit, so — unlike a /proc number — it
  goes through the broker's read-only `service.status` capability (the read twin
  of `service.restart`, sharing its allowlist; hpd never execs systemctl itself).
  A broker error yields "unknown" rather than dropping the tiles. Proven live: the
  services endpoint reports a real state per service (the running OpenLiteSpeed as
  `active`, the unstarted database/cache as `inactive`), the call lands on the
  broker's audit chain, and a subscriber receives a pushed `monitor:services`
  event.

Container stats already exist from Phase 5 (`docker.container.stats`) and are
surfaced on the Docker page; the dashboard links there rather than duplicating
them.

**Slice M3 (done):** history and rollups. A **persister** writes one raw node
point a minute — always, *not* subscription-gated, because a chart that skipped
the hours nobody watched would lie by omission (a once-a-minute /proc read is
nothing). An **hourly rollup** averages the last whole hour of raw into a single
'hour' row and prunes: raw ~48h for recent detail, hourly ~30d for the long tail,
so a week-long chart reads ~168 rows not ~10 000, and the table never grows
unbounded. The averaging is done in Go, not SQL `AVG`, so it is byte-identical on
SQLite and MariaDB (which round and cast integers differently), and the rollup is
idempotent (it replaces the hour row). `GET /monitor/history?range=…` picks the
granularity for the caller — raw within the window, hourly beyond. The dashboard
charts it as **single-series small multiples** (CPU %, memory %, load) — never two
scales on one axis — inline SVG with a hover crosshair, no chart library.
Repository unit tests cover the rollup average, its idempotency, the empty-hour
no-op and pruning against real SQLite; the endpoint shape is proven live.

**Slice M4 (done):** alerts. A rule watches one metric, compares it with an
operator against a threshold, and fires **only after the breach has persisted for
`for_sec` seconds** — the duration is the whole point, because a single-tick spike
is not an incident and paging on one is how alerting gets muted. A rule fires once
per incident (not once per tick) and **resolves** when the metric recovers, so an
operator sees the incident's shape. Evaluation is folded into the history
persister — the raw sample it just wrote is what the rules check — so there is no
second sampler. Notifications go to **webhook** or **Telegram** (both outbound
HTTP, with a bounded client so a slow endpoint never wedges the evaluator), or
**`log`** (the recorded event is itself the channel). Notification targets are
**sealed with the panel's data key** (a Telegram token is a standing credential)
and are **write-only** — the API never returns them, and the `log` kind needs no
key so it always works. Rules are `monitor.write`; reading rules and events is
`monitor.read`. Proven live: a breaching rule POSTs its webhook to a receiver, the
event is recorded, and the target never appears in the rules list. The evaluator's
breach/duration/resolve logic and the seal/write-only round trip are unit-tested.

With M4 the Monitor module is complete: node, per-site, service and container
metrics live and historical, with threshold alerting on top.

---
Back to [index](README.md).
