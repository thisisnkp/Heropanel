# 14 — Databases (module design)

MariaDB databases, users, and grants, plus the operations an operator actually
needs day to day: **size**, **export**, **import**, and a one-click hand-off to
Adminer/phpMyAdmin.

State lives in the control-plane datastore; every statement runs as root through
`hp-broker`, which authenticates to MariaDB over the local socket (`unix_socket`
auth), so no database password ever exists for the panel to store or leak.

Implemented in [internal/database](../internal/database), with capabilities in
[broker/capabilities](../broker/capabilities) (`db.*`).

## 1. Scope

**In:**
- Create/drop databases; create/drop users; grant/revoke privileges.
- **Size** (bytes + table count).
- **Export**: a gzipped `mysqldump`, streamed to the client and then deleted.
- **Import**: a streamed upload (plain or gzipped) loaded into a database.
- **Adminer/phpMyAdmin hand-off** signed in with a throwaway account (§4).

**Deferred:**
- PostgreSQL/MongoDB engines (`db_instances.engine` already carries the column).
- Per-database backups on a schedule — that belongs to the Backup module (Phase 6).
- Remote/multi-host database servers (everything here assumes the local socket).
- Table-level and column-level grants; the allowlist is database-scoped.

## 2. SQL safety

- **Statements go in over stdin, never argv.** `/proc/<pid>/cmdline` is
  world-readable, so a password on a command line is readable by every other site
  on the box.
- **Identifiers are allowlisted, not escaped.** `ValidateDBIdentifier` bounds
  database and user names to a conservative charset before they are interpolated
  into a statement, so there is no quoting to get wrong. Privileges are matched
  against `allowedPrivileges`; string values (passwords) go through
  `escapeSQLString`.
- **`REVOKE` is not `REVOKE IF EXISTS`.** That spelling is MySQL 8.0.16+ syntax
  and MariaDB — the engine HeroPanel targets — rejects it outright. The plain
  statement runs instead and MariaDB's `ER_NONEXISTING_GRANT` (1141) is forgiven:
  the caller asked for an end state that already holds, and failing there would
  break `revoke` in exactly the case where access is already gone.

## 3. Export and import

Dumps live in `/var/lib/heropanel/dumps` (`DumpDir`), owned by the **panel user**
and `0700`.

Neither direction passes through the broker. The broker's transport is
length-prefixed JSON over a Unix socket; a multi-gigabyte database would have to
be buffered whole and base64'd across it. Instead:

- **Export** — `mysqldump --result-file=<path>` writes straight to a file, `gzip`
  compresses it as a separate step (no shell, no pipe), and the result is chmod
  `0600` + chown'd to the panel user. hpd then streams the file and deletes it,
  however the request ends. The `0600` is the point, not a detail: `mysqldump`
  creates its output under root's umask (`0644`), which would leave one customer's
  entire database readable by every other site user on the box.
  `--single-transaction` keeps the dump from locking a live site out.
- **Import** — hpd streams the upload to a staging file (never buffering it), then
  the broker decompresses if needed and feeds it to the client with
  `SOURCE <path>`. The path is derived from a validated bare filename
  (`reDumpFile`: no slashes, no `..`), so nothing a caller sends can point `SOURCE`
  outside `DumpDir`. The staged file is removed either way — a customer's data
  must not be left lying around.

The dump directory belongs to the panel user rather than root because **hpd has
to traverse it** to stream an export back out; a `0700` root directory would lock
the panel out of its own export.

The panel user's name is broker **policy** (`PanelUser`, default `heropanel`),
not a constant: the account name is a deployment fact — a packager may install
under a different name, and a test harness runs hpd as root.

**Export requires `database.write`**, not `database.read`: it takes a full copy of
the data off the server.

## 4. Adminer / phpMyAdmin hand-off

**HeroPanel does not store database user passwords.**

It could. There is a perfectly good cipher in [pkg/secrets](../pkg/secrets), and
most panels do exactly this. But then one panel compromise hands over every
customer's standing database credentials, and there is no way to tell afterwards
which ones were used.

So a hand-off **mints a throwaway account** instead:

1. `POST /databases/{uid}/adminer-sso` creates `hpsso_<random>` with a random
   password, granted on **exactly one** database.
2. The credentials are returned once, for the browser to POST at Adminer's login
   form (`auth[driver|server|username|password|db]`). They are never persisted.
3. A row in `db_sso_sessions` records the account name and expiry — no secret.
4. A sweeper drops expired accounts every 5 minutes; sessions live 15 minutes.

The cost is a little machinery; the benefit is that a session's blast radius is
one database for fifteen minutes, and it is revocable and auditable.

Two guards worth naming:
- The sweeper **only ever drops accounts prefixed `hpsso_`**, even if a row names
  something else. It deletes MariaDB users; it must never become a weapon.
- A drop that fails **leaves the row behind** so the next sweep retries. Deleting
  the row would strand a live account with nothing tracking it.

Enabled by `database.adminer_url`. Unset, the endpoint reports "unavailable" —
there is nowhere to hand off to.

## 5. Data model (migrations 0004, 0013)

```
db_instances   id, uid, owner_id, engine, name, charset, status, created_at
db_users       id, uid, owner_id, engine, username, host, created_at
db_grants      id, db_user_id (FK), db_instance_id (FK), privileges
db_sso_sessions id, uid, db_instance_id (FK), username, created_at, expires_at
```

The panel's rows are a record of what it *asked for*; MariaDB is the source of
truth. Hence the ordering rule throughout: **the broker call comes first, the row
changes only after MariaDB agrees.** A record removed while the account still has
live access is worse than no record at all.

## 6. API surface

```
GET    /api/v1/databases                    database.read   → list
POST   /api/v1/databases                    database.write  → create
DELETE /api/v1/databases/{uid}              database.write  → drop
GET    /api/v1/databases/{uid}/size         database.read   → {bytes, tables}
GET    /api/v1/databases/{uid}/export       database.write  → gzip stream
POST   /api/v1/databases/{uid}/import       database.write  → body = dump
POST   /api/v1/databases/{uid}/grant        database.write  → grant
POST   /api/v1/databases/{uid}/revoke       database.write  → revoke
POST   /api/v1/databases/{uid}/adminer-sso  database.write  → one-time credentials
GET    /api/v1/database-users               database.read   → list
POST   /api/v1/database-users               database.write  → create
DELETE /api/v1/database-users/{uid}         database.write  → drop
```

Import sets `?filename=x.sql.gz` or `Content-Encoding: gzip` to declare
compression. The hand-off response is `Cache-Control: no-store` — it carries a
live password.

## 7. Broker capabilities

`db.create`, `db.drop`, `db.user.create`, `db.user.drop`, `db.grant`, `db.revoke`,
`db.size`, `db.export`, `db.import` — all on the policy allowlist, all audited
through the hash chain.

## 8. Definition of Done

- [x] Domain + service + repo interface (clean architecture)
- [x] Broker capabilities with identifier allowlists + stdin-only SQL
- [x] REST endpoints
- [x] RBAC scopes + audit coverage for every mutation ([15](15-audit.md)) —
      including `GET /export`, which changes nothing but hands over the whole
      database, and the Adminer hand-off, which records the throwaway account it
      minted. This box was checked long before anything wrote to `audit_log`.
- [x] Unit tests: service ordering (broker-then-record), capability argv/SQL,
      dump filename injection, sweeper safety
- [x] **Live e2e** (`deploy/docker/e2e/run-php-app.sh`, in CI) against real
      MariaDB: create db/user/grant, a PHP app reads a row through the grant,
      size reports bytes, export streams a verified gzip (and is cleaned up),
      import restores a deleted row, the hand-off account exists in
      `mysql.user` scoped to one database, and revoke + user-delete take effect
      in MariaDB.

---
Back to [index](README.md). Related: [05 — Security](05-security-architecture.md),
[11 — Git Deployments](11-git-deployments.md), [10 — Roadmap](10-roadmap.md) Phase 3.
