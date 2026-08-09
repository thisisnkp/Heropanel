# 22 — Backups

Full + incremental site backups: compressed, **always encrypted**, stored
locally or on any S3-compatible endpoint, on a schedule, restored **into a new
site**. In-core, satellite-ready like its siblings.

Back to [index](README.md).

## 1. The split of trust

The privileged half is deliberately tiny. The broker knows three verbs:
`backup.create` (tar a validated site tree into a staging file),
`backup.restore` (untar a staged file into a site tree and re-own it), and
`backup.prune` (delete one staged file). Every clever thing — sealing, uploads,
chain bookkeeping, scheduling, retention — lives in **unprivileged npd**, which
can be wrong without being root. The staging directory
(`/var/lib/nexpanel/backups`, 0700, panel-owned) is the hand-off point, the
same pattern as database dumps.

## 2. Incrementals are GNU tar's own

`--listed-incremental` with a per-site snapshot file: a **full** resets the
snapshot (level 0), each **incremental** records only what changed since —
including deletions. Restore replays the chain (the full, then each incremental
in order) with the documented `/dev/null` snapshot. This is decades-old tar
behaviour, not a bespoke diff format that would have to earn trust from zero.
Compression is zstd via `tar --zstd` — the system binary, zero Go dependencies.

Chain arithmetic lives in one small function (`chainFor`) and is unit-tested:
the latest full at-or-before the target plus everything after it; a second
chain's full never leaks into the first; an incremental with no full refuses
rather than restoring garbage. **Deleting a backup deletes its dependents** —
every later backup in the same chain — and says so, because a chain that breaks
silently at restore time is the worst outcome a backup system can have.

## 3. Encryption is not optional

Every archive is sealed with **chunked AES-256-GCM** (`pkg/blobcrypt`, the
STREAM construction: per-file random nonce prefix, per-chunk counter, a
final-chunk flag so truncation fails authentication — tamper, reorder, truncate
or append and the restore refuses with nothing written). The key is a
purpose-derived subkey of the panel master key (HKDF, `"backup-v1"`), so backup
and column-sealing keys can never be confused. **No `NP_SECRET_KEY`, no
backups** — the module reports unavailable rather than ever storing a site's
data in the clear. The plaintext staging archive is removed before Create
returns, success or failure.

## 4. Targets

- **local** — the sealed file stays in the staging directory. Always available.
- **s3** — any S3-compatible endpoint (AWS, R2, B2, MinIO), configured via
  `backup.s3.*` / `NP_BACKUP_S3_*`. The client is ~200 lines of stdlib with
  hand-rolled **SigV4** (the panel's lean-dependency rule; three verbs do not
  justify an SDK's tree), verified in tests by *recomputing* the signature
  server-side rather than matching a golden string — and proven live in e2e
  against a real **MinIO** (an independent S3 implementation the client did not
  write): upload, restore-from-bucket, delete. Uploads use UNSIGNED-PAYLOAD —
  the payload is already AEAD-sealed, so integrity does not rest on the
  transport hash. At startup npd creates the bucket if it is missing
  (idempotent PUT; 409 = fine), so the most common S3 misconfiguration
  surfaces at boot instead of at the first scheduled backup.

Deferred: SFTP and OAuth drives (GDrive/OneDrive/Dropbox) — each pulls a
dependency or an OAuth flow, and the S3 surface already covers the common
self-hosted and commodity-cloud cases.

## 4b. The database rides along

A site's backup policy may name one panel-managed database (`db_uid`). Every
backup then carries a **full dump** of it — SQL dumps do not do incrementals;
each stands alone — as a **second sealed object** on the same target
(`<uid>.db.enc` next to the tree's `<uid>.enc`), produced by the same
`db.export` capability customers use (mysqldump `--single-transaction`),
sealed before it touches storage, plaintext discarded before Create returns.
A failed dump **fails the whole backup**: a backup that silently skipped its
database is a lie the operator would discover at restore time. Deleting a
backup deletes its dump object with it.

On restore, the wizard may name a **new** database: the dump is fetched,
authenticated, staged through the import path, and loaded into a freshly
created database — the original database is never touched, for exactly the
reason the tree goes into a new site.

## 4c. The panel backs itself up

The panel's own database — users, sites, sealed secrets, the one thing
packages cannot rebuild — rides the same pipeline: snapshotted (SQLite
`VACUUM INTO` on the live handle; MariaDB via the broker's `db.export`),
wrapped with a small manifest, sealed with the same derived key, stored on a
configured target, swept hourly against `backup.panel.*` (enabled by default,
daily, keep 7 — it costs a few MB and is the difference between a bad day and
a disaster; it still runs only when `NP_SECRET_KEY` exists).

Restore is deliberately **not an API endpoint**: a panel that needs its
database back cannot be trusted to serve that request. Recovery is
out-of-band —

```
NP_SECRET_KEY=<the master key you kept safe> \
  npd decrypt <snapshot>.enc panel.tar.gz
tar -xzf panel.tar.gz          # panel.db (or panel.sql.gz) + manifest.json
# stop npd; put the database back (copy the file / import the dump); start npd
```

`npd decrypt` works with nothing but the binary and the master key — no
config, no datastore, no broker — and opens *any* sealed backup object (site
archive, database dump, panel snapshot). The master key is the one thing the
operator must hold outside the backups themselves; without it every stored
object is ciphertext, which is the point.

## 5. Scheduling and retention

A per-site policy (`enabled`, `interval_hours`, `target`, `keep_chains`) drives
an in-process hourly sweep — npd's own ticker, like the SSL renewer, because the
job needs the panel's key and database (a systemd cron unit could carry
neither). Any enabled site whose newest backup is older than its interval gets
one (auto level: full for a fresh chain, incremental after). A new full retires
the oldest chains beyond `keep_chains`, rows and stored objects both.

## 6. Restore goes into a NEW site

The wizard provisions a fresh site and replays the chain into it, then re-owns
the tree to the new site's user. The original keeps serving untouched while the
copy is verified — a mistaken restore destroys nothing, and promoting the copy
(pointing the domain, suspending the old site) is an explicit act. This is also
exactly the phase's exit criterion, and the shape disaster recovery actually
takes: restore beside, verify, then switch.

## 7. Definition of done

Broker capabilities unit-tested with the fake runner/fs: a full resets the
snapshot and an incremental keeps it, archives are staged 0600 panel-owned,
restore extracts with `/dev/null` and re-owns to the destination user,
traversal/level/vhost inputs are refused before tar runs. `pkg/blobcrypt` is
tested for round trips across chunk boundaries, tamper, truncation, wrong-key
and trailing-garbage rejection. The S3 signer is verified by server-side
recomputation. Chain selection and dependent-deletion are unit-tested.

Live proof: **`deploy/docker/e2e/run-backup.sh`** (in CI) — a site with real
content: full backup (**at rest is blobcrypt ciphertext**, unreadable as tar; no
plaintext staging file survives), edits, an incremental (**smaller than the
full**), then the chain restored into a **new site**: the edited file carries
its latest content, the added file exists, everything belongs to the new site's
user, the original is untouched, both capabilities are on the broker's audit
chain, and deleting the full removes its dependent incremental explicitly.

The same suite also proves, live: the **S3 leg against a real MinIO** — the
backup lands in the bucket (and *not* on the local disk), restores from the
bucket, and leaves the bucket when deleted; the **database leg against a real
MariaDB** — a row written before the backup comes back in a **new** database
restored beside a new site, the original database untouched; and the **panel
leg** — a sealed snapshot taken via the API, opened offline with `npd
decrypt`, and yielding a tarball whose `panel.db` actually contains the
panel's data.

## SFTP target

An **SFTP (SSH) target** gives an off-cloud copy for the 3-2-1 rule. Rather than
pull a third-party SFTP dependency, it is a **hand-rolled minimal SFTP-v3
client** (`internal/backup/sftp.go`) over `x/crypto/ssh` — the transport the git
deploy keys already use — implementing just what a backup target needs
(create+write, open+read, remove), the same lean-deps posture as the hand-rolled
SigV4 signer and WebAuthn verifier. Credentials (password or private key) come
from the **secret env**, never the yaml file; the server's **host key is pinned**
(an `authorized_keys` line), and when pinned the client also forces that key's
algorithm so a host offering another key type cannot slip past the fixed-key
check. Sealed blobs are opaque bytes to it; encryption stays in the caller.

Live proof (`run-backup.sh`, against a **real openssh** SFTP server in e2e): a
sealed backup is **written over SFTP** to the server and is **not** on local
disk; the object at rest is blobcrypt ciphertext (unreadable as tar); restore
**fetches it back over SFTP** and replays it into a new site; delete removes it
from the server (SFTP remove).

## rclone target (cloud drives)

An **rclone-backed target** reaches rclone's 70+ cloud backends — Google Drive,
Dropbox, OneDrive, Backblaze and the rest — **without the panel implementing any
provider API or OAuth flow**. The operator configures the remote once with
`rclone config`; the panel streams its already-sealed blobs to it. rclone is a
local CLI npd execs directly (like local/S3/SFTP — no broker; there is no
privilege to cross, only the operator's rclone config and the network): `rclone
rcat` to upload, `rclone cat` to fetch, `rclone deletefile` to remove, with a
missing object tolerated on delete. Configured via `NP_BACKUP_RCLONE_REMOTE`
(e.g. `gdrive:nexpanel-backups`) + optional `NP_BACKUP_RCLONE_CONFIG`.

Live proof (`run-backup.sh`, against an rclone `:local:` backend in e2e): a
sealed backup is **streamed to the remote via rclone** and is **not** on npd's
local disk; the object at rest is blobcrypt ciphertext; restore **fetches it
back via rclone** and replays it into a new site; delete removes it.

This is the lean answer to "OAuth drive targets": one external tool, no OAuth
code or provider SDKs in npd. Native in-panel OAuth per provider stays out of
scope by design. The panel-restore procedure is documented and its decrypt half
proven, but the "stop npd, swap the database, start npd" walk-through is manual
by design.

---
Back to [index](README.md).
