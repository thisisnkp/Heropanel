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

**In (slice 1, this pass):**
- One Git **source** per site: `repo_url` (public HTTPS), `branch`, optional
  `build_command`, optional `web_root` subdirectory, an auto-generated
  `webhook_secret`.
- **Deploy** (async job): shallow-clone the branch into a fresh timestamped
  release under the site home, run the build command as the site user, then
  atomically flip the live release.
- **Release history** + **rollback** to any prior successful release.
- **Webhook** endpoint (secret-gated, no panel session) so `git push` triggers a
  deploy.
- Broker capabilities `git.deploy` and `git.rollback`, both executing entirely
  as the **unprivileged site user** (`runuser`), confined to the site root.

**Deferred (later slices, explicitly out of scope now):**
- Private-repo credentials (PAT / deploy key). Requires an encrypted secret
  store; slice 1 is public-HTTPS only. `GIT_TERMINAL_PROMPT=0` makes a private
  repo fail fast instead of hanging on a credential prompt.
- Provider-specific webhook **signature** verification (GitHub `X-Hub-Signature-256`,
  GitLab token header). Slice 1 uses a constant-time shared-secret compare.
- App runtimes (Node/Python/Go long-running processes under systemd). Slice 1
  builds static/PHP output; process supervision is its own module.
- Repo cache / incremental fetch, submodules, LFS, monorepo path filters.

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
- Release **retention** (prune old release dirs, keep the last N) is deferred to a
  follow-up maintenance step so the deploy capability stays a deterministic,
  fully-testable argv sequence; the `keep` hint is already plumbed through.

## 3. Deploy sequence (broker `git.deploy`)

All steps run as the site user via `runuser -u <user> --`, every path
`ValidatePath`-checked against policy, with a minimal explicit environment
(`HOME=<home>`, a fixed `PATH`, `GIT_TERMINAL_PROMPT=0`, `GIT_SSH_COMMAND` off).

1. `install -d -m0750 -o user -g user <home>/releases <home>/shared` (idempotent).
2. `git clone --depth 1 --single-branch --branch <branch> <repo_url> <release>`.
3. `git -C <release> rev-parse HEAD` → record the commit SHA.
4. If `build_command` set: `runuser -u user -- /bin/sh -lc 'cd <release> && <build_command>'`,
   bounded by a timeout. (See §5 on why a shell here is safe.)
5. Ensure `public → current/<web_root>` exists (create the symlink on first
   deploy, replacing the empty `public` dir).
6. **Activate:** `ln -sfn <release> <home>/current.tmp` then
   `mv -T <home>/current.tmp <home>/current` — the atomic flip.

Release retention (pruning old release dirs) is a deferred follow-up (§2).

Returns `{commit, release, activated:true, log:<tail>}`. On failure before step 6
the live release is untouched (fail-safe: a broken build never takes the site
down).

`git.rollback` is steps 6 only, targeting a caller-supplied existing release dir
(validated to be under `<home>/releases/`).

## 4. Data model (migration 0006)

```
git_sources
  id, uid, site_id (UNIQUE FK sites), repo_url, branch,
  build_command, web_root, webhook_secret, created_at, updated_at

git_deployments
  id, uid, site_id (FK sites), source_id (FK git_sources),
  commit_sha, status(pending|running|success|failed|rolled_back),
  trigger(manual|webhook|rollback), release_dir, log,
  created_at, finished_at
```

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
- **Input validation.** `repo_url` must be `https://` with a clean host/path;
  `branch`/`web_root` match strict ref/subpath allowlists (no shell metachars, no
  `..`); `build_command` is length-bounded.
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

## 7. Definition of Done (this slice)

- [x] Domain + service + repo interface (clean architecture)
- [x] Broker capabilities with validation + path confinement + atomic/fail-safe apply
- [x] REST endpoints; async ops return jobs with WS progress
- [x] RBAC scopes + audit coverage
- [x] Unit tests: service validation + capability argv/sequence (FakeRunner)
- [x] **Live e2e** (`deploy/docker/e2e/run-git.sh`): real OpenLiteSpeed clones
      `octocat/Hello-World` over HTTPS, builds as the site's unprivileged user,
      and serves the built output (HTTP 200); a second deploy + rollback proves
      the atomic swap is reversible. Wired into CI.

---
Back to [index](README.md). Related: [05 — Security](05-security-architecture.md),
[10 — Roadmap](10-roadmap.md) Phase 3.
