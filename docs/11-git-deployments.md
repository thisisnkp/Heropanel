# 11 — Git Deployments (module design)

Phase 3, in-core. Lets a site pull its content/app from a Git repository, build
it as the site's own unprivileged Linux user, and activate the result with an
**atomic, reversible** release swap. This is the "point a site at a repo and it
goes live on push" workflow that RunCloud/Coolify/Vercel users expect.

Prereqs already in place: the Sites module (isolated Linux user + directory
tree), the privileged broker, the async job queue with WS progress, and RBAC.
This module adds no new isolation primitive — it reuses the per-site user.

---

## 1. Scope

**In:**
- One Git **source** per site: `repo_url`, `branch`, optional `build_command`,
  optional `web_root` subdirectory, an auto-generated `webhook_secret`.
- **Private repositories** (slice 2 — see §5.1): an HTTPS personal access token,
  or an ed25519 **deploy key** the panel generates itself. Credentials are sealed
  with [pkg/secrets](../pkg/secrets) into `git_sources.credential_enc`.
- **Composer auto-install** (§3): a release with a `composer.json` gets
  `composer install --no-dev --optimize-autoloader` before the build command, so
  a Laravel/Symfony checkout is runnable with no build command at all.
- **Release retention**: each deploy prunes superseded releases, keeping the live
  one plus `keep - 1` others.
- **Deploy** (async job): shallow-clone the branch into a fresh timestamped
  release under the site home, run the build command as the site user, then
  atomically flip the live release.
- **Release history** + **rollback** to any prior successful release.
- **Webhook** endpoint (secret-gated, no panel session) so `git push` triggers a
  deploy.
- Broker capabilities `git.deploy` and `git.rollback`, both executing entirely
  as the **unprivileged site user** (`runuser`), confined to the site root.

**Webhook signature verification (done).** The webhook now accepts, in
constant time and any one of which authorizes the push: a **GitHub**
`X-Hub-Signature-256` HMAC-SHA256 over the raw body (the strong proof — bound to
*this* payload, so a captured request cannot be replayed with different bytes), a
**GitLab** `X-Gitlab-Token`, or the bare shared secret (URL/header) for a manual
`curl`. The proof *kind* is recorded in the audit entry (never its value). See
`WebhookProof` in [internal/git/service.go](../internal/git/service.go);
live-verified by `run-fastapi.sh`.

**SSH host-key pinning (done).** A source may carry a `host_key`
(`ssh-keyscan` / `known_hosts` format). When set, the clone runs with
`StrictHostKeyChecking=yes` against that pinned key, so the **first** connection
is verified too — defeating a MITM on first contact; empty falls back to
`accept-new` (TOFU), which still refuses a *changed* key mid-clone. The host key
is public and is returned by the API. Live-verified by `run-git-private.sh` (a
wrong pin is refused with `host key verification failed` even with a valid deploy
key; the correct pin clones).

**Token/HTTPS clone (verified live).** The HTTPS-token path is now exercised
end-to-end against a real HTTPS `git http-backend` server behind a private-CA
cert (`run-git-token.sh`): the right token clones and serves, a wrong token is
refused, and the token is sealed at rest.

**Deferred (explicitly out of scope now):**
- OAuth app authorization (the panel takes a token you paste; it does not run an
  OAuth flow to obtain one).
- Repo cache / incremental fetch, submodules, LFS, monorepo path filters.
- Rotating **data keys** for the sealed credential (docs/05 §6). Today one key is
  derived from the master key, so rotating means re-encrypting rows; the `hp1.`
  version prefix reserves room to change that.

## 2. Release layout

Everything lives **inside the existing site home** (`/srv/heropanel/sites/<id>`),
so the broker's `PathRoots` confinement already covers it — no new allowed root.

```
<home>/
  releases/
    01J.../          # one immutable checkout per deploy (ULID-named)
    01K.../
  shared/            # persisted across releases (reserved for slice 2: .env, uploads)
  current  -> releases/01K...        # the live release (a symlink; the swap point)
  public   -> current/<web_root>     # OLS docRoot; created once, never re-swapped
```

- **`public` is the OLS docRoot** and is unchanged from a bare-metal site's point
  of view. For a git site it is converted **once** from a real directory into a
  symlink to `current/<web_root>` (default `web_root` = release root).
- **Only `current` flips per deploy**, and it flips via `rename(2)` of a
  pre-created temp symlink — atomic. Because `public → current`, in-flight
  requests always resolve to a complete release; the swap is never half-applied.
- **Rollback** = repoint `current` at an older release dir. No rebuild.
- **Retention**: the last step of a deploy prunes superseded releases, keeping the
  live one plus `keep - 1` others (`keep` = 5). It is best-effort and last on
  purpose — the new release is already live, so a pruning hiccup must never fail a
  good deploy. Release ids are ULIDs, so a lexical sort is a chronological one.
  Without this every deploy leaks a full checkout and the disk fills up silently.

## 3. Deploy sequence (broker `git.deploy`)

All steps run as the site user via `runuser -u <user> --`, every path
`ValidatePath`-checked against policy, with a minimal explicit environment
(`HOME=<home>`, a fixed `PATH`, `GIT_TERMINAL_PROMPT=0`, `GIT_SSH_COMMAND` off).

0. **Stage the credential** (private repos only, §5.1) under
   `/run/heropanel/gitauth/<random>/`.
1. `install -d -m0750 -o user -g user <home>/releases <home>/shared` (idempotent).
2. `git [-c credential.helper=…] clone --depth 1 --single-branch --branch <branch> <repo_url> <release>`.
3. `git -C <release> rev-parse HEAD` → record the commit SHA.
3b. **Destroy the credential** — see §5.1 for why this must precede step 5.
4. **Composer**, if `auto_composer` is on and the release has a `composer.json`:
   `composer install --no-interaction --no-progress --prefer-dist --no-dev
   --optimize-autoloader` in the release dir. It runs *before* the build command
   so a build can rely on `vendor/`, and `--no-dev` keeps dev tooling off a
   production box. A release without a `composer.json` is a no-op, not an error.
5. If `build_command` set: `runuser -u user -- /bin/sh -lc 'cd <release> && <build_command>'`,
   bounded by a timeout. (See §5 on why a shell here is safe.)
6. Ensure `public → current/<web_root>` exists (create the symlink on first
   deploy, replacing the empty `public` dir).
7. **Activate:** `ln -sfn <release> <home>/current.tmp` then
   `mv -T <home>/current.tmp <home>/current` — the atomic flip.
8. **Prune** superseded releases (§2). Best-effort; never fails the deploy.

Returns `{commit, release, activated:true, log:<tail>, pruned:<n>}`. On failure
before step 7 the live release is untouched (fail-safe: a broken build never
takes the site down), and the half-built release is removed.

`git.rollback` is step 7 only, targeting a caller-supplied existing release dir
(validated to be under `<home>/releases/`).

**Both a deploy and a rollback then restart the site's app runtime** (a no-op for
sites without one). Flipping `current` is enough for a static or PHP site — the
web server reads the files per request — but a long-running app has already
loaded its code from the old release and will keep serving it. A rollback that
does not restart reports success while the bad release stays live, which is the
worst possible outcome for the one operation an operator reaches for when things
are already broken.

**A real deploy needs the async job path.** `npm install && next build` takes
minutes; the panel's HTTP write timeout is 30s. With Redis configured a deploy
returns `202` + a job and runs on a worker, which is the real path. The
synchronous fallback (no Redis) only works for builds that finish inside a
request. The broker client's own timeout for `git.deploy` is likewise raised well
above the broker's internal budget — see `internal/broker`'s `capabilityTimeouts`.

## 4. Data model (migrations 0006, 0011, 0012)

```
git_sources
  id, uid, site_id (UNIQUE FK sites), repo_url, branch,
  build_command, web_root, webhook_secret,
  auth_kind(none|token|ssh_key), auth_username, credential_enc, public_key,
  auto_composer, created_at, updated_at

git_deployments
  id, uid, site_id (FK sites), source_id (FK git_sources),
  commit_sha, status(pending|running|success|failed|rolled_back),
  trigger_kind(manual|webhook|rollback), release_dir, log,
  created_at, finished_at
```

`credential_enc` holds the sealed token or private key and is **never** returned
by the API. `public_key` is the half an operator registers on the repository.
(`trigger` is spelled `trigger_kind` because `trigger` is reserved in MariaDB.)

`git_sources` is 1:1 with a site (like `site_system_users`). Deployments are the
append-only history that drives the UI timeline and rollback.

## 5. Security model

- **No new privilege.** `git.deploy`/`git.rollback` do everything as the site's
  existing unprivileged uid via `runuser`. The broker (root) only *drops* to that
  uid; it never runs git or the build as root.
- **Path confinement.** Every filesystem argument is `ValidatePath`-checked
  against `PathRoots` (`/srv/heropanel/sites`) after `path.Clean`, so `..`
  traversal and absolute escapes are rejected. Release dirs are ULIDs the broker
  generates — never caller-controlled strings.
- **Input validation.** `repo_url` must be `https://` with a clean host/path (or
  an SSH remote when a deploy key is configured — §5.1); `branch`/`web_root`
  match strict ref/subpath allowlists (no shell metachars, no `..`);
  `build_command` is length-bounded. Credentials embedded in the URL
  (`https://user:pw@host/…`) are rejected outright rather than quietly accepted:
  they would end up in argv, logs, and the API response.
- **The build shell.** `build_command` is intentionally run through `sh -lc` —
  builds are inherently shell (`composer install && npm run build`). This is
  *not* a command-injection hole: the command is supplied by an authenticated
  `site.write` caller and executes **only** as the unprivileged, `open_basedir`-
  confined site user. It cannot reach another tenant or escalate. This is the
  same trust boundary every deploy tool operates under, and it is confined by the
  isolation the Sites module already guarantees.
- **Webhook.** Mounted outside the authenticated group; authorized solely by a
  constant-time compare of the per-source `webhook_secret`. It only ever enqueues
  a deploy for the one site the secret belongs to.
- **Audit.** Both capabilities flow through the broker's hash-chained audit like
  every other privileged op; deploys are also first-class rows in
  `git_deployments`.

### 5.1 Private repositories

Two mechanisms, one rule: **the secret never appears in argv, and never outlives
the clone.**

- **At rest.** The token or private key is sealed by [pkg/secrets](../pkg/secrets)
  (AES-256-GCM, per-record nonce) and bound as AAD to `git_sources:<site_id>:credential_enc`,
  so a ciphertext lifted into another site's row fails to open rather than
  decrypting into the wrong tenant's credential. It keys on `site_id`, not the
  surrogate `id`, because the source is 1:1 with the site and is upserted — the
  surrogate can change, the site cannot. Without a configured
  `security.secret_key` the panel **refuses** to store a credential; it never
  falls back to plaintext.
- **Never in argv.** `/proc/<pid>/cmdline` is world-readable, so a token on a git
  command line is a token every other site on the box can read. Instead the secret
  goes into a 0600 file owned by the site user and git is pointed at the *path*:
  `credential.helper=store --file=…` for a token, `GIT_SSH_COMMAND -i …` for a key.
  Only paths and the (non-secret) basic-auth username ever reach a command line.
- **Staged on tmpfs.** `/run/heropanel/gitauth/<random>/`, so the secret never
  touches persistent disk or a backup, and is gone after a reboot even if a crash
  skips cleanup. The parent is `0711` root — traversable so git (running as the
  site user) can reach its own credential, but not listable, so one site cannot
  enumerate another's deploys; each per-deploy directory is `0700` owned by its
  own site user.
- **Destroyed before the build runs** (step 3b). The credential file is owned by
  the site user *because git runs as that user* — which means the site's own
  `build_command` could otherwise just `cat` it and walk off with the operator's
  deploy token. Nothing after the clone needs it, so it dies first. It is also
  removed on every error path.
- **Deploy keys are generated by the panel**, never uploaded. The private half
  then has exactly one provenance and one home, and there is no window in which it
  sits in a browser, a clipboard, or a request log. The operator handles only the
  public half. Re-saving a source keeps the key (rotating it would silently break
  the repo's registered key); `rotate_key` is explicit.
- **The URL and the auth kind must agree.** A token is basic-auth over HTTPS and
  an `ssh://` URL would silently ignore it; a deploy key is useless against an
  `https://` remote. Rejecting the mismatch at save time turns an opaque
  "authentication failed" at clone time into a clear error.
- **Token usernames** default per host (`x-access-token` for GitHub, `oauth2` for
  GitLab, `x-token-auth` for Bitbucket) — the single most common cause of a
  "works in my terminal, fails in the panel" token clone.
- **Host keys** are trust-on-first-use (`accept-new` against an empty per-deploy
  `known_hosts`). See §1 Deferred.

## 6. API surface

Gated by new RBAC scopes `git.read` / `git.write` (a git source belongs to a
site; scopes are kept separate for granular delegation, matching dns/ssl/db).

```
GET    /api/v1/sites/{uid}/git                 git.read   → source (404 if unset)
PUT    /api/v1/sites/{uid}/git                 git.write  → upsert source
GET    /api/v1/sites/{uid}/git/deployments     git.read   → history
POST   /api/v1/sites/{uid}/git/deploy          git.write  → 202 + job (async)
POST   /api/v1/sites/{uid}/git/rollback/{dep}  git.write  → 202 + job
POST   /hooks/git/{siteUID}                     (secret)  → 202 + job   [no session]
```

Async ops return `202 + job` and stream progress over the existing WS hub
(`job:<uid>`), identical to `site.create`.

The source body accepts `auth_kind` (`none|token|ssh_key`), `auth_username`,
`token` (write-only — never echoed back; omit on an update to keep the stored
one), `rotate_key`, and `auto_composer` (a JSON `null`/absent keeps the stored
setting, so a client that does not know the field cannot silently disable
Composer). The response carries `auth_kind`, `auth_username`, `public_key`, and
`auto_composer` — never the secret.

## 7. Definition of Done

- [x] Domain + service + repo interface (clean architecture)
- [x] Broker capabilities with validation + path confinement + atomic/fail-safe apply
- [x] REST endpoints; async ops return jobs with WS progress
- [x] RBAC scopes + audit coverage ([15](15-audit.md); every mutation here is
      recorded by the edge auditor, incl. the webhook. This box was checked long
      before anything wrote to `audit_log` — it is true now.)
- [x] Unit tests: service validation + capability argv/sequence (FakeRunner),
      including "the secret never appears in argv" and "the credential dies before
      the build runs"
- [x] **Live e2e** (`deploy/docker/e2e/run-git.sh`): real OpenLiteSpeed clones
      `octocat/Hello-World` over HTTPS, builds as the site's unprivileged user,
      and serves the built output (HTTP 200); a second deploy + rollback proves
      the atomic swap is reversible. Wired into CI.
- [x] **Live e2e, private repo** (`run-git-private.sh`): a real `sshd` + bare repo.
      The panel generates a deploy key; the clone **fails** with
      `Permission denied (publickey)` until the public half is registered, then
      succeeds and serves the page. Asserts the private key is absent from the API
      response, sealed at rest (no PEM in the datastore), and that no credential
      material survives on `/run`. Wired into CI.
- [x] **Live e2e, Composer** (`run-php-app.sh`): a Laravel-shaped private repo with
      a real `composer.json` dependency and **no build command** deploys, Composer
      resolves and installs it, and the served page proves the autoloader works and
      the app reaches a panel-created MariaDB database. Wired into CI.

**Verified as not-covered:** the **token/HTTPS** path is unit-tested only. Standing
up an HTTPS git server with a trusted certificate inside the e2e image is not worth
the fidelity it buys, so the SSH path is the one exercised live.

---
Back to [index](README.md). Related: [05 — Security](05-security-architecture.md),
[10 — Roadmap](10-roadmap.md) Phase 3.
