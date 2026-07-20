# 17 — File Manager (module design)

Phase 4, in-core. Browse, edit, upload, download, organise, and extract a site's
files from the panel — every operation performed by the root broker **as the
site's own Linux user** and confined to the site home. Ships with a code editor
(CodeMirror 6) for in-place editing.

It is deliberately **baremetal-only**. A git- or docker-managed site's content is
owned by its deploy pipeline; editing a live release out from under an
atomic-swap deploy would corrupt exactly the guarantee those modes exist to
provide. The gate lives in the service (`internal/files`, `resolveEditable`) and
is proven closed in CI.

Builds on Sites (per-site Linux user + home) and the `hp-broker` security spine
(capability model, path policy, hash-chained audit).

---

## 1. The security model is the module

A file manager is the single most dangerous surface a panel exposes: it turns
"read any byte, write any byte" into an HTTP endpoint. Two layers contain it, and
the first is what actually matters.

**1. Run as the site user, never as root.** Every file capability but one execs
under `runuser -u <site-user> -- /usr/bin/env <tool> …` (no shell, argv array).
This is the real containment: a symlink under the site root pointing at
`/etc/shadow` is only as reachable as the site user's own uid allows — i.e. not
at all — and a write cannot escape the tree the site user owns. Root never holds
the file handle. The e2e proves this directly: every file created through the API
is owned by the site's uid (`hps1`), not `root`.

The single exception is `file.chown` (§3): changing a file's *owner* is root-only
on Linux, so that operation cannot run as the site user by definition. It is
constrained by its target instead — it can only ever assign the site's own
account — and it is called out explicitly here rather than hidden, because "every
op runs unprivileged" would otherwise be a claim this module does not quite meet.

**2. Path confinement (belt and suspenders).** `confinedFilePath(root, rel)`
prefixes `/` and `path.Clean`s the relative part first, so `../` sequences and
absolute inputs collapse to nothing above the root; the joined result is then
re-checked against the broker's `PathRoots` policy. A request naming
`../../../../etc/passwd` is clamped to `<site-root>/etc/passwd` — the real file is
never named, let alone touched. (Combined with layer 1, even the clamped write
only succeeds where the site user could already write.)

**3. No content on argv.** File bytes travel through **stdin** (`tee`) on write
and **stdout** (`dd`) on read, never as command arguments — argv is world-readable
via `/proc`, so a secret in a file must not pass through it.

Everything the module does is one of eleven capabilities (`broker/capabilities/files.go`
and `files_extra.go`), each policy-gated — `file.list`, `file.read`, `file.write`,
`file.mkdir`, `file.remove`, `file.rename`, `file.chmod`, `file.extract`,
`file.compress`, `file.chown`, `file.search` — and each audited (intent +
outcome) on the hash chain.

## 2. Chunked I/O, and why

The broker's wire framing caps a single frame at 1 MiB (`pkg/brokerwire`). A file
is arbitrarily large, so read and write are **chunked** at 512 KiB — comfortably
under the cap once base64 expansion (4/3) and the JSON envelope are accounted for:

- **Read** takes an `offset` + `length`. The broker runs
  `dd if=<file> bs=<length> count=1 skip=<offset> iflag=skip_bytes,fullblock`.
  `fullblock` is load-bearing: it makes `dd` accumulate a whole block across short
  reads, so one `count=1` block is exactly `length` bytes (or fewer at EOF). A
  short read is how end-of-file is detected. (`count_bytes` must **not** be used
  here — it would reinterpret `count=1` as a single byte and truncate every read
  to one byte. That bug shipped past the mock tests and was caught only by the
  live e2e; the read test now asserts `count_bytes` never appears.)
- **Write** takes an `append` flag. `append=false` truncates then writes (an
  editor save, or the first chunk of an upload); `append=true` appends (later
  upload chunks). `hpd` streams a whole file by looping.

`internal/files` wraps this: `Download(w io.Writer)` loops `file.read` over
advancing offsets until EOF; `Upload(r io.Reader)` streams the body into 512 KiB
`file.write` chunks (first truncating, rest appending), and truncates to zero for
an empty body so "clear a file" works. Content is binary-safe end to end: Go
marshals `[]byte` to base64 in the envelope, so NUL and high bytes round-trip
(the e2e asserts a `\x00\xff…` blob survives).

## 3. Archives, ownership, and search

**Extract.** `file.extract` unpacks an archive into a destination directory,
still as the site user. The archive kind is chosen by filename suffix and
restricted to `.zip` (unzip, confined to `-d <dest>`) and `.tar[.gz|.bz2|.xz]`
(tar `-C <dest>`); anything else is refused, never shelled out. The destination
is created (as the user) first. This is how a customer uploads a built site as
one archive instead of hundreds of PUTs.

**Compress.** `file.compress` is the counterpart, and it is what makes "download
this folder" possible at all — HTTP hands back one file, not a tree. Sources are
each clamped, then required to share a single parent, and the tool runs *from
that parent* with basenames. That is deliberate: an archive must contain
`assets/logo.png`, not `srv/heropanel/sites/1/assets/logo.png`, or unpacking it
elsewhere would recreate the server's absolute tree. The e2e asserts exactly
that.

**Copy and move.** `file.copy` runs `cp -aT` as the site user with both ends
clamped. `-T` matters: without it, copying onto an existing directory would land
the source *inside* it, at a path that was never confined. A folder cannot be
copied into its own subtree (that recurses until the disk fills). There is
deliberately **no** `file.move` capability — `file.rename` already confines both
ends, so moving into another folder is a rename with a destination elsewhere in
the tree, and a second capability would be a second thing to audit for no gain.

Whether the destination is already taken is decided one layer up, in
`internal/files`, not in the broker: `cp` and `mv` clobber silently, and a paste
that replaces a file the operator had forgotten about is the data loss this
module must not make easy. Both `Copy` and `Move` therefore list the destination's
parent first and refuse with `destination_exists` (HTTP 409) unless the caller
passes `on_conflict: "rename"`, which is what the **Duplicate** action uses — it
picks `logo copy.png`, then `logo copy 2.png`, keeping compound suffixes whole so
`site.tar.gz` never becomes `site.tar copy.gz`. There is a window between the
check and the operation; that is acceptable because both run inside one site's
tree as that site's own user, so losing the race means the overwrite the check
makes *unlikely*, never a write anywhere the caller could not already write.

**Folder download.** `GET /files/archive` builds a zip server-side, streams it,
and deletes it — including when the client disconnects mid-transfer, since the
cleanup runs on a context detached from the request. It exists because the only
other route to "download this folder" is compress-then-download-then-remember-to-
delete, and the third step never happens, so sites accumulate stale archives of
themselves. It is **not** a zero-copy stream: `zip` needs seekable output and
cannot be produced on a pipe, so a temporary `.hp-download-<random>.zip` really is
written to disk. Its lifetime is one request, and the random name keeps two
concurrent downloads of the same folder from colliding. Archiving the site root
is a special case — the root has no parent inside the tree, and `file.compress`
requires all sources to share one — so its entries are listed and archived
instead.

**Ownership.** `file.chown` resets a path (recursively) to `<site-user>:<site-user>`.
It is **the one file operation that runs as root**, because changing a file's
owner is root-only on Linux — a site user can neither give a file away nor take
one. Since the "run as the site user" rule cannot hold here, the *target* is
constrained instead: the new owner is always the same validated site username the
rest of the module uses, and `root` is refused outright. There is no way to
express "chown to root" or "chown to another site", which is what stops a
root-run operation from becoming a privilege-escalation primitive. `-h` keeps it
from following symlinks out of the tree. It exists because ownership does drift
in practice (an archive carrying stored uids, a restored backup), and when it
does a site stops being able to read its own files.

**Search.** `file.search` walks a subtree as the site user: `find -iname` for
names, `grep -rlIF` for contents. `-F` means the term is a **fixed string**, so a
search box can never become a regular expression (nor a ReDoS). Results are
capped (500) and the walk is time-bounded — a convenience must not become a way
to pin a core or blow the frame budget. The e2e checks that a traversing search
path still cannot list `/etc`.

## 4. HTTP surface

All under `/api/v1/sites/{uid}/files`, gated by two new RBAC permissions —
`file.read` (browse, download) and `file.write` (everything that mutates). A
download hands a file's bytes to the caller, so although it is read-gated it is
**force-audited**, the same reasoning as a database export.

| Method | Path | Perm | Notes |
|--------|------|------|-------|
| GET | `/files?path=` | `file.read` | List a directory (empty = site root). |
| GET | `/files/content?path=` | `file.read` | Stream raw file bytes. Force-audited. |
| GET | `/files/archive?path=` | `file.read` | Stream a directory as a `.zip`; the temporary archive is deleted afterwards. |
| PUT | `/files/content?path=` | `file.write` | Write (save/upload); body is the raw bytes, truncates first. |
| DELETE | `/files?path=` | `file.write` | Delete a file or tree (refuses the site root). |
| POST | `/files/mkdir` | `file.write` | `{path}` |
| POST | `/files/rename` | `file.write` | `{from,to}` |
| POST | `/files/copy` | `file.write` | `{from,to,on_conflict?}` — `fail` (default) or `rename`; echoes the destination used. |
| POST | `/files/move` | `file.write` | `{from,to,on_conflict?}` — same contract; implemented over `file.rename`. |
| POST | `/files/chmod` | `file.write` | `{path,mode}` (3–4 octal digits) |
| POST | `/files/extract` | `file.write` | `{archive,dest}` |
| POST | `/files/compress` | `file.write` | `{sources[],archive,format}` — sources must share one folder. |
| POST | `/files/chown` | `file.write` | `{path}` — recursive; the target account is **not** a parameter. |
| GET | `/files/search?q=&path=&mode=` | `file.read` | `mode=name\|content`; capped, with a `truncated` flag. |

Content transfer is raw bytes, not the JSON envelope: base64'ing a file through an
envelope would be wasteful, and the server already streams it in chunks.

**Uploads use `XMLHttpRequest`, not `fetch`** (`uploadWithProgress` in
`web/src/lib/api.ts`). That is not a style choice: `fetch` has no upload-progress
event at all, so a 200 MB upload could only ever show a spinner — no percentage,
no way to tell a slow connection from a stalled one, and no cancel. The
streaming-request-body alternative needs HTTP/2 plus `duplex: "half"` and is not
broadly supported. The XHR path carries the same session cookie and CSRF token
and parses the same `{ error }` envelope, so callers still get an
`ApiRequestError`; a cancellation gets its own `upload_cancelled` code, because
the operator asking to stop is not a failure to report as one. Progress is
byte-accurate across the whole *batch* rather than per file — someone dropping
forty images wants to know how long the drop takes, not to watch a bar restart
forty times.

Every route is documented in the generated OpenAPI. Two tests keep that honest:
the golden drift test (a route missing from `docs/openapi.json` fails the build)
and a **permission drift test**, which drives the real router and asserts that a
principal with no permissions is refused and a principal holding exactly the
documented permission gets through. The second exists because the `Permission`
field in `openapi_routes.go` is hand-written metadata that nothing verified — a
route documented as `file.read` could have been gated on something else, or on
nothing, and shipped silently.

A third test guards the generator itself. The spec is built by *walking a
router*, and optional services mount behind `if d.X != nil`, so a service the
test router forgets is simply absent from the walk — the golden test then
compares one incomplete spec against another and passes. That is exactly how the
File Manager and terminal routes stayed out of `docs/openapi.json` for a whole
phase. `TestFullRouterMountsEveryService` reflects over `Deps` and fails on any
nil field, naming it; a hand-written checklist would have needed the same manual
update that was missed in the first place. (`/api/v1/ws` is the one route
deliberately excluded, in `skipRoute` — a WebSocket handshake has no OpenAPI 3.1
representation.)

## 5. The editor: CodeMirror 6, not Monaco

The roadmap named Monaco; we chose **CodeMirror 6** instead. Monaco is ~2 MB+ and
pulls a web-worker/AMD loader that fights a strict CSP and a Vite build; the panel
prizes a lean bundle and ships a strict `default-src 'self'` CSP with no
`'unsafe-inline'`. CodeMirror 6 is a fraction of the size, and — critically — its
`style-mod` stylesheet is injected through the **CSSOM** (`insertRule`), not as an
inline `<style>` element, so it works under that CSP without an exception. It is
**code-split**: the editor and its language packages load in their own chunk only
when a file is actually opened, so the file browser adds nothing to the initial
bundle.

Several files can be open at once in a **tab strip**, each with its own dirty
marker; closing a modified buffer confirms first. A **Diff** view shows exactly
what a save would change, computed client-side by a small LCS differ
(`web/src/lib/diff.ts`) rather than a dependency — the whole question is "which
lines changed", and it trims the common prefix/suffix and caps the compared
window so a large file cannot pin the tab.

**Keyboard shortcuts.** `Ctrl/⌘-S` is bound *inside* CodeMirror at highest
precedence (so it fires with focus in the editor and never reaches the browser's
own "save page" dialog) **and** on the panel, so it works wherever focus is. The
rest come from `basicSetup`'s standard keymaps: `Ctrl/⌘-A` select all, `Ctrl/⌘-F`
find, `Ctrl/⌘-D` select next occurrence, `Ctrl/⌘-Z` / `-Y` undo and redo, and
Alt-click or Alt-drag for multiple cursors. In the file browser: `/` focuses the
filter, `Ctrl/⌘-A` selects every visible entry, `Ctrl/⌘-C` / `Ctrl/⌘-X` stage a
copy or a cut, `Ctrl/⌘-V` pastes into the current folder, `F2` renames the single
selected entry, `Enter` opens it, `Delete` removes the selection (with a
confirm), and `Esc` clears selection and search. Every one of these is ignored
while focus is in a text input, and `Ctrl/⌘-C` additionally stands down when the
browser has a text selection — the operator is copying text they highlighted, not
files.

Language is chosen by file extension (php, html, js/ts, css, json, markdown,
python, yaml); unknown suffixes get plain text with the same editing niceties.
The theme is driven entirely by the app's CSS custom properties, so it follows
the light/dark toggle with no second theme system. Image files open in a preview
instead of the editor; a file over 2 MiB is offered for download rather than
opened in a browser buffer.

## 6. The browser

Every action lives in a **right-click menu** (`web/src/components/ContextMenu.tsx`)
rather than a row of eight icon buttons: the actions belong where the pointer
already is, and a row that carries all of them is unreadable. Rows keep only
Download and a `⋯` button that opens the same menu, so nothing is mouse-only —
the menu is a `role="menu"` with arrow-key navigation, `Enter` to select and
`Esc` to close, and it flips back into the viewport near an edge. Right-clicking
*inside* the selection acts on the whole selection; right-clicking outside it acts
on that row alone, which is what every desktop file manager does.

**Copy / cut / paste.** The clipboard is a pending *intent* — nothing moves until
paste — and it records the folder the names came from, because by then the
operator has usually navigated elsewhere, which is the entire point. Cut entries
render faded until the paste lands, and a cut clipboard clears itself afterwards
(its paths no longer exist) while a copy stays, so it can be pasted repeatedly.
Pasting a copy back into the folder it came from is a duplicate and silently
takes a free name; anywhere else, a collision is a surprise and is reported. A
folder in the menu also offers "Paste into <name>", which saves navigating in
just to paste.

**Uploads** show a byte-accurate progress bar with the current filename, the
position in the batch, bytes transferred, and a **Cancel** button that aborts the
request in flight. A cancelled or failed batch still refreshes the listing, since
the files uploaded before the stop are really there.

Beyond that: **multi-select** with a header select-all and bulk Copy / Cut /
Compress / Delete, **drag-and-drop** upload onto the listing, **New file** and
**New folder**, **sortable columns** (name, size, mode, modified — folders stay
above files whichever is sorted, and ties fall back to the name so the order
never jitters), a **show-hidden** toggle for dotfiles (on by default: `.htaccess`
and `.env` are load-bearing on a hosting panel, not clutter), a client-side
**filter** for the current folder, and a **recursive search** (by name or
content) whose results replace the listing and are individually navigable.
Renaming preselects the stem rather than the whole filename, since renaming
`logo.png` almost always means changing `logo`.

**gitignore.** Entries matched by `.gitignore` are greyed out and badged, with an
optional "hide git-ignored" toggle. Every ignore file from the site root down to
the folder being browsed is loaded and applied with **git's precedence**: each
governs its own directory and below, patterns match relative to that directory,
and the deepest file with something to say wins — so `!node_modules/keep` in a
subdirectory beats `node_modules/` at the root. The load is scoped to that chain
because an ignore file in a sibling subtree governs nothing in this listing. The
matcher (`web/src/features/sites/gitignore.ts`) covers the patterns that actually
appear in a web project's ignore file — comments, negation, directory-only,
anchoring, `*` / `**` / `?` — and is still deliberately not a full
reimplementation of git's rules: no character classes, no `.git/info/exclude`, no
global excludesfile. It is a display hint, never a security or correctness
boundary, so the worst a miss can do is show a file git would have hidden. It is
unit-tested (`gitignore.test.ts`), which is how the nesting bug that made the
site root sort as deep as a top-level folder was caught.

## 7. Deferred (not in this pass)

- **Terminal session recording / playback** — see [18](18-web-terminal.md). Still
  a privacy decision as much as a technical one.
- **Full git ignore semantics** — nesting and precedence are now implemented, but
  character classes, `.git/info/exclude`, and the global excludesfile are not.
- **True streaming archives** — folder download no longer leaves anything behind,
  but it still materialises a temporary zip, because `zip` cannot write to a pipe.
  Streaming a tar out of the download endpoint would remove the disk write, at the
  cost of a second content path through the broker.
- Multi-file/folder **upload as a single archived transfer** (today each file is
  one chunked PUT; `extract` covers the "upload a zip" case).

## 8. Definition of done

Backend: twelve broker capabilities (path-confined, run-as-user — except the
deliberately constrained root-run `file.chown` — and no-shell) with unit tests
for each security invariant; the `internal/files` service with the baremetal gate,
chunk-looping streaming, and the no-clobber conflict policy, unit-tested for the
gate, broker payloads, base64 round-trip, chunk boundaries, and every copy/move
refusal; the HTTP layer with RBAC, force-audited downloads, and OpenAPI metadata;
registry + bootstrap wiring.

Frontend: a **Files** tab (baremetal sites only, gated on `file.read`) with a
breadcrumb browser, a right-click menu, drag-and-drop upload with a cancellable
progress bar, file and folder download, new file/folder,
rename/chmod/repair-ownership/delete,
copy/cut/paste/duplicate, compress and extract, sortable columns, a hidden-files
toggle, multi-select with bulk actions, recursive search, nested gitignore-aware
listing, image preview, and a multi-tab CodeMirror editor with a diff view and
working keyboard shortcuts. The pure logic behind it — the ignore matcher, the
differ, and the path helpers — is covered by `vitest` (`npm test`), which runs in
CI before the bundle is built.

Live proof: **`deploy/docker/e2e/run-files.sh`** provisions a real baremetal site
(real Linux user) and drives the whole surface through the API against the real
broker — asserting that (1) every written/extracted/compressed file is owned by
the site user, not root; (2) content (including binary) round-trips; (3) a
`../../etc/passwd` traversal never touches the real file and is clamped under the
site root; (4) a created archive holds relative paths and compress→extract
round-trips; (5) name and content search work and a traversing search path still
cannot list `/etc`; (6) deliberately broken ownership is repaired back to the site
user; (7) copy runs as the site user, refuses to overwrite with a 409, takes a
free name when asked, moves without duplicating, and clamps a traversing
destination; (8) a folder download returns a valid zip holding the folder's
entries **and leaves no temporary archive behind**; and (9) a git-managed site is
refused with `not_baremetal`. Wired into CI.
