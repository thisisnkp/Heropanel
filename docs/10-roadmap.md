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

**Cryptographic signing — done.** On top of the SHA256SUMS checksum manifest, a
release now carries an **ed25519 signature over the manifest** (`SHA256SUMS.sig`).
When a release public key is pinned (`--pubkey` / `HP_RELEASE_PUBKEY`, and
install.sh forwards its pinned key), hp-installer verifies that signature before
it trusts a single hash — anchoring the whole checksum chain to a key held
offline, so an attacker who swaps a binary *and* its manifest still cannot forge
the signature. `hp-installer --gen-key` mints the keypair and `--sign` produces
the signature at release time; keys are accepted as base64/hex/`@path`. Verified
live (`run-installer.sh`): a fresh key signs the real manifest, a **tampered
manifest is refused** (`signature does not verify`, nothing installed), and the
signed manifest installs with `SHA256SUMS signature verified against the release
key` on the log — plus unit tests for signed-installs / unsigned-refused /
wrong-key / tamper / key encodings.

**Deferred from this phase (unchanged):** the public `install.sh` bootstrap
**hosting** at `get.heropanel.io` (the script itself is written and pins the key;
only the domain/CDN is external infra); a full *emulated* arm64 apt/dnf install
(the install logic is arch-agnostic and proven on amd64, so the arm64 check
smoke-tests the binaries rather than re-running the package manager under
emulation); and the "echo" demo job specifically — the async job + WS progress
path itself is exercised by the Phase 3 runtime suites (`run-nextjs.sh`).

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

**Rocky/Alma PHP layout — done.** A distro-aware resolver
([broker/capabilities/phpplatform.go](../broker/capabilities/phpplatform.go))
owns every path that differs between the two families, so the pool and extension
capabilities stay layout-agnostic: **Debian/Ubuntu** (Sury) uses `/etc/php/<v>/`,
`phpenmod`/`phpdismod` over `mods-available` symlinks, and a `php<v>-fpm` service;
**Rocky/Alma** uses the **Remi SCL** packages (the standard way to run *multiple*
PHP versions on RHEL — base `dnf module` gives exactly one) under
`/etc/opt/remi/php<vv>/` with a flat `php.d`, a `php<vv>-php-fpm` service, and no
phpenmod — so an extension is toggled by renaming its ini between `.ini` and
`.ini.disabled`. The family is detected from the host (phpenmod present ⇒ Debian).
Both layouts' exact argv + the RHEL enable/disable/missing/list paths are
unit-tested; the Debian path is live in `run-php-tuning.sh`. (Live Rocky-PHP e2e
needs a Remi-based image and is the one residual live gap.)

**Per-version OPcache tuning — done.** The `PHP_INI_SYSTEM` shared-memory
directives a pool cannot set (`opcache.memory_consumption`,
`interned_strings_buffer`, `max_accelerated_files`, `jit_buffer_size`,
`validate_timestamps`, `revalidate_freq`) now have a version-wide surface: hpd
renders and validates the ranges, broker `php.write_opcache` writes the pinned
`99-heropanel-opcache.ini`, config-tests, and **restarts** FPM (a fresh master
re-allocates the segment; a reload would not), rolling back on rejection;
`php.read_opcache` reads the effective values live (no DB cache, like SSH/updates
status). Gated by `system.write` and labelled in the UI as affecting every site
on the version. Verified live (`run-php-tuning.sh`): a raised
`memory_consumption`/`max_accelerated_files` is read back and **observed in a
served `phpinfo`** after the version-wide restart, and the change is audited.

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

**DNSSEC — done.** A per-zone toggle (migration 0033) renders `dnssec-policy
default` + `inline-signing yes` + a `key-directory` into the zone's named.conf
block; BIND generates the keys, signs the zone in memory (the plain zone file
hpd writes is untouched, so record edits keep working), and manages rollover.
Broker `dns.dnssec_status` reads the KSK/CSK from the key-directory and runs
`dnssec-dsfromkey` to return the **DS record for the registrar**. Verified live
(`run-dns.sh`): enabling signs the zone (a real DNSKEY 257 answers `dig`), the
API returns a real DS (`example.test. IN DS 7241 13 2 …`), and the state is
audited.

**Zone import/export — done.** Export renders a zone as a standard RFC 1035
master file (text/plain download); import parses a pasted master file — a
tolerant parser handling `$ORIGIN`/`$TTL`, owner inheritance, optional TTL/class,
MX/SRV priority split and TXT unquoting — and **adds** the records (the SOA and
apex NS stay the panel's), validating each and reporting skips rather than
failing the whole import. Verified live (`run-dns.sh`): a zone exports with its
SOA + records, and a pasted master file imports 3 records (a `DNSKEY` line
skipped as unsupported) that then answer `dig`.

**ZeroSSL — done.** ZeroSSL is a standard RFC 8555 CA that additionally requires
**External Account Binding** (a KID + HMAC key from the operator's dashboard); the
existing ACME issuer gained an EAB option (`NewACMEIssuer`, HMAC accepted as
base64url/base64) that binds the account at registration, and the SSL service now
carries per-provider issuers (`WithIssuer`) selected by a `provider` field on the
issue request — Let's Encrypt stays the default, ZeroSSL is enabled when EAB
credentials are configured (KID in config, HMAC key in the **secret env** only).
Renewal repeats with the same provider. Verified live (`run-zerossl.sh`): against
**Pebble configured to require EAB**, an EAB-bound account registers, HTTP-01
validates, and a certificate is issued and recorded under the `zerossl` provider,
its leaf **signed by the Pebble CA** — Pebble would reject a missing or wrong
EAB, so registration succeeding is the proof the binding is correct.

**Deferred:** DNS as a true *satellite* module (needs the [06](06-plugin-architecture.md)
registry/gRPC — DNS is in-core for now; a Phase 9/10 re-architecture). (Live ACME
against a real CA is verified for both Let's Encrypt — `run-acme.sh` — and ZeroSSL
EAB — `run-zerossl.sh`.)

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

**PostgreSQL engine — done.** A second managed database engine alongside MariaDB.
The database service is now engine-aware (`brokerCap(engine, op)` routes to `pg.*`
vs `db.*`); PostgreSQL admin runs as the **postgres OS user** via
`runuser -u postgres -- psql` with SQL on stdin (peer auth, no password in argv),
identifiers strictly validated + double-quoted and password literals escaped for
`standard_conforming_strings`. Nine capabilities — create/drop, role create
(a create-or-update DO block)/drop, grant/revoke (CONNECT + schema/table + default
privileges, run inside the target DB), size (`pg_database_size`), and
export/import (pg_dump to a postgres-owned staging dir, then root gzips and hands
it to the panel; import copies back the other way). A grant across engines is
refused. Broker capabilities are argv/SQL-exact unit-tested and service routing is
unit-tested; live e2e is `run-postgres.sh` (real PostgreSQL: create → role →
grant, then a row round-trips through export→import) — its Docker image adds the
`postgresql` package. **Mongo stays out of scope by decision** (Postgres covers
the "another engine" intent; Mongo's non-relational model is a separate module).

**Rotating data keys — done.** The `hp1` sealed-column format reserved room for an
envelope; it is now built ([pkg/secrets/keyring.go](../pkg/secrets/keyring.go)).
A panel can hold a **keyring** of random data keys, each *wrapped* (sealed) under
the master and stored in `data_keys` (migration 0034); the active generation
seals new values as `hp2.<gen>.<blob>`, older generations keep opening existing
values, and a panel that never rotates stays on legacy `hp1` — fully backward
compatible. **Data-key rotation** (`POST /system/keyring/rotate`, system.write)
mints a new active generation; **master rotation** re-wraps the handful of data
keys under a new master (`Rewrap`) without touching the many row blobs — the
whole point of the envelope. AAD binding still applies throughout. Comprehensive
unit tests (seal/open across generations, persist+reload, rewrap-under-new-master,
reseal migration, AAD enforcement) plus a keyring service test through a real DB;
verified live (`run-keyring.sh`): rotate → generation 1 persisted → **hpd
restarted → keyring reloaded from the DB → still generation 1**, audited.

**Deferred:** OAuth app authorization (paste a token instead); php.ini / extension management
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
- **Session recording/playback** — **done**. Every terminal session is recorded as **asciicast v2** (the asciinema format, so it outlives this software) and replayed in-panel through xterm.js — the same emulator the live terminal uses, so no second player library ships. Output *and* keystrokes are captured, kept 30 days, swept hourly, and readable only with `terminal.recordings.read`; deleting needs a separate `terminal.recordings.delete`, because destroying an audit artifact is what an operator under scrutiny would most want. Downloading one is force-audited, and the terminal says it is recorded *before* the session starts. **Passwords are never stored:** input typed at a password prompt is replaced with a single `[redacted]` marker (one per run, not per keystroke — the count would leak the length). The discriminator is the subtle part and the live e2e is what found it: "redact while ECHO is off" is *wrong*, because readline runs with ECHO off nearly all the time and that rule redacted every command anyone typed. A real password prompt is ECHO off **while still canonical** (`ICANON` set) — `read -s` and getpass keep the kernel's line discipline and just silence it, whereas readline clears both. Only the broker can see that state, so it reports it over a `StreamEcho` control frame. Honest limit: this redacts *input*; a program that prints a secret to the terminal itself is still recorded. Recordings are a **top-level page** listing every session across every site, beside the audit log — not a sub-view of the terminal. That was a fix, not the first design: the list originally lived only inside the Terminal tab, which is gated on `terminal.use`, so the exact role the separate permission was created for — an auditor with `terminal.recordings.read` and no shell — could not open a single recording. Every backend test passed, because the permission was correct in the API and wrong in the navigation; `web/e2e/recordings.spec.ts` now pins it, and a repository test pins the site join the cross-site list is unreadable without. **Server-side recording search — done.** The Recordings page now searches across
*all* history on the server (`?q=` on the listing, matched case-insensitively as a
substring of the actor email, system user, actor IP, or site name; optional
`after`/`before` date bounds), so "nothing matches" is a real answer instead of
"not in the newest 200". Repo-tested (search by email/site/IP, empty-match, and
site scoping) and wired into the page, which drops the "this page only" caveat
when a query is active. *(Terminal reattach / multiple concurrent tabs — see the
Phase 4 tail below.)* See [18 — Web Terminal](18-web-terminal.md).
- **Browser e2e (Playwright)** — **done**: a Chromium suite driving the **real hpd serving the real bundle**, covering routing, first-run setup, sign-in, deep-link reload, the command palette, and navigation. It deliberately stops at privileged behaviour — file ops as the site user, a PTY, recording — which needs a root broker and real Linux accounts and stays in the container e2e. Wired into CI with a report artifact on failure. It immediately earned itself: it caught that *any* `/auth/status` failure (a 429 from the rate limiter, a 500) rendered the "panel is not configured" screen, sending an operator to fix a database setting that was never the problem — now only a 404 counts as that evidence.
- **Full gitignore semantics — done.** The matcher
  ([web/src/features/sites/gitignore.ts](../web/src/features/sites/gitignore.ts))
  now covers git's working-tree rules: on top of nesting/precedence and the
  `*`/`**`/`?` wildcards it handles **character classes** (`[abc]`, `[a-z]`,
  `[!abc]` negation, a `/`-never-in-a-class rule, unterminated-bracket-as-literal)
  and the two lower-precedence sources git also consults — **`.git/info/exclude`**
  and the **global excludesfile** (`core.excludesfile`) — layered under the
  .gitignore chain in git's exact order (global < info/exclude < .gitignore, deep
  wins). It is still a display hint (git sites are anyway blocked from the file
  manager), but what it matches is now what git would hide. 22 vitest cases pin it.
- **Archived multi-file upload — done.** A **multi-file upload as a single
  transfer**: the client sends one `.zip`/`.tar.gz` to
  `POST /sites/{uid}/files/upload-archive?path=<dir>`, and hpd stages it under an
  unpredictable temp name, extracts it into the directory (creating it if needed),
  and deletes the staged archive whether the extract succeeds or fails — so the
  tree never accumulates upload leftovers. The FilesTab gains an "Upload archive &
  extract…" action. Unit-tested (mkdir → stage → extract → remove sequence, and a
  non-archive filename is refused) and verified live (`run-files.sh`): a tar.gz
  bundle extracts its whole nested tree in one request, owned by the site user,
  with **no `.hp-upload-*` archive left behind**.
- **Still deferred out of Phase 4 (honest list):** **true streaming archive
  *download*** — folder download no longer leaves anything behind (cleanup runs on
  every path out), but it still *materialises* the archive because the broker's
  exec buffers stdout; eliminating the transient file entirely needs a broker
  **output-stream** (the same connection-upgrade the terminal PTY and
  `docker logs --follow` use, applied to `tar czf -`), which is transport work
  grouped with the gRPC-satellite items.
- **Terminal idle-timeout + concurrent tabs — done; reattach reclassified.** An
  **idle timeout** (`HP_TERMINAL_IDLE_TIMEOUT`, 0 = off) closes a session gone
  quiet in *both* directions for the window — a long-running command keeps it
  alive because its output counts as activity, so only a genuinely idle shell is
  killed, bounding the standing risk of a forgotten terminal on a customer's site.
  **Multiple concurrent tabs already work**: each terminal WebSocket opens its own
  broker PTY stream, so nothing server-side limits a site to one — opening a second
  tab just opens a second PTY. **Reattach is reclassified deferred-by-design**: it
  requires the broker to *keep a PTY alive across a disconnect* and re-bind a new
  socket to it, which directly contradicts the deliberate security property that a
  disconnect kills the whole process group (so a dropped connection can never
  leave a root-adjacent shell running unattended). Delivering it means broker-side
  session persistence — a Phase 9/10 transport change weighed against that
  security property, not a backfill. Frontend testing is now unit (`vitest`) **and** browser (`Playwright`); what remains uncovered is privileged UI behaviour, which the container e2e proves at the API level instead of through the DOM.
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
- **Scheduler fires live (the timer gap, now closed).** The in-process sweeper now **sweeps once at startup** (a backup that came due while hpd was down runs promptly, not up to a tick later) and its interval is configurable (`HP_BACKUP_SWEEP_INTERVAL_SEC`, default one hour — small values for e2e only; due-ness is still each policy's `interval_hours`). Verified live (`run-backup.sh`): a fresh site with an enabled policy and no prior backup gets one from the **sweeper alone, with no `POST /backups`**, and hpd logs it completed. A not-due policy is left untouched (unit test).
- **Backup SFTP target — done (Tier 1).** An off-cloud SSH copy via a **hand-rolled minimal SFTP-v3 client** (`internal/backup/sftp.go`) over `x/crypto/ssh` — no third-party SFTP dependency, the same lean-deps posture as the SigV4 signer and WebAuthn verifier. Credentials come from the secret env; the server host key is **pinned** (and its algorithm forced). Verified live (`run-backup.sh`, real openssh in e2e): a sealed backup is **written over SFTP**, ciphertext at rest, **not** on local disk; restore **fetches it back over SFTP** and replays into a new site; delete removes it. *(OAuth-drive targets remain deferred.)*
- **rclone backup target (cloud drives) — done (Tier 2, per decision).** Reaches rclone's 70+ backends (GDrive/Dropbox/OneDrive/…) via an operator-configured remote — **no OAuth code or provider SDK in hpd**. A backup Target that execs `rclone rcat`/`cat`/`deletefile` (`internal/backup/rclone.go`), streaming already-sealed blobs. Config `HP_BACKUP_RCLONE_REMOTE`/`_CONFIG`. Verified live (`run-backup.sh`, rclone `:local:` backend): sealed backup **streamed to the remote**, ciphertext at rest, **not** on local disk; restore **fetches it back via rclone**; delete removes it. *(Native per-provider OAuth stays out of scope by design — this is the lean answer chosen for "OAuth drive targets".)*

## Phase 7 — Email
- **Mail module — done, in-core (satellite-ready).** Postfix + Dovecot driven by **rendered flat maps** (virtual domains/mailboxes/aliases + a Dovecot passwd-file) — the MTAs never read the panel's database, so mail keeps flowing when the panel is down; every change re-renders the complete state and applies through the broker with rollback (the `webserver.apply` discipline). Mailbox passwords are **`{BLF-CRYPT}` write-only** both directions; delivery is LMTP into vmail-owned Maildirs with Dovecot-enforced per-mailbox quotas (read back live via `doveadm`); **suspension blocks logins but keeps receiving** (bouncing a suspended user's mail would be data loss). **DKIM** keys are generated in hpd (RSA-2048) and **sealed with the panel data key before they touch the database** — unsealed only to hand OpenDKIM its 0600 key file; MX/SPF/DKIM/DMARC **auto-wire into panel-managed zones** (SPF appends at the apex rather than clobbering operator TXT records; long DKIM TXT values split into RFC 1035 character-strings) and a **live DNS check** resolves each record against real DNS. The queue is `postqueue -j` parsed over fixtures, flush, and **explicit-ID-only** deletes (no delete-ALL by design). Mail carries its own `mail.read`/`mail.write` permission pair — site.write must not reach mailboxes. Live proof (`run-mail.sh`, in CI, 34 assertions): one API call provisions host+domain with the DKIM key **ciphertext at rest** and its DNS `p=` **byte-identical to the private key's public half**; a real SMTP message lands in the Maildir **DKIM-signed** and IMAP reads it back against the BLF-CRYPT credential; an alias hops; a suspended mailbox refuses login while mail still lands; a genuinely deferred message shows in the queue and is deleted by ID; every capability audited. See [23 — Mail](23-mail.md).
- **Exit criteria:** provision a mail domain with passing DKIM/SPF/DMARC and send/receive — **met**: all four records resolve from live DNS with the key-pair correspondence proven, and a message is sent (SMTP), delivered (LMTP, DKIM-signed) and received (IMAP) end to end.
- **Mail TLS — done (the Phase 8 deferral, now closed).** The mail host presents **one** certificate — its own FQDN (`HP_MAIL_HOSTNAME`, e.g. `mail.example.com`), not a per-domain cert, because a client connects to the *mail host*, not to each hosted domain. hpd delegates to the SSL module to make sure that certificate is installed (a real Let's Encrypt cert when the operator has issued one for the host, a self-signed fallback otherwise — TLS out of the box), then the broker's `mail.tls` capability wires it into Postfix (main.cf cert + the submission/smtps services via idempotent `postconf -M`/`-P`, both **authenticated-relay-only** so it can never become an open relay) and Dovecot (an `ssl = required` drop-in + the postfix-private SASL socket). Opens **submission/587 (STARTTLS+AUTH)**, **imaps/993** and **smtps/465**. Verified live (`run-mail.sh`): `mail.tls` and `cert.install` on the broker's audit chain; `openssl s_client` completes STARTTLS on 587 and implicit TLS on 993 against the host cert; an **authenticated** submission over 587 is delivered end-to-end, and an **unauthenticated** relay to an external domain is **refused**. *(Still deferred: inbound verification policy — Phase 8 §Tier-2.)*
- **Webmail (Roundcube) — done.** Served by the panel's own OpenLiteSpeed + PHP against the **local** Dovecot/Postfix over TLS, as a **system vhost** on `HP_WEBMAIL_HOSTNAME` with a dedicated `webmail` user and FPM pool. One call (`POST /webmail/install`) lays down the runtime — broker `webmail.install` (user, sqlite metadata db that Roundcube self-initialises, rendered `config.inc.php` pointing at `tls://127.0.0.1:143`/`:587`), then the same `php.write_pool` every site uses, then a web-server re-apply. **No mailbox password is handled** — Roundcube authenticates each user against Dovecot at login. Verified live (`run-webmail.sh`): OpenLiteSpeed serves Roundcube's login page, a **real mailbox user logs in through Roundcube** (IMAP auth against Dovecot over TLS, the full OLS→PHP→Roundcube→Dovecot chain) and a **wrong password is refused**; the three capabilities are audited. **Passwordless SSO — done:** the panel mints a **one-time Dovecot master credential** (a random per-session master user + a random password, bound to exactly one mailbox, expiring in minutes) and hands off `mailbox*master` + that one-time password for the browser to POST at Roundcube's login form — Dovecot's master passdb (`auth_master_user_separator = *`, an inert-until-populated master passwd-file in the drop-in) then logs the user in AS the mailbox **without its own password**. Same shape as the database sign-on hand-off: declarative render-all of the live sessions, single-target, swept on a TTL; the panel stores only the bcrypt hash, never the plaintext. Own `POST /mail/accounts/{uid}/webmail-sso`, a per-mailbox "Webmail" button, and the sweeper. Verified live (`run-mail.sh`): a hand-off is `mailbox*master`, the master file carries the one-time user, an **IMAP login with only the master credential reads the mailbox** (no mailbox password), a wrong master password is refused, and the stored row holds a `{BLF-CRYPT}` hash rather than the one-time password.

## Phase 8 — Security suite
- **Security module — core done, in-core (satellite-ready).** The three exit criteria and the supporting hardening are built and verified. **Firewall** (nftables): an apply is **two-phase and self-reverting** — `firewall.apply` snapshots the live ruleset, applies the rendered one, and arms a revert that fires unless `firewall.confirm` lands before the deadline, so a rule that locks the operator out undoes itself. The timer lives in unprivileged hpd (local to the box, so it fires even when the change cut off *remote* access), persists its deadline (survives an hpd restart), refuses a stale confirm, and renders default-drop while always keeping established/related + loopback. **Malware** (ClamAV): `malware.scan` over a confined site tree; `malware.quarantine` **moves** a detection into a root-only 0600 area where it can neither be served nor run, restore/delete for the false-positive path — the quarantine target is validated to lie within the named site. **Passkeys (WebAuthn)**: a **hand-rolled, dependency-free** verifier (stdlib crypto + a ~200-line CBOR reader) that checks the assertion signature (ES256/RS256), challenge, origin, RP-ID binding, user presence and signature-counter clone detection; attestation is deliberately not verified (documented posture); registration while signed-in, passwordless login. **Panel IP-allowlist** (application-layer, after RealIP, defence-in-depth with the firewall) and **Fail2Ban** surfacing (jails/banned view + ban/unban) round it out. Own `security.read`/`security.write` permission pair. Live proof (`run-security.sh`, in CI): an applied firewall rule is live in `nft`, then left unconfirmed **auto-reverts** past its window, and applied+confirmed it **sticks**; a **real clamscan** finds the EICAR test file (one-line custom signature, no DB download) and quarantine **removes it from the site** to root-only 0600 where the site user cannot read it, then restores it — every capability audited. WebAuthn is proven by Go integration tests (a virtual authenticator drives a full register→passwordless-login: pure crypto, so a Go test is the honest end-to-end, not a container — round trip plus rejection of tampered signature/replayed challenge/wrong origin/rolled-back counter/wrong key). See [24 — Security](24-security.md).
- **Exit criteria:** malware scan quarantines a test EICAR file — **met** (live); a firewall change auto-reverts if not confirmed — **met** (live); WebAuthn login works — **met** (register→passwordless login through the service, virtual-authenticator integration test).
- **Deferred (a security suite is never "finished"):** ModSecurity + OWASP CRS per site;  SSH hardening; automatic security updates; geo/IP allow-block lists; file-integrity monitor (FIM); `rkhunter`/`lynis`/`maldet` alongside ClamAV; session-management UI; IPv6 firewall source rules + port ranges. Each is a substantial feature sequenced as a follow-up rather than half-built.
- **IPv6 firewall rules + port ranges — done (Tier 2).** The nftables table was already `inet` (dual-stack); rules now accept an **IPv6** source (rendered on `ip6 saddr` vs `ip saddr` by address family) and a **port range** (`port_end`, migration 0031, rendered as `dport start-end`). Validation accepts v6 addresses/CIDRs and rejects an inverted or protocol-less range. Verified live (`run-security.sh`): an IPv6 rule and a `8000-9000` range both render into the live confirmed nft ruleset.
- **Inbound mail verification policy — done (Tier 2).** DKIM is already verified inbound (OpenDKIM `Mode sv` stamps Authentication-Results); on top, a 3-level policy (off/standard/strict) applies postfix HELO/sender/recipient restrictions via broker `mail.inbound` (local submission exempt — `permit_mynetworks`/`permit_sasl_authenticated` first). Verified live (`run-mail.sh`): with `standard`, a **forged/unknown sender domain is rejected** while a resolvable sender delivers; `off` lifts it. **Full SPF/DMARC rejection with alignment — done:** an `off`/`monitor`/`enforce` posture (`POST /mail/authverify`) wires two real daemons — **policyd-spf** (a postfix policy service, into `smtpd_recipient_restrictions`) and **OpenDMARC** (a milter evaluating SPF/DKIM **alignment**, into `smtpd_milters` ordered AFTER OpenDKIM so a DKIM pass is visible). Because both integration points are surfaces other capabilities own (the inbound level and DKIM), the broker composes them **read-modify-write** via a pure, unit-tested token helper — inserting/removing only its own entry, never clobbering the DKIM milter or the inbound restrictions. `enforce` rejects a hard SPF fail and a DMARC failure the sender's own policy asks to reject; `monitor` evaluates and stamps results without rejecting; local/authenticated submission is always exempt (PermError/TempError never bounce good mail). Verified live (`run-mail.sh`, real policyd-spf + OpenDMARC): enforce wires the DMARC milter alongside DKIM in the right order, registers the policyd-spf master.cf service + recipient restriction, runs OpenDMARC in reject mode; turning it **off** removes both **while preserving the DKIM milter** (proving the composition), and every capability is audited.
- **Geo/IP allow-block lists — done (Tier 2).** CIDR allow/block entries (migration 0032) rendered as **nftables interval sets** (`hp_allow4/6`, `hp_block4/6`) evaluated ahead of the ordinary rules — allow always let in, block always dropped — one set-membership test covering thousands of ranges (a country block is a bulk CIDR import). Goes through the same firewall apply/confirm/auto-revert machinery. Verified live (`run-security.sh`): a block CIDR renders into the live ruleset as a set + `ip saddr @hp_block4 drop`, with the allow set accepted first. **Automatic country import — done:** `POST /firewall/countries` bulk-fetches a country's published CIDR ranges (`country` column, migration 0035) from a **configured** geo source (`HP_SECURITY_GEODB_URL`/`_URL6`, defaulting to the ipdeny aggregated mirrors — an operator can point at their own copy so the panel's only outbound reach is one they chose), parses/de-duplicates them into the same block/allow set, and manages them as a unit (re-import replaces, `DELETE /firewall/countries/{cc}` removes); a country's thousands of ranges are never listed individually. A missing address-family file (a country with only v4) is tolerated rather than failing the import. Nothing is fetched unless an import is explicitly requested, and — like every list change — it is never applied as a side effect. Verified live (`run-security.sh`, against a local mirror): an import stores the de-duplicated v4+v6 ranges, they render into `hp_block4/6` after apply+confirm, and removing the country clears them.
- **CrowdSec — intentionally NOT built (Tier 2, per decision).** Fail2Ban (Phase 8) already provides the intrusion-prevention baseline and CrowdSec overlaps it — they are an either/or, not additive. Left out on purpose rather than shipped as a redundant second IPS; can be added later behind the same `cscli`-surfacing pattern Fail2Ban uses if a deployment prefers it.
- **File-integrity monitoring (AIDE) — done (Tier 2).** A baseline of the panel's security-critical paths (configs, `/etc/ssh`, the daemons) built by broker `fim.init`; `fim.check` compares the filesystem against it and reports added/removed/changed. Shells out to real AIDE (fixed argv) and parses its summary — the parser is careful to ignore AIDE's bare detail-section headers that repeat the count labels without a number. Verified live (`run-fim.sh`, real AIDE): a check without a baseline is refused (409); after init the check is **clean**; **tampering a watched file makes the next check report `changed=true`** with a non-zero count; all three capabilities audited. **Host-wide FIM — done:** `fim.init` takes a `scope` — `panel` (the default, the panel's own security-critical set) or `host`, which extends the watch to all of `/etc` and the system binary/library trees (`/bin`,`/sbin`,`/usr/bin`,`/usr/sbin`,`/lib`,`/lib64`,`/boot`) with `!` exclusions for the paths that legitimately churn (mounts, clock, machine identity, the panel's own state). The chosen scope is **recorded** so a later check renders the identically-scoped config (a mismatch would report the whole extra tree as spuriously changed) and the AIDE timeout scales with it. Verified live (`run-fim.sh`): a host baseline reports `scope=host`, its fresh check is clean, and a change under `/etc` is then detected `changed=true`.
- **Host audit scanners (rkhunter, lynis) — done (Tier 2).** `audit.scan` runs **rkhunter** (rootkit hunter) or **lynis** (system auditor) and parses the output — rkhunter's warning count, lynis's hardening index + warning/suggestion counts. `maldet` deliberately skipped (overlaps ClamAV). Scans take minutes, so the broker-client timeout + `HP_SERVER_WRITE_TIMEOUT` (new env) are raised. Verified live (`run-fim.sh`, real tools): lynis returns a parsed hardening index, rkhunter returns its warning count, an unknown tool is refused (400), `audit.scan` audited.
- **SSH hardening — done (Tier 1).** A panel-owned sshd drop-in rendered by hpd from a fixed, validated field set (port, root-login policy, password auth default-off/key-only, pubkey auth, auth-try budget, optional allow-list) plus a block of fixed hardening (empty passwords off, X11/agent forwarding off, tight grace/client-alive). Broker `ssh.harden` writes the one pinned path, **config-tests with `sshd -t` before it can take effect**, and **reloads (not restarts)** so the live session survives; a rejected config rolls back and never reaches a reload. A **self-lockout (both auth methods off) is refused** (400). `ssh.status` reads effective config with `sshd -T`. Verified live (`run-security.sh`): `sshd -T` shows the new port, `PermitRootLogin no`, `PasswordAuthentication no`, `PermitEmptyPasswords no`; the self-lockout is refused; both capabilities audited.
- **Automatic security updates — done (Tier 1, both distro families).** A panel-owned auto-update policy rendered by hpd from validated options (enable, security-origin-only, auto-reboot + time). On **Debian/Ubuntu** it is an `unattended-upgrades` apt drop-in: broker `updates.configure` writes the pinned path, **validates with `apt-config dump`** (a malformed apt.conf rolls back before wedging apt), and enables the apt timers; `updates.status` reads the effective merged config back with `apt-config dump`. On **Rocky/Alma** the same options render a `dnf-automatic` INI (`upgrade_type security|default`, `apply_updates`, `reboot when-needed|never`): the broker **detects the family** (apt-config present ⇒ Debian, else RHEL — the same distro-detection shape the PHP capabilities use), writes `/etc/dnf/automatic.conf`, structurally validates the INI, and enables the `dnf-automatic.timer`; status reads `apply_updates` back, mapped to the same effective 1/0 so one UI reads both families. hpd renders both configs (pure, testable) and the broker applies whichever the host uses. Verified live on Debian (`run-security.sh`): `apt-config dump` shows `Unattended-Upgrade "1"` in effect, disabling flips it to `"0"`, both capabilities audited; the RHEL/dnf branch is unit-tested (config render + the broker's family dispatch, write and timer-enable) since the e2e image is Debian-based.
- **Session management — done (Tier 1).** A signed-in user sees their own active sessions (one per login/device, with IP, user-agent and timestamps; the current one flagged) and can revoke any of them or **sign out everywhere else**. Revocation is scoped by `user_id` so no one can cut another user's session; the account page hangs off the top-bar identity. Pure hpd/DB (no broker), covered by a repository test (list/revoke/revoke-others + cross-user isolation) and a **full-stack httpapi e2e** (two logins → both visible → revoke-others → the revoked cookie is dead, the current one still works). Honest note: a revoked session whose principal is already cached stays usable until the 30s principal-cache TTL expires; the caller's own session is evicted immediately.
- **Per-site WAF (ModSecurity + OWASP CRS) — done (Tier 1).** A `waf_enabled` toggle (migration 0030) renders a `module mod_security` block into the site's OLS vhost (plus a server-level `ls_enabled 1` when any site has it on, which OLS requires to activate the module); the broker `waf.provision` writes the pinned rules file. Shaped for **libmodsecurity v3** (Include-only, base engine inline, CRS by concrete pieces — the distro `owasp-crs.load` uses v2-only `IncludeOptional` and cannot be included verbatim). Verified live (`run-waf.sh`, real OLS ModSecurity + CRS): with the WAF **off** a SQLi probe is allowed (200); with it **on** the same request is **blocked (403)** while normal traffic serves (200); disabling allows it again. `waf.provision` audited. *(Migration count now 30.)*

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
