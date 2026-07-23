# 10 — Development Roadmap

Build order optimized so that **each phase produces something runnable and demoable**, the security boundary exists from day one, and modules layer on without rework. We implement **one module at a time**, each to a strict Definition of Done.

## Definition of Done (applies to every module)
- [ ] Domain interfaces + service layer + repository implemented (clean architecture respected)
- [ ] Broker capabilities (if any) added to the allowlist with validation + config-test + rollback
- [ ] REST endpoints + OpenAPI annotations; async ops return jobs with WS progress
- [ ] Frontend feature slice (list/detail/create flows, realtime, empty/error states)
- [ ] Unit tests (services, validation, broker capabilities) + integration tests (real MariaDB/Redis) + e2e for the primary flow
- [ ] RBAC scopes + audit coverage for every mutation
- [ ] Docs: module README + API reference + user-facing help
- [ ] Passes the module contract tests (satellite modules) and systemd hardening review

---

## Phase 0 — Foundations (the skeleton that everything hangs on)
**Goal:** a running, secure, empty panel you can log into.
- Repo/monorepo scaffold, `go.work`, Make/Task, lint, CI (build + test matrix, multi-arch cross-compile).
- `pkg/proto` base contracts; `pkg/plugin` SDK skeleton; `pkg/arch` detection.
- **`hp-broker`** with the capability framework (exec-arg-array, peer-cred + token auth, audit chain) and the *first* capabilities (system user, service restart, file-op-in-site-root).
- **`hpd`**: config loader, Chi edge + middleware chain, MariaDB + migrations (identity/RBAC/audit/settings/jobs tables), Redis wiring, job dispatcher + worker pool, realtime hub, module registry skeleton.
- **Auth**: login, sessions+JWT, Argon2id, RBAC enforcement, audit log (hash-chained), API keys.
- **Frontend shell**: app skeleton, auth flow, layout, command palette, notifications, job/progress drawer, theme system.
- **`hp-installer`** MVP: preflight/detect, install core+broker+MariaDB+Redis+OLS(panel vhost), journal + rollback; `install.sh` bootstrap.
- **Exit criteria:** `curl | bash` installs on Ubuntu + Rocky (amd64 + arm64); browser login works; a trivial async "echo" job streams progress end-to-end; rollback verified.

**Status:** The skeleton is complete. `pkg/proto` + `pkg/plugin` SDK **skeleton**
(transport-agnostic types + a `Handler` that stamps the API version and enforces
the capability allowlist; gRPC deferred to Phase 9/10, [06](06-plugin-architecture.md)),
`pkg/arch` detection, and the module **registry** ([internal/registry](../internal/registry))
into which in-core features register their capabilities. `hp-broker` with the
capability framework (exec-arg-array, peer-cred + token auth, hash-chained audit).
`hpd`: config loader, Chi edge + middleware chain, MariaDB + migrations, Redis
wiring, job dispatcher + worker pool, realtime hub. **Auth**: login, sessions,
MFA (TOTP), RBAC enforcement, hash-chained **audit** ([15](15-audit.md)) written
through structural middleware so coverage is not per-handler, and scoped **API
keys**. **Frontend shell** ([web](../web)): app skeleton, auth flow, layout,
**command palette** (⌘K), toast notifications, global **job/progress drawer**
(WS-backed), and a theme system — plus the full Phase 1–3 feature slices on top.
**`hp-installer`**: preflight/detect, plan, and the **execute + journal + resume
+ rollback** path ([07](07-installer-architecture.md)).

**Verified live** (Docker, in CI): `run-installer.sh` — on a fresh **`ubuntu:24.04`
(apt)** *and* **`rockylinux:9` (dnf)** image, `hp-installer --execute` installs
packages, creates the service user, renders config + hardened units, migrates a
SQLite store as that user, starts the broker + daemon, and its own verify step
confirms `/healthz` answers; `--resume` is a no-op once everything is done; and
`--rollback` reverses the install (user, files, units removed; panel stops).
`run-ui.sh` — the built SPA is embedded (`//go:embed all:dist`) and served by
`hpd`, deep links fall through to client-side routing, and every page the UI
renders has a live endpoint behind it. The OpenAPI document is served at
`/api/v1/openapi.json` and a drift test fails if any mounted route is
undocumented ([04](04-api-design.md)).

**arm64 is verified** too: `run-arch-smoke.sh` runs the cross-compiled binaries
under qemu on both Ubuntu and Rocky aarch64 images — arch detection reports
`linux/arm64`, the SQLite driver applies every migration, and the broker's
offline self-check passes. The binaries are **checksum-verified** at install time
against a `SHA256SUMS` manifest (a mismatch aborts and rolls back), and the spec
now has an interactive **`/api/docs`** viewer.

**Deferred from this phase:** the public `install.sh` bootstrap + `get.heropanel.io`
hosting, and the **cryptographic signature** (beyond checksum) of fetched
artifacts (§6 of [07](07-installer-architecture.md)); a full *emulated* arm64
apt/dnf install (the install logic is arch-agnostic and proven on amd64, so the
arm64 check smoke-tests the binaries rather than re-running the package manager
under emulation); and the "echo" demo job specifically — the async job + WS
progress path itself is exercised by the Phase 3 runtime suites (`run-nextjs.sh`).

## Phase 1 — Sites Core (the reason the panel exists)
**Goal:** create and serve a real PHP + static site with full isolation.
- **Sites module (in-core)**: create/list/detail/suspend/clone; per-site Linux user/group; site directories; cgroup slice + `site_limits`; OLS vhost generation (validated, tested, reloaded via broker); logs.
- **PHP module (in-core)**: multi-version support, PHP selector, dedicated FPM pool per site, php.ini editor, extension manager, FPM sizing, OPcache/JIT, Composer auto-install.
- **Static + Proxy** site types.
- Frontend: Create-Site wizard, per-site workspace (Overview, Domains, PHP/Runtime, Logs, Advanced).
- **Exit criteria:** create a WordPress-ready PHP site and a static site, each fully isolated (separate user, pool, tmp, logs), reachable over HTTP, PHP version switchable per site.

**This phase is now closed.** The bullets that were open after the first pass —
`php.ini` editor, extension manager, FPM sizing, OPcache/JIT ([16](16-php.md)),
and site **suspend / clone** plus per-site **logs** ([03](03-data-model.md), and
the lifecycle notes below) — all landed with live e2e. Composer auto-install and
the **cgroup slice + `site_limits`** landed alongside Phase 3
([12 §3.2](12-app-runtimes.md)).

**Verified live** (Docker, in CI): `run-php-tuning.sh` — an ini override,
memory_limit and an OPcache toggle observed in a served phpinfo; invalid FPM
sizing rejected by two independent guards while the site keeps serving; a
php.ini value that tries to break out of its directive refused; an extension
disabled then re-enabled for the whole version, each seen in a served phpinfo
after a real FPM restart. `run-lifecycle.sh` — a suspended site returns 503 and,
crucially, its domains do **not** fall through to another customer's site; resume
restores it; clone produces a separate site whose files belong to its own user;
logs (0750, owned by the site user) are read back through the broker.

**Deferred from this phase:** Rocky/Alma PHP layout (the extension manager is
Debian/Ubuntu-only for now); per-version OPcache shared-memory tuning (a
`PHP_INI_SYSTEM` surface distinct from the per-site pool).

## Phase 2 — Domains, SSL, DNS ✅ **DONE**
- **SSL (in-core)**: Let's Encrypt (HTTP-01 + DNS-01/wildcard), ZeroSSL, custom upload, auto-renewal scheduler, per-domain status.
- **Domains**: aliases, subdomains, redirects, force-HTTPS.
- **DNS module (satellite)**: authoritative zones (PowerDNS/BIND backend), record CRUD, import/export, DNSSEC.
- **Exit criteria:** point a domain, auto-issue + auto-renew a cert (incl. wildcard via DNS-01), manage its zone in-panel.

**Status:** Domains ([internal/domain](../internal/domain)) — aliases/subdomains map onto
the vhost, redirect domains 301 to an absolute target, force-HTTPS is opt-in
(enabling it before a cert exists would take the site offline). DNS
([13-dns.md](13-dns.md)) — authoritative zones + record CRUD on a real BIND9
backend. SSL — HTTP-01, self-signed, custom upload, **DNS-01 incl. wildcard**
(publishing `_acme-challenge` TXT into a managed zone), and a **renewal sweeper**
([internal/ssl/renew.go](../internal/ssl/renew.go)) that repeats whichever flow
issued each cert.

**Verified live** (Docker, in CI): `run-dns.sh` — `dig` returns authoritative
answers for API-created records; `run-domains.sh` — alias serves, redirect 301s,
force-HTTPS toggles. `run-acme.sh` — a **real HTTP-01 order against Pebble** (the
Let's Encrypt team's test ACME server): account registration, a challenge written
to the site webroot and served by OpenLiteSpeed, Pebble's validation authority
fetching it, finalize, and a downloaded leaf **signed by the Pebble CA** — the
full RFC 8555 flow against an actual ACME server, with Pebble's CA installed into
the trust store so the ACME HTTPS calls verify normally.

**Deferred:** DNS as a true *satellite* module (needs the [06](06-plugin-architecture.md)
registry/gRPC — DNS is in-core for now); DNSSEC; zone import/export; ZeroSSL.
(Live ACME against a real CA is **now verified** — see `run-acme.sh` above.)

## Phase 3 — Databases & Git deployments ✅ **DONE**
- **Databases (in-core)**: MariaDB create DB/users/grants, import/export, size; phpMyAdmin/Adminer SSO handoff.
- **Git (in-core)**: sources (GitHub/GitLab/Bitbucket; PAT/deploy key/OAuth), webhook deploys, auto pull/build, deploy history + rollback, SSH/deploy key management.
- **App runtimes (in-core)**: Node/Python/Go site types (systemd-supervised in the site slice), build/start/env/health, process controls.
- **Exit criteria:** deploy a Laravel app (DB + composer) and a Next.js/FastAPI app via Git with auto-deploy + rollback.

**Status:** Databases ([14-databases.md](14-databases.md)) — create/drop, users,
grants **and revoke**, size, gzipped export (streamed, then deleted), streamed
import, and an Adminer hand-off that **mints a throwaway account per session**
rather than storing database passwords. Git ([11-git-deployments.md](11-git-deployments.md))
— **private repos** via HTTPS token or a panel-generated ed25519 **deploy key**,
sealed at rest by [pkg/secrets](../pkg/secrets) (AES-256-GCM, AAD-bound per row);
webhook deploys, history, rollback, auto-restart, **release pruning**, and
**Composer auto-install**. App runtimes ([12-app-runtimes.md](12-app-runtimes.md))
— proxy sites, hardened systemd units, OLS reverse-proxy, process controls, and
**health checks** that make `running` mean the app actually answers.

App runtimes run **in the site slice**: every site gets
`heropanel-site-<user>.slice` at provisioning, the app unit is placed in it, and
`site_limits` (CPU/memory/tasks) are applied to it — closing Phase 1's "cgroup
slice + `site_limits`" bullet as well.

**Exit criteria met, verified live** (Docker, in CI):
- **Laravel app (DB + composer)** — `run-php-app.sh`: deploys from a **private**
  repo, Composer resolves and installs a real dependency with **no build
  command**, and the served page reads a row out of a panel-created MariaDB
  (`composer dependency: loaded` / `db row: hello from mariadb`). Export streams a
  verified gzip; import restores a deleted row.
- **FastAPI app via Git with auto-deploy + rollback** — `run-fastapi.sh`: a real
  `requirements.txt` resolved from PyPI into a real venv, served by real uvicorn;
  then `git push` → **webhook** → `"trigger":"webhook"` → app restarted → v2
  served; then **rollback** → app restarted → v1 served.
- **Next.js app** — `run-nextjs.sh`: real `npm install` + `next build` as the site
  user, served by `next start` (`x-powered-by: Next.js`), over the **async job
  path** (a real build cannot finish inside an HTTP request).
- **Go app** — `run-go.sh`: the build command compiles a real binary as the site
  user; the unit execs it out of the release.
- `run-git-private.sh` — a real `sshd`: the clone fails with
  `Permission denied (publickey)` until the generated deploy key is registered,
  then serves; the private key never appears in the API response, is sealed at
  rest, and leaves nothing on `/run`.
- `run-app.sh` — the Node app probes healthy, while a crash-on-boot runtime
  reports `error`, not green.
- `run-git-token.sh` — the **token/HTTPS** clone path against a real HTTPS git
  server (`git http-backend` behind TLS from a private CA installed into the
  trust store, HTTP Basic auth): the right token clones and serves; a wrong token
  is refused with `Authentication failed`; the token is sealed at rest.
- `run-git-private.sh` also now proves **SSH host-key pinning**: a wrong pinned
  key is refused under strict checking (`host key verification failed`) even
  though the deploy key is valid, and the correct pin clones.
- Webhook **signature** verification is exercised by `run-fastapi.sh`: a valid
  GitHub HMAC-SHA256 signature over the body is accepted, a tampered one is
  denied, and the proof kind is audited.

**Deferred:** OAuth app authorization (paste a token instead); rotating data keys
for sealed credentials; PostgreSQL/Mongo engines; php.ini / extension management
(open from Phase 1's PHP bullet); placing the **php-fpm pool** in the site slice
(a pool is a child of the fpm master, not its own unit); the remaining
`site_limits` columns docs/03 lists (io / disk+inode quota / bandwidth) — they
need per-device IO limits, filesystem quotas, and the Monitor module. (SSH
host-key pinning, the token/HTTPS clone, and provider webhook signatures are
**now done** — see the live suites above.)
**Not verified live:** the e2e container has no systemd, so the slice's
*properties* and the app unit's `Slice=` are asserted, but **kernel enforcement of
the limits is not** — that is real systemd's job.

## Phase 4 — Files, Editor, Terminal
- **File Manager (in-core, baremetal-only)** — **done**, the full roadmap list and then some: browse / upload (incl. drag-and-drop) / download (files **and folders**) / **compress** (zip + tar.gz) / extract / permissions / **ownership repair** / recursive **search** (name + content) / image preview / **diff** / **nested gitignore-aware** listing, plus new file, new folder, rename, delete, **copy / cut / paste / duplicate**, a **right-click menu**, sortable columns, a hidden-files toggle, and multi-select with bulk actions; hard-blocked for git/docker sites. Every op runs as the site's Linux user via the broker (twelve `file.*` capabilities), path-confined, chunked under the 1 MiB wire cap, binary-safe — the one exception being `file.chown`, which must run as root to change an owner at all and is constrained so it can only ever assign the site's own account. Copy and move never silently overwrite: the destination is checked first and a collision is a 409 unless the caller asks for a free name. Folder download builds the zip server-side, streams it, and deletes it, so nothing accumulates in the tree. See [17 — File Manager](17-file-manager.md). Verified live (`run-files.sh`): run-as-user ownership, binary round-trip, `../../etc/passwd` clamping, archives holding relative paths and round-tripping, copy refusing to clobber and clamping a traversing destination, a folder download that leaves no temp archive, search confinement, ownership repair, and the baremetal gate — all in CI.
- **Code editor — done (CodeMirror 6, not Monaco):** chosen over Monaco for bundle size and strict-CSP compatibility (its stylesheet injects via the CSSOM, so it needs no `'unsafe-inline'`); code-split so it loads only when a file is opened. **Multi-file tabs** with per-tab dirty state, a **diff view** (dependency-free LCS differ) showing what a save will change, per-extension syntax (php/html/js/ts/css/json/md/py/yaml), app-driven light/dark theme, and working shortcuts: `Ctrl/⌘-S` save (bound inside CodeMirror at highest precedence *and* on the panel, so it never hits the browser's save dialog), `Ctrl/⌘-A` select all, `Ctrl/⌘-F` find, `Ctrl/⌘-D` select next occurrence, `Ctrl/⌘-Z`/`-Y` undo & redo, Alt-click multi-cursor. In the browser: `/` filter, `Ctrl/⌘-A` select all, `Ctrl/⌘-C`/`-X`/`-V` copy, cut and paste, `F2` rename, `Enter` open, `Delete` remove selection, `Esc` clear — each stood down while typing in an input, and `Ctrl/⌘-C` also stands down when text is selected, since then it means "copy this text". The pure logic underneath (the ignore matcher, the differ, the path helpers) is covered by a `vitest` suite that runs in CI.
- **Web terminal (xterm.js + PTY)** — **done**: a real PTY hosted by the root broker and run as the site's Linux user (`runuser`, fixed argv, never root), bridged to xterm.js over a WebSocket. The broker connection *upgrades* to a bidirectional stream rather than growing a second protocol, so the peer-credential check, token handshake, policy gate, and audit chain all still apply. Own permission (`terminal.use`), force-audited sessions, working directory clamped by the same helper the File Manager uses, and disconnect kills the whole process group. The UI does what a terminal actually needs: `Ctrl/⌘-Shift-C`/`-V` copy and paste (plain `Ctrl-C` is deliberately untouched — it is still SIGINT), `Shift-Insert`, a right-click menu, `Ctrl/⌘-Shift-F` scrollback search, a font size persisted across visits, and fullscreen with `Esc` to leave. See [18 — Web Terminal](18-web-terminal.md). Verified live (`run-terminal.sh`): shell runs as the site user, I/O round-trips, a traversing `cwd` never reaches `/etc`, unauthenticated upgrades are refused, no orphaned processes, session on the hash chain. The PTY layer is additionally unit-tested against a real pseudo-terminal (I/O, window size, resize, exit code, and that closing a session kills a *backgrounded* child), and the transport pins that a refused terminal stays a plain response instead of upgrading to a stream.
- **Session recording/playback** — **done**. Every terminal session is recorded as **asciicast v2** (the asciinema format, so it outlives this software) and replayed in-panel through xterm.js — the same emulator the live terminal uses, so no second player library ships. Output *and* keystrokes are captured, kept 30 days, swept hourly, and readable only with `terminal.recordings.read`; deleting needs a separate `terminal.recordings.delete`, because destroying an audit artifact is what an operator under scrutiny would most want. Downloading one is force-audited, and the terminal says it is recorded *before* the session starts. **Passwords are never stored:** input typed at a password prompt is replaced with a single `[redacted]` marker (one per run, not per keystroke — the count would leak the length). The discriminator is the subtle part and the live e2e is what found it: "redact while ECHO is off" is *wrong*, because readline runs with ECHO off nearly all the time and that rule redacted every command anyone typed. A real password prompt is ECHO off **while still canonical** (`ICANON` set) — `read -s` and getpass keep the kernel's line discipline and just silence it, whereas readline clears both. Only the broker can see that state, so it reports it over a `StreamEcho` control frame. Honest limit: this redacts *input*; a program that prints a secret to the terminal itself is still recorded. Recordings are a **top-level page** listing every session across every site, beside the audit log — not a sub-view of the terminal. That was a fix, not the first design: the list originally lived only inside the Terminal tab, which is gated on `terminal.use`, so the exact role the separate permission was created for — an auditor with `terminal.recordings.read` and no shell — could not open a single recording. Every backend test passed, because the permission was correct in the API and wrong in the navigation; `web/e2e/recordings.spec.ts` now pins it, and a repository test pins the site join the cross-site list is unreadable without. Deferred: server-side search across all history (the page loads the 200 newest and says so, rather than letting a client-side filter's "nothing matches" pass for "no such session"). See [18 — Web Terminal](18-web-terminal.md).
- **Browser e2e (Playwright)** — **done**: a Chromium suite driving the **real hpd serving the real bundle**, covering routing, first-run setup, sign-in, deep-link reload, the command palette, and navigation. It deliberately stops at privileged behaviour — file ops as the site user, a PTY, recording — which needs a root broker and real Linux accounts and stays in the container e2e. Wired into CI with a report artifact on failure. It immediately earned itself: it caught that *any* `/auth/status` failure (a 429 from the rate limiter, a 500) rendered the "panel is not configured" screen, sending an operator to fix a database setting that was never the problem — now only a 404 counts as that evidence.
- **Deferred out of Phase 4 (honest list):** **full git ignore semantics** — nesting and precedence now work, but character classes, `.git/info/exclude` and the global excludesfile do not, and the matcher remains a display hint rather than a correctness boundary; **true streaming archives** — folder download no longer leaves anything behind, but it still materialises a temporary zip, because `zip` cannot write to a pipe; multi-file **upload as a single archived transfer**; and terminal **reattach / multiple concurrent tabs** with idle-timeout policy knobs. Frontend testing is now unit (`vitest`) **and** browser (`Playwright`); what remains uncovered is privileged UI behaviour, which the container e2e proves at the API level instead of through the DOM.
- **Two gaps found and closed after the phase was first called done**, recorded because both were invisible rather than broken: **uploads had no progress at all** (`fetch` has no upload-progress event, so a large upload showed only a spinner — the transport is now `XMLHttpRequest` with a byte-accurate, cancellable bar), and the **File Manager and terminal routes were absent from `docs/openapi.json` entirely** — the spec is generated by walking a test router that never mounted those two services, so ~1500 lines of the published API were silently missing while the drift test passed. The generator now mounts them; a reflection-based guard fails on any `Deps` field the test router leaves nil, so the *class* of bug cannot recur; and a new **permission drift test** drives the real router to prove every documented permission is the one actually enforced, which nothing had ever checked.
- **Exit criteria:** edit files ✓, extract an archive ✓, and open an audited terminal scoped to a single site user ✓ — **Phase 4 complete**, with every feature named in the bullets above now built, not just the exit criteria.

## Phase 5 — Docker & One-Click Apps
- **Docker module — foundation done, in-core (not satellite).** The registry is transport-agnostic by construction and `internal/registry` states plainly that the gRPC satellite transport is Phase 9/10 work, so Docker registers as an in-core Provider today and can be extracted to a process later without the UI noticing. **Privilege went to the broker, not to the `docker` group** — a deliberate departure from [06](06-plugin-architecture.md)'s manifest example, because membership of that group is root by another name and would have made a compromise of the network-facing `hpd` a compromise of the host. Eleven capabilities (`docker.info`, container list/inspect/logs/stats, start/stop/restart/remove, image list/pull), each policy-gated and audited, all driving the docker CLI with an argv array so the dependency surface stays at zero. Two boundaries do the work: an **ownership label** verified by live inspect before any mutation — the equivalent of the File Manager's path confinement, and without it a stop button is a remote off-switch for every container on the host — and an allowlist that makes a value docker would read as a **flag** (`--privileged` as a container name) unrepresentable, which an argv array alone does *not* prevent. Unmanaged containers are listed read-only rather than hidden: an admin whose host is out of memory must see what is eating it. Own `docker.read`/`docker.write` permissions, log reads force-audited, `docker rm` never passing `--volumes`. Verified live against a **real dockerd** (`run-docker.sh`, in CI): four lifecycle verbs refused with 403 on a container the panel did not create **and that container still running afterwards**, the managed one obeying, logs/stats round-tripping, a flag-shaped name refused, and the action on the broker's hash chain. That e2e earned itself immediately — it caught a 30s stop grace running against hpd's 30s HTTP write timeout, which reported a *failure* for an action that had in fact succeeded. See [19 — Docker](19-docker.md).
- **Docker module — now complete.** On top of the read/lifecycle foundation: **creating containers**, where the hardening lives — the caller sends typed fields and the broker builds the argv, so `--privileged`/`--cap-add`/`--device`/host namespaces have no field at all, host bind mounts are *unrepresentable* (named volumes only, the pattern admits no `/`), every port publishes to `127.0.0.1` only (docker's firewall rules run ahead of the host's, so `0.0.0.0` would expose it), `no-new-privileges` is added, and environment travels by stdin env-file rather than argv because `/proc` is world-readable. **Volumes and networks** (list/create/remove, ownership-guarded, networks always bridge). **`exec` into a container**, reusing the web terminal's PTY and stream upgrade outright rather than a second mechanism — bounded by *which container* (the managed label) as the site terminal is bounded by *which user*, `docker.write`, audited but not transcribed. **Compose stacks** (up/down/ps/logs), framed honestly as an escape hatch: a compose file is arbitrary YAML the broker cannot harden, so it labels and scopes the stack (via a generated override, since `up` has no label flag) and writes the file to a broker-chosen path, but does not pretend to make arbitrary compose safe. Full UI: a top-level Docker page (containers/images/volumes/networks, create form, per-container logs and shell) and a site-level Docker tab. Verified live in `run-docker.sh` (63 assertions, in CI): a created container has no host mounts and publishes only to loopback, four host-path volumes are refused with no container created, an env secret never reaches the broker log, a shell in a foreign container is refused. That e2e also caught a real bug — a `compose ps` row silently dropped because a `Publishers` array was modelled as a string, which made a running stack list as empty.
- **Apps module — done.** Curated one-click templates deployed as labelled compose stacks (Ghost, Uptime Kuma, NocoDB, Vaultwarden, Gitea, Postgres, Redis, and a demo Nginx), each with a **memory-feasibility** verdict computed against the host's `MemAvailable` and shown *before* the operator commits — an app the host cannot run is marked, not discovered by the OOM killer. **Secrets are generated** with `crypto/rand`, never taken from input or defaulted in the template, returned once on success and kept out of the audit log. Every template publishes to loopback and keeps data in a volume that survives a redeploy. The module adds no privilege — an app *is* a compose stack, so it reuses that ownership boundary and the `docker.read`/`docker.write` permissions. UI: a catalog that disables an infeasible app with the reason, and a deploy wizard whose result screen shows the generated credentials once with a plain warning that they are not stored. See [19 — Docker](19-docker.md).
- **Exit criteria:** one-click deploy Ghost + Uptime Kuma, view live logs/stats, restart, and tear down cleanly — **met**. The app-deploy pipeline (feasibility → generated secrets → rendered compose → loopback-published stack → clean tear-down) is proven end-to-end against a real dockerd in CI.
- **The deferred set — now built.** The four items Phase 5 first shipped without are in, each keeping the module's two boundaries rather than working around them, and all proven live (`run-docker.sh` is now **80 assertions**). **Image removal and pruning:** images carry no per-panel label — the same base image backs a panel app and something run by hand — so ownership cannot apply; instead docker's own refusal to remove an image a container (running *or* stopped) still uses is passed straight through, and prune is dangling-only unless `all` is explicitly asked. **Volumes and networks, first-class:** `inspect` returns a volume's record *and the containers that mount it* (a live `ps`, because the daemon's view is the truth), and a network's connected containers; read-only and, like every read here, deliberately not ownership-gated. **Live log streaming:** the one-way twin of the container shell — the same connection upgrade, but output-only, `docker logs --follow` streamed until either side hangs up, `docker.read` and force-audited because logs carry secrets. **Reverse-proxy auto-wiring:** exposing an app on a domain creates a real **proxy site** whose vhost reverse-proxies to the app, so the app inherits the whole site machinery (domains, TLS, redirects, suspension) and is managed on the Sites page from then on; the upstream is resolved **live at render time** from the app's published loopback port (never baked in, so a redeploy on a new port is followed and a torn-down app falls back to a static wall), and port allocation reads docker's live set rather than a counter that reset on restart. Fixing this also surfaced and fixed a **pre-existing latent bug** in the shared site-delete path — the systemd-unit removal ran *after* the soft-delete and then could not resolve the now-hidden site, which the async job queue had been quietly swallowing on every proxy-site delete. See [19 — Docker](19-docker.md).

## Phase 6 — Monitoring & Backups
- **Monitor module — foundation (M1) done, in-core (satellite-ready).** Live **node** metrics (CPU/load/memory/swap/uptime/per-filesystem disk) with the module's organising principle in place: **subscription-gated sampling** — the browser subscribes over the realtime hub and the server pushes, but samples *only while at least one client is watching*, so an unattended panel does no metric work at all. Node numbers come from world-readable `/proc` + `statfs` read directly by hpd (no broker: there is no privilege to cross); the parsers are pure over fixtures, tested on any OS. The hub's local push needs no Redis, so live dashboards work on a Redis-less single node. `monitor.read` gates both the one-shot read and the `monitor:*` channels. Proven live in `run-docker`-style `run-monitor.sh` (in CI, with a hub-protocol `wsprobe`): the hub comes up without Redis, the sample reports sane memory/load/disk, an unauthenticated read is 401, and — the exit criterion — a subscriber **receives a pushed sample**, the cold one-shot's `cpu_percent: 0` becoming a real delta-computed figure the instant someone watches. See [20 — Monitoring](20-monitoring.md).
- **Monitor module — COMPLETE (M1–M4).** On the M1 foundation: **per-site metrics** read straight from each site's cgroup v2 accounting (`memory.current`, `cpu.stat` as a per-slice rate, `pids.current` — mode-0444 files, no broker; the payoff of Phase 1 turning accounting on), with `present:false` honesty for a slice that has not run; **service health** through the broker's read-only `service.status` (the read twin of `service.restart`, same allowlist — proven live with OpenLiteSpeed reporting `active` and the unstarted DB/cache `inactive`, on the audit chain); **history** — a raw sample a minute written *always* (not subscription-gated: a chart that skips unwatched hours lies), folded hourly by an idempotent Go-side rollup and pruned (raw ~48h, hourly ~30d), charted as single-series small multiples; and **alerts** — threshold rules that fire only after a breach persists `for_sec` (once per incident, resolving on recovery), notifying by webhook/Telegram/log with targets **sealed at rest and write-only**. Live proof (`run-monitor.sh`, in CI): live pushes for node and services over the hub, a breaching rule **firing its webhook into a local receiver** with the event recorded and the target never returned. See [20 — Monitoring](20-monitoring.md).
- **Scheduler — done, in-core.** Cron as **real systemd timers**: each job a `.timer` + `Type=oneshot` `.service`, which buys a real calendar, catch-up after downtime (`Persistent=true`) and the overlap policy free (a oneshot still running when its timer fires is not started again — no lock files). The safety story is absolute: every job is **site-scoped**, running as the site's unprivileged user in its home, inside its cgroup slice, with app-unit hardening — no API input produces a root cron. Schedules are charset-validated (`daily; rm -rf /` is a 400, not a quoted string), unit filenames are ULIDs, and logs are captured by the launcher into the site's own logs dir so they work without the journal. Live proof (`run-cron.sh`, in CI): units on disk with the site user and schedule, `cron.apply` audited, and a run-now job whose command was `id -un` **reading back `hps1`, not root** — the invariant observed from the job's own output. See [21 — Scheduler](21-scheduler.md).
- **Backup module — done, in-core (satellite-ready).** Full + **incremental** (GNU tar `--listed-incremental` — decades-old snapshot semantics, not a bespoke diff format), **zstd** via the system tar (zero Go deps), and **always sealed**: chunked AES-256-GCM (`pkg/blobcrypt`, STREAM construction — tamper/reorder/truncate/append all fail authentication with nothing written) under a purpose-derived subkey of the master key; no `HP_SECRET_KEY`, no backups — never plaintext at rest. The privileged half is three tiny broker verbs (tar in, tar out, delete a staged file); sealing, uploads, chains, scheduling and retention all live in unprivileged hpd. Targets: **local** and **S3-compatible** (AWS/R2/B2/MinIO) via ~200 lines of hand-rolled **SigV4** (the lean-deps rule; verified in tests by *recomputing* the signature server-side). Scheduling is a per-site policy (interval, target, keep_chains) swept hourly in-process; a new full retires chains beyond retention. **Restore goes into a NEW site** — the original keeps serving while the copy is verified, so a mistaken restore destroys nothing. Deleting a backup deletes its dependents explicitly, because a silently broken chain is the worst failure a backup system has. Live proof (`run-backup.sh`, in CI): at-rest file is blobcrypt ciphertext **unreadable as tar**, no plaintext staging survives, the incremental is a fraction of the full (372 vs 2707 bytes), the chain replays into a new site with the latest content owned by the new site's user, the original untouched, both capabilities audited. Honest gaps (docs/22): live-bucket S3 (the signer is fake-verified), DB-in-backup, panel self-backup, SFTP/OAuth drives. See [22 — Backups](22-backup.md).
- **Backup module — gaps closed.** The three documented gaps are built and live-proven (`run-backup.sh`): **live-bucket S3** — the same hand-rolled SigV4 client driven against a real **MinIO** in e2e (upload lands in the bucket and *not* on local disk, restore pulls from the bucket, delete empties it; hpd creates a missing bucket at boot, idempotently); **database-in-backup** — a site's policy names a panel database and every backup then carries its **full dump as a second sealed object** (a failed dump fails the whole backup — a backup that silently skips its database is a lie), restored on request into a **NEW database** beside the new site, original untouched, proven with a real MariaDB row round-trip; **panel self-backup** — the panel's own DB (SQLite `VACUUM INTO` live / MariaDB via `db.export`) sealed on the same pipeline, swept daily by default, with restore deliberately **out-of-band**: `hpd decrypt` opens any sealed object with nothing but the binary and `HP_SECRET_KEY` (a panel that needs its database back cannot be trusted to serve that request) — proven in e2e by decrypting an API-taken snapshot offline and reading the panel's own rows out of it. Still deferred by design: SFTP/OAuth-drive targets (dependency rule). See [22 — Backups](22-backup.md).
- **Exit criteria:** live dashboards with no idle polling — **met** (subscription-gated push, proven by wsprobe); scheduled encrypted incremental backup + successful restore into a new site — **met end-to-end on both targets**, local and a live S3 bucket (MinIO in e2e).

## Phase 7 — Email
- **Mail module (satellite)**: Postfix + Dovecot, domains/accounts/aliases/forwarders, DKIM/SPF/DMARC generation + DNS wiring, quotas, mail-queue view.
- **Exit criteria:** provision a mail domain with passing DKIM/SPF/DMARC and send/receive.

## Phase 8 — Security suite
- **Security module (satellite)**: firewall (nftables/ufw abstraction, CSF-compatible) with rollback-timer applies; Fail2Ban + optional CrowdSec; ModSecurity + OWASP CRS per site; ClamAV/maldet/rkhunter/lynis scans + quarantine; FIM; auto security updates; SSH hardening; geo/IP allow-block.
- **Panel auth hardening**: WebAuthn/passkeys GA, session management UI, IP allowlist for the panel, brute-force + rate-limit tuning.
- **Exit criteria:** malware scan quarantines a test EICAR file; a firewall change auto-reverts if not confirmed; WebAuthn login works.

## Phase 9 — Multi-user, API, Plugin marketplace, Polish
- **Multi-user/RBAC GA**: admin/reseller/developer/client + custom roles, granular permissions, reseller tenant scoping, impersonation (audited).
- **Public REST API GA** + OpenAPI docs site + API-key management + webhooks.
- **Module marketplace** (signed, third-party) built on the existing manifest/gRPC contract.
- **UX polish**: accessibility pass, i18n, performance budgets enforced, docs/help center.
- **Exit criteria:** a reseller manages an isolated tenant; a third-party signed module installs from the catalog; API + docs are complete.

## Phase 10 — Self-update, HA path, hardening & GA
- **Self-update** GA: channels (stable/beta/nightly), delta + signed updates, atomic swap, health-gated auto-rollback; independent module updates.
- **Multi-node readiness**: validate the agent-transport swap (broker/module gRPC over mTLS) on a two-node prototype; document the HA topology (Galera + Redis Sentinel + LB) — implementation optional/next-major.
- **Enterprise hardening**: full audit of systemd profiles, AppArmor/SELinux profiles, threat-model review, penetration test, load/perf tuning to hit the RAM/startup budgets.
- **Exit criteria:** in-place upgrade stable→stable with auto-rollback proven; all budgets met; security review passed → **1.0 GA**.

---

## Cross-cutting workstreams (continuous, every phase)
- **Testing**: unit + integration (testcontainers) + e2e (Playwright) + installer matrix; coverage gates.
- **Security**: threat-model deltas per module; broker capability review is mandatory for any new privileged op.
- **Performance**: track idle RAM (`hpd`+broker < 80 MB) and cold-start (< 1.5 s) as CI budgets.
- **Docs**: every module documented on merge; ADRs for every significant decision.
- **Multi-arch**: every release built + smoke-tested on amd64 + arm64 (and 386 where feasible).

## Suggested sequencing note
Phases are ordered by dependency and demo value, not rigid time. Phases 0–1 are the critical path; after Phase 1 the panel is genuinely useful and later modules are largely parallelizable by capability team, since each is an independent module behind the registry.

---
Back to [index](README.md).
