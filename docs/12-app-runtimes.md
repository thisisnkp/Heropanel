# 12 — App Runtimes (module design)

Phase 3, in-core. Completes the "deploy a real app" story: a git-deployed
Node/Python/Go app is run as a **supervised long-running process** (a per-site
systemd unit, executed as the site's own unprivileged user in its release
directory), and OpenLiteSpeed **reverse-proxies** the site's domain to it. This
is the roadmap's Phase-3 exit criterion — "deploy a Next.js/FastAPI app via Git."

Builds directly on: Sites (per-site Linux user + home), Git deployments
(`current` → release symlink), the privileged broker, and the webserver renderer.

---

## 1. Scope

**In (slice 1):**
- A new site **`type: proxy`** — the vhost forwards to a local backend instead of
  serving files.
- A per-site **runtime**: `command` (the process to run), `port` (the local port
  it listens on), `env` (key/value pairs), and a `runtime` label
  (node|python|go|generic, informational).
- Broker `app.unit_apply` / `app.unit_remove`: write, enable, and manage a
  **hardened systemd unit** `heropanel-app-<vhost>.service` that runs
  `command` as the site user with `WorkingDirectory=<home>/current`, **inside the
  site's cgroup slice** (§3.2).
- Process controls: **start / stop / restart / status**.
- An optional **health check** (`health_path`, e.g. `/healthz`) the panel probes
  on the app's own port — see §3.1.
- OLS **reverse-proxy** vhost: `extProcessor <name> { type proxy; address
  127.0.0.1:<port> }` + `context / { type proxy; handler <name> }`.
- Lifecycle: setting a runtime writes+starts the unit and re-renders the vhost as
  a proxy; deleting a proxy site removes the unit.

**Deferred (documented, later slices):**
- Zero-downtime handoff (today a restart is a restart), log streaming, autoscaling.
- Continuous background health polling + alerting. The probe today runs on demand
  and after an apply/restart; a monitor that watches an app go unhealthy *later*
  belongs to the Monitor module (Phase 6).
- Non-HTTP health checks (a TCP connect, or an exec probe).
- Buildpack/nixpack auto-detection of `command` (slice 1: operator supplies it).
- Multiple processes per app (web + worker), scale count.

## 2. Model

A proxy site is provisioned exactly like any other (dedicated user + home + dirs).
Its content arrives via Git (`current` → release), and instead of OLS serving
files, a systemd unit runs the app and OLS proxies to it:

```
domain ──▶ OpenLiteSpeed vhost (context / → proxy) ──▶ 127.0.0.1:<port>
                                                          ▲
                     heropanel-app-<vhost>.service ───────┘
                     User=<siteuser>  WorkingDirectory=<home>/current
                     ExecStart=<command>   (e.g. `node server.js`)
```

Only `current` moves per deploy, so restarting the unit picks up the new release
(`WorkingDirectory=<home>/current` resolves through the symlink at exec time).

`app_runtimes` (migration 0007), 1:1 with a site:

```
app_runtimes
  id, uid, site_id (UNIQUE FK sites), runtime, command, port,
  env (JSON text), status(stopped|running|error), created_at, updated_at
```

## 3. The systemd unit (broker `app.unit_apply`)

Written to `/etc/systemd/system/heropanel-app-<vhost>.service`, root-owned 0644,
then `systemctl daemon-reload` + `systemctl enable --now <unit>`:

```ini
[Unit]
Description=HeroPanel app <vhost>
After=network.target

[Service]
User=<siteuser>
Group=<siteuser>
WorkingDirectory=<home>/current
Environment=PORT=<port> <k=v ...>
ExecStart=<command>
Restart=on-failure
RestartSec=2
# Hardening — the app can only touch its own tree.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=<home>
UMask=0027

[Install]
WantedBy=multi-user.target
```

`app.unit_remove` runs `disable --now` + deletes the file + `daemon-reload`.

**Validation** (broker, untrusted input): vhost name (`ValidateVhostName`), home
`ValidatePath`-confined, port 1024–65535, `command`/env length-bounded and
NUL-free, env keys `^[A-Z_][A-Z0-9_]*$`. `command` is written into an `ExecStart`
line — systemd parses it itself (its own quoting), and it runs **only** as the
unprivileged, sandboxed site user, so this is the same trust boundary as the git
build command (docs/11 §5). The unit name is derived from the validated vhost id,
never a raw string.

### 3.1 Health checks

**systemd reporting "started" and "the app works" are not the same claim.**
systemd has forked the process; that is all it knows. Without a probe the panel
shows green for an app that crashed on boot, failed to bind, or died on a bad
config — which is exactly when an operator most needs it to be honest.

So when `health_path` is set:

- `SetRuntime` and `start`/`restart` **wait** for the app to answer before
  reporting `running`; if it never does, the status is `error`. The wait matters:
  an app needs a moment to bind, and probing once, immediately, would call every
  healthy app broken. `ReadyTimeout` is 20s — generous, because a JVM or a
  Next.js server genuinely takes several seconds.
- `RestartForSite` (the post-deploy hook, docs/11) **returns an error** when the
  app comes back unhealthy. A deploy that builds fine but crashes the app is a
  very common failure; it must surface on the deploy, not show green.
- A **stop** is not probed — a stopped app is `stopped`, not `error`.
- With no `health_path` the panel claims nothing: the status reflects only what
  systemd reported, and `GET .../health` returns `configured: false`.

The probe is always `http://127.0.0.1:<port><health_path>` — the same place OLS
proxies to. `health_path` is validated as a **path**, never a URL: accepting
`http://elsewhere/` would turn the panel's probe into a request-forgery
primitive. Any 2xx or 3xx counts as serving (a `204` is as much a "yes" as a
`200`), and redirects are not followed.

### 3.2 The site slice

Every site gets a systemd slice, `heropanel-site-<vhost>.slice`, written at
provisioning time by the broker's `site.apply_slice`. The app unit names it in
`Slice=`, which is what "supervised **in the site slice**" means: without that
directive the unit lands in `system.slice` and a runaway app is bounded only by
the size of the node.

- **Limits** come from `site_limits` (docs/14 is databases; the table lives with
  Sites): `cpu_quota_pct` → `CPUQuota=N%`, `mem_limit_bytes` → `MemoryMax=`,
  `pids_max` → `TasksMax=`. Zero means unlimited, and unset properties are
  **omitted** rather than written as `0` — `MemoryMax=0` means "no memory at
  all", which would stop every unlimited site from starting.
- **A slice exists even with no limits**, with accounting on. The cgroup has to
  be there before anything can be placed in it, and the accounting is what lets
  an operator see a site's real CPU/memory use before deciding what to cap.
- **The kernel is told first.** `SetLimits` applies the slice and only then
  records the row: a stored limit that is not being enforced is worse than no
  limit, because the panel would report a cap that does not exist.
- **Slice naming escapes `-`.** In systemd a `-` in a slice name is the
  *hierarchy separator*: `a-b-c.slice` means `c` inside `a-b` inside `a`. A vhost
  may legally contain `-`, so `my-site` would silently nest as
  heropanel/site/my/site — a different cgroup than intended, and one that
  collides with a site actually named `my`. The literal is escaped to `\x2d`,
  as systemd's own escaping does.

**Not verified live:** the e2e container has no systemd (a shim supervises the
units), so it asserts that the slice unit is written with the right properties and
that the app unit is placed in it — but **not** that the kernel enforces the
limits. That is systemd's job and depends on real cgroup v2 delegation.

**Deferred:** placing the **php-fpm pool** in the site slice. A pool is a child of
the php-fpm master process, not its own unit, so it needs different machinery
(cgroup delegation or pool `rlimit_*`) than a `Slice=` line. The other columns
docs/03 lists for `site_limits` (io, disk/inode quota, bandwidth) need per-device
IO limits, filesystem quotas, and the Monitor module respectively, and are
deliberately absent from the table rather than present-and-ignored.

## 4. Reverse-proxy vhost

Rendered by `internal/webserver` when a site has a `ProxyTarget` (its runtime
port). Server-level external app + a proxy context:

```
extProcessor proxy_<vhost> {
  type                    proxy
  address                 127.0.0.1:<port>
  maxConns                100
  respBuffer              0
}
virtualhost <vhost> {
  ...
  context / {
    type                  proxy
    handler               proxy_<vhost>
    addDefaultCharset     off
  }
}
```

A proxy site with **no runtime yet** (just created) renders as a normal static
vhost (docRoot `public`) so OLS config stays valid; setting the runtime upgrades
it to a proxy. Applying config is the same fail-safe broker `webserver.apply`
(reload-first) as everything else.

## 5. API

Runtime is a facet of a site, so it reuses `site.read` / `site.write` (like the
PHP selector under `/sites/{uid}/php`) — no new RBAC scope.

```
GET  /api/v1/sites/{uid}/runtime           site.read   → runtime (404 if unset)
GET  /api/v1/sites/{uid}/runtime/health    site.read   → probe result
PUT  /api/v1/sites/{uid}/runtime           site.write  → upsert + apply unit + reproxy
POST /api/v1/sites/{uid}/runtime/start     site.write  → start unit
POST /api/v1/sites/{uid}/runtime/stop      site.write  → stop unit
POST /api/v1/sites/{uid}/runtime/restart   site.write  → restart unit (pick up new release)

GET  /api/v1/sites/{uid}/limits            site.read   → resource limits
PUT  /api/v1/sites/{uid}/limits            site.write  → apply limits to the slice
```

`health` is `site.read`: it asks a question, it does not change anything.
`limits` is a full replace, not a patch: every field is part of one envelope, and
an omitted field means "unlimited" rather than "leave as-is".

## 6. Definition of Done

- [x] Domain + service + repo interface
- [x] Broker capabilities (unit apply/remove) with validation + hardening
- [x] Reverse-proxy vhost render
- [x] REST endpoints + RBAC + audit ([15](15-audit.md)). This box was checked
      long before anything wrote to `audit_log`.
- [x] Unit tests: service, capability (unit file + systemctl argv), proxy render
- [x] **Live e2e** (`deploy/docker/e2e/run-app.sh`): a `proxy` site is git-deployed,
      the runtime is set, a real Node process runs as the site user, and OLS
      reverse-proxies the domain to it — `curl` returns the app's dynamic
      response (`HeroPanel app live on port 3000 pid …`); restart changes the pid.
      The health probe returns `{"healthy":true,"status_code":200}` for the live
      app, and a runtime pointed at a command that exits immediately reports
      `"status":"error"` / `"healthy":false` rather than a green light.
      The container has no systemd, so the process is supervised via the
      systemctl-shim; the unit file itself is asserted by unit tests. Production
      uses real systemd. Wired into CI.
- [x] **All three advertised runtimes proven live**, each with a real build, not a
      one-liner. Wired into CI:
      - **python** — `run-fastapi.sh`: a real `requirements.txt` resolved from
        PyPI into a real venv (Ubuntu 24.04 is PEP-668, so a deploy genuinely has
        to build one), served by real uvicorn.
      - **node** — `run-nextjs.sh`: a real Next.js app, `npm install` +
        `next build` as the site user, served by `next start`
        (`x-powered-by: Next.js`).
      - **go** — `run-go.sh`: the build command compiles a real binary as the site
        user and the unit execs it out of the release directory.
- [x] **Auto-deploy + rollback proven live** (`run-fastapi.sh`): `git push` → the
      webhook endpoint (no session, no CSRF) → a `"trigger":"webhook"` deployment
      → the app restarts and serves v2; then rollback → the app restarts and
      serves v1 again.
- [x] The app unit is asserted to carry `Slice=heropanel-site-<vhost>.slice`, and
      `PUT /limits` is asserted to reach the slice as `CPUQuota=` / `MemoryMax=` /
      `TasksMax=` (see §3.2 for what is *not* verified live).

---
Back to [index](README.md). Related: [11 — Git Deployments](11-git-deployments.md),
[05 — Security](05-security-architecture.md), [10 — Roadmap](10-roadmap.md) Phase 3.
