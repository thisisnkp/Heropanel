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
  `command` as the site user with `WorkingDirectory=<home>/current`.
- Process controls: **start / stop / restart / status**.
- OLS **reverse-proxy** vhost: `extProcessor <name> { type proxy; address
  127.0.0.1:<port> }` + `context / { type proxy; handler <name> }`.
- Lifecycle: setting a runtime writes+starts the unit and re-renders the vhost as
  a proxy; deleting a proxy site removes the unit.

**Deferred (documented, later slices):**
- Auto-restart the unit after a successful git deploy (slice 1: restart is an
  explicit call; the wiring point is noted in code).
- Health checks / readiness gating, zero-downtime handoff, per-app resource
  caps via the cgroup slice, log streaming, autoscaling.
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
PUT  /api/v1/sites/{uid}/runtime           site.write  → upsert + apply unit + reproxy
POST /api/v1/sites/{uid}/runtime/start     site.write  → start unit
POST /api/v1/sites/{uid}/runtime/stop      site.write  → stop unit
POST /api/v1/sites/{uid}/runtime/restart   site.write  → restart unit (pick up new release)
```

## 6. Definition of Done (this slice)

- [x] Domain + service + repo interface
- [x] Broker capabilities (unit apply/remove) with validation + hardening
- [x] Reverse-proxy vhost render
- [x] REST endpoints + RBAC + audit
- [x] Unit tests: service, capability (unit file + systemctl argv), proxy render
- [x] **Live e2e** (`deploy/docker/e2e/run-app.sh`): a `proxy` site is git-deployed,
      the runtime is set, a real Node process runs as the site user, and OLS
      reverse-proxies the domain to it — `curl` returns the app's dynamic
      response (`HeroPanel app live on port 3000 pid …`); restart changes the pid.
      The container has no systemd, so the process is supervised via the
      systemctl-shim; the unit file itself is asserted by unit tests. Production
      uses real systemd. Wired into CI.

---
Back to [index](README.md). Related: [11 — Git Deployments](11-git-deployments.md),
[05 — Security](05-security-architecture.md), [10 — Roadmap](10-roadmap.md) Phase 3.
