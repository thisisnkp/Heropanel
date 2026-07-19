# 16 — PHP (module design)

Phase 1, in-core. The per-site PHP surface: the version selector, a dedicated
FPM pool, FPM **sizing**, an allowlisted **php.ini editor**, **OPcache/JIT**, and
a per-version **extension manager**. Everything except extensions renders into
one file — the site's FPM pool — and is written and reloaded atomically through
the broker.

Builds on Sites (per-site user + home + pool socket) and the webserver renderer
(the vhost's fcgi handler points at the pool socket).

---

## 1. What is per-site and what is not

This distinction is the spine of the module, because PHP does not make it
obvious and getting it wrong produces settings that lie.

**Per-site** (a property of the pool file `/etc/php/<v>/fpm/pool.d/<user>.conf`):
- PHP **version** (which version's pool the site runs in).
- **FPM sizing**: `pm`, `pm.max_children`, and the manager-specific knobs.
- **memory_limit** (a first-class field; see §4).
- **php.ini overrides** that are `PHP_INI_ALL` or `PHP_INI_PERDIR` — set with
  `php_admin_value[...]` in the pool.
- **OPcache enable** and **JIT mode** — both `PHP_INI_ALL`.

**Per-version, never per-site** (a property of the FPM master, allocated once at
startup, before any pool is read):
- **Extensions.** The master loads them from `conf.d` when it execs. A pool may
  carry `php_admin_value[extension] = foo.so` and `php-fpm -t` will call the
  config *valid* — it is simply ignored. That combination is the trap: a
  per-site extension toggle would look like it worked, pass the config test, and
  do nothing. The manager is therefore version-scoped and says so (§5).
- OPcache's **shared-memory** knobs (`memory_consumption`, `jit_buffer_size`,
  `max_accelerated_files`). `PHP_INI_SYSTEM`: the master allocates that memory
  once. They are deliberately not columns — offering them per site would be a
  control that silently does nothing.

## 2. The pool file, and why order matters

`internal/php` renders the pool; the broker (`php.write_pool`) writes it and
reloads php-fpm. The template emits, in order:

1. identity + socket,
2. the process manager and only the directives that manager uses,
3. the operator's php.ini overrides,
4. OPcache,
5. **the panel's confinement** — `memory_limit`, `open_basedir`,
   `upload_tmp_dir`, `session.save_path`, `chdir`.

The confinement comes **last on purpose**. A pool file is last-one-wins, so even
if a directive somehow slipped past the allowlist, it cannot loosen
`open_basedir` or restore `disable_functions`: the panel's line is written after
it and wins. This is the second of two independent guards; the first is the
allowlist (§3).

## 3. The php.ini editor is an allowlist, not a text box

`ini_overrides` is a JSON object of allowlisted directives, not a free-text
php.ini. This is a security boundary, not a convenience:

- The pool file is *also* where a site is confined. A free-text editor able to
  write `open_basedir = /` would let whoever holds `site.write` on one site read
  every other customer on the box, and `disable_functions =` would hand back
  `exec()`. Those directives are **absent from the allowlist** and, per §2,
  re-asserted after any override regardless.
- Every value is **newline-checked**. A value is written as
  `php_admin_value[key] = value`; a newline in it does not make a bad setting, it
  makes *extra pool directives* — `256M\nuser = root` would hand the site's
  workers to root. This is the single most important check in the module.
- Values are also `;`/NUL-rejected (a `;` would comment out the confinement that
  follows) and **type/range-checked** per directive.

The allowlist and its bounds live in `internal/php/settings.go`; the UI reads the
settable keys from `GET /php` (`allowed_ini`) rather than hardcoding a list that
would drift.

## 4. FPM sizing is validated because php-fpm won't just warn

php-fpm does not merely warn about a bad pool — with `dynamic` and
`min_spare > max_spare`, or `start_servers` outside the spare range, the master
**refuses to start**. Because one master serves every site on a version, one
site's bad numbers take down *all* of them. So there are two guards:

- **hpd validates** the sizing and returns a field-level error naming the wrong
  number, before the broker is even called.
- **the broker config-tests** (`php-fpm -t`) before reloading, and rolls the
  pool file back if the test fails — the same reload-first discipline as
  `webserver.apply`. `php.write_pool` does the test; `php.set_extension` does too.

`memory_limit` is a first-class field rather than an allowlisted directive on
purpose: it already exists as `php_pools.memory_limit_mb`, two sources of truth
would have no answer to "which wins", and `memory_limit × pm_max_children` is the
ceiling on what one site can take from the node — a plan dimension, not a tweak.

## 5. The extension manager restarts, it does not reload

Enabling an extension for a version runs `phpenmod -s fpm` (FPM SAPI only, so a
web-server change does not alter what the site's own `php` CLI sees), config-tests,
and then **restarts** php-fpm — not reloads. An extension is linked into the
master at exec time; `SIGUSR2` re-reads pool config but will not add an extension
to a process already running. A reload here would report success and load
nothing. The restart briefly interrupts every site on the version, which is
inherent, not a shortcut, and the API says so with `scope_note`.

The enabled list is read from `fpm/conf.d`, **never** from `php -m` — that
reports the CLI SAPI, which has its own conf.d and would be confidently wrong
about the thing being asked.

RBAC: extensions are gated by `system.read` / `system.write`, not `site.*`. An
operation that restarts FPM for every site on a version must not be reachable by
a tenant who holds write on one site.

## 6. API

```
GET  /api/v1/sites/{uid}/php   site.read   → version, memory, FPM, ini, opcache, allowed_ini
PUT  /api/v1/sites/{uid}/php   site.write  → full replace of the above; write+test+reload

GET  /api/v1/php/extensions    system.read  → available, enabled, scope_note   (?version=)
POST /api/v1/php/extensions    system.write → enable/disable for a version; restarts FPM
```

`PUT /php` is a **full replace**, not a patch, because it maps onto a file
rewritten whole: an omitted field means "default", not "leave as-is". Anything
else would make the pool on disk depend on the order of past requests. A client
that wants to change one thing reads the current settings, edits, and sends the
envelope back — the OPcache object is a pointer in the handler for exactly this
reason (an absent object means "default", not "disabled").

## 7. Definition of Done

- [x] Domain (settings + validation) + service + repo (migration 0016)
- [x] Broker `php.write_pool` (now with config-test), `php.list_extensions`,
      `php.set_extension`, all with validation + rollback
- [x] REST + RBAC (`site.read/write` for the pool, `system.read/write` for
      extensions) + audit
- [x] Unit tests: newline-injection / confinement / allowlist, dynamic sizing
      rules, render-per-PM, stable render, OPcache, settings round-trip; broker
      argv + config-test rollback + CLI-SAPI trap
- [x] **Live e2e** (`deploy/docker/e2e/run-php-tuning.sh`, in CI) on real
      php-fpm: an ini override, memory_limit and an OPcache toggle observed in a
      served phpinfo; invalid sizing rejected while the site keeps serving;
      newline-injection and `open_basedir` refused; an extension disabled then
      re-enabled for the version, each observed in a served phpinfo after a real
      FPM restart.
- [ ] Frontend feature slice (the PHP/Runtime tab of the site workspace).

**Deferred:** the Debian/Ubuntu layout only (phpenmod/phpdismod over
mods-available). Rocky/Alma put a flat `/etc/php.d` in front of a single version
and need a different implementation. Per-version OPcache shared-memory tuning
(§1) awaits a version-settings surface distinct from the per-site pool.

---
Back to [index](README.md). Related: [12 — App Runtimes](12-app-runtimes.md),
[05 — Security](05-security-architecture.md) §3, [10 — Roadmap](10-roadmap.md) Phase 1.
