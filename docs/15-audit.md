# 15 — Audit Log (module design)

Phase 0, in-core. The tamper-evident record of **who did what, to what, from
where, with what outcome, and when** — the panel-side half of the accountability
story whose privileged half lives in the broker's own chain.

Implements [05 §9](05-security-architecture.md). Table `audit_log` (migration
0001) has existed since Phase 0; **this module is what finally writes to it.**

---

## 1. Why it was rebuilt

The schema, the `audit.read` permission, and three modules' Definition-of-Done
checkboxes claiming "RBAC scopes + audit coverage for every mutation" all shipped
before anything wrote a single row. Nothing was auditing anything. The lesson is
baked into the design below: **the coverage rule is enforced by construction, not
by remembering.**

## 2. Scope

**In:**
- `internal/audit`: the chain (hash, link, verify), the `Service`, and the
  request-scoped `Annotation`.
- `internal/repository`: `AuditRepository` — append, head, filtered list.
- `internal/httpapi`: the `auditor` middleware, `GET /audit`, `GET /audit/verify`.

**Deferred (documented):**
- Export to an external append-only sink / SIEM. Until the head is witnessed
  somewhere the panel cannot reach, the chain proves tampering only to someone
  who has a copy of an earlier head (§4).
- A scheduled verifier that alerts on a break (belongs with Monitor, Phase 6).
- Per-worker audit rows for async jobs (§6).
- Retention/rotation. The table only grows; listings are paged, but nothing
  prunes yet.

## 3. The chain

Per [05 §9](05-security-architecture.md):

```
row_hash = SHA-256(prev_hash ‖ canonical(row))
canonical(row) = uid ␟ created_at ␟ actor_user_id ␟ actor_ip ␟ actor_kind
                 ␟ action ␟ resource_type ␟ resource_id ␟ outcome ␟ detail
```

- The separator is the ASCII **unit separator** (`0x1f`), which cannot occur in
  any of these values — so no field can be shifted into its neighbour to forge a
  row that hashes the same.
- `Verify` walks the table from the first row and reports the first entry whose
  contents disagree with its `row_hash`, or whose `prev_hash` does not match its
  predecessor. Editing a row, or deleting one, breaks every link after it.
- **Appends are serialized in-process** (a mutex in `Service`). The chain is a
  linked list; two writers reading the same head would fork it permanently. This
  is correct for the single `hpd` HeroPanel runs today; the multi-node HA path
  (Phase 10) needs the head claimed in the database (`SELECT … FOR UPDATE`), and
  the code says so at the mutex.
- **A failed insert does not advance the head.** Otherwise the next row would
  point at a predecessor that does not exist and `Verify` would blame an innocent
  record for someone else's outage.

### 3.1 The hash covers the row *as the database returns it*

This is the subtle part, and it is engine-specific:

- **`created_at` is written with microsecond precision** (`…15.123456`), because
  MariaDB types the column `DATETIME(6)` and always renders six fractional
  digits. Writing the repository's second-precision layout would mean every row
  came back one `.000000` longer than it went in — and `Verify` would fail on
  MariaDB while passing on SQLite, which returns TEXT verbatim. The audit package
  therefore keeps its **own** timestamp layout rather than sharing the
  repository's: one needs timestamps that sort, the other needs bytes that
  round-trip.
- **`detail` is always a JSON object** (`{}` when empty), because MariaDB's JSON
  column rejects a bare empty string while SQLite's TEXT accepts anything.
- **Detail keys are sorted** when rendered. Go randomizes map iteration and the
  detail is hashed; unsorted keys would make a row's hash depend on the run.
- **MariaDB only.** MariaDB's `JSON` is an alias for LONGTEXT + `json_valid()`,
  so it returns the bytes it was given. MySQL 8's native JSON parses to a binary
  form and re-serializes on read — reordering keys and respacing — which would
  change the hashed bytes. HeroPanel targets MariaDB ([02](02-tech-stack.md)); a
  port to MySQL means moving `detail` to TEXT first.

## 4. What tamper-evidence does and does not buy

`Verify` proves the table has not been edited **by someone who could not also
recompute the chain**. An attacker with `UPDATE` on `audit_log` can rewrite a row
and every hash after it, and `Verify` will then pass. The chain's value is that it
forces tampering to be *wholesale rather than surgical*, and that it is detectable
against **any independently held earlier head** — which is why exporting to a sink
the panel cannot reach is the documented next step, not a nice-to-have.

## 5. Coverage: middleware, not convention

`auditor` is middleware mounted on the authenticated API group. Consequences:

- **Every unsafe method (POST/PUT/PATCH/DELETE) is recorded, automatically.** A
  route added tomorrow is audited because it is mounted. Forgetting is not
  available.
- It records what the edge can know: the **route pattern** (`POST
  /api/v1/sites/{uid}/git/deploy` — the pattern, not the path, so calls group by
  endpoint), the actor from the request principal, the resource type from the
  path, the `{uid}` param, and the outcome from the status code.
- Handlers add what only they know via `audit.SetResource` / `audit.AddDetail` /
  `audit.SetActor` — e.g. a `POST /sites` cannot name the site from the URL,
  because the uid is minted inside the service.
- **The auditor sits above CSRF**, so a rejected mutation is still recorded.
  Below it, CSRF would short-circuit the chain and the attempt would vanish —
  and "someone tried and was refused" is exactly what a reviewer looks for.
  `401`/`403` are recorded as `denied`, distinct from other failures.
- **The git webhook carries the auditor explicitly.** It is mounted outside the
  auth group (it has no session by design), and an endpoint that deploys code on
  presentation of a shared secret is the last one that should be missing.
- **Reads are not audited** — auditing every GET buries the signal — **except
  where a read discloses as much as a write**: `GET /databases/{uid}/export`
  hands over the entire database and calls `audit.Force`. So does reading the
  audit log itself.

### 5.1 Secrets never enter the chain

Audit rows outlive the credentials they describe and are meant to be exported.
Handlers record the *fact*, not the material: `auth_kind: "ssh_key"` never the
key; the database username never its password; the login's email never the
password attempt. The one deliberate inclusion is Adminer SSO's throwaway
**account name** — it is what ties the row to the queries the database's own log
will show.

## 6. Known gap: async mutations

A mutation that returns `202 + job` is recorded at the edge as the **request**
("user 7 asked to deploy site X as job J"), not as the completion. The worker's
own actions are not written to this chain. They are not unrecorded — the broker
audits every privileged capability it runs — but joining the two means going
through the job uid, which is why the edge records it as a detail. Per-worker
audit rows are the documented follow-up.

Likewise, the entry is written **after** the handler returns, so a failed audit
write cannot fail the request: the response is already on the wire and the
mutation already happened. It is logged at ERROR instead. Recording an *intent*
row before the handler (as the broker does) would close that window at the cost
of doubling the table; the broker's chain already covers the privileged half of
any such gap.

## 7. API

```
GET /api/v1/audit          audit.read  → entries, newest-first
    ?actor_user_id= &resource_type= &resource_id= &action= &limit= &offset=
GET /api/v1/audit/verify   audit.read  → { intact: bool, error?: string }
```

`verify` answers **200 with `intact:false`** when the chain is broken. A 500 would
be wrong: the request succeeded, and the answer is "no". Reporting a finding as a
server error invites it to be dismissed as a glitch.

Listings are capped at 200 and default to 50 — this is the one table that only
grows.

## 8. Definition of Done

- [x] Domain + service + repo interface (clean architecture)
- [x] REST endpoints + RBAC (`audit.read`)
- [x] **Every mutation covered by construction** (middleware, §5)
- [x] Unit tests: chain links, tamper detection, deletion detection, failed-append
      head handling, concurrent appends (one unbroken chain), restart resumption,
      timestamp round-trip, stable detail ordering
- [x] Middleware tests: every unsafe method recorded; reads ignored; forced reads
      recorded; denied/failure outcomes; user vs API-key vs anonymous actors;
      handler annotations; a failing audit write does not break the response
- [x] Repository tests against SQLite incl. tamper-via-SQL detection
- [x] **Live e2e** (`deploy/docker/e2e/run-audit.sh`, in CI) against **real
      MariaDB** — see below
- [ ] Frontend feature slice
- [ ] Export to an external append-only sink (§2)

**Live e2e — and why it runs on MariaDB.** `run-audit.sh` is the first suite to
run hpd's *own* control-plane store on MariaDB rather than SQLite. Every other
suite uses SQLite, which meant the **mysql half of the migrations had never once
been executed** and the chain had only ever been verified against an engine that
returns TEXT verbatim. It asserts: all 15 mysql migrations apply; bootstrap and
login are themselves in the chain; a password never appears in it; a real
mutation is recorded with its resource and detail; an unauthenticated attempt is
recorded as `denied`/`anonymous`; a plain GET adds no row while an export adds
one; **the chain verifies on MariaDB**; `created_at` comes back with six
fractional digits; an `UPDATE` run directly in SQL is detected; and an anonymous
caller cannot read the log.

---
Back to [index](README.md). Related: [05 — Security](05-security-architecture.md) §9,
[10 — Roadmap](10-roadmap.md) Phase 0.
