# 18 — Web Terminal (module design)

Phase 4, in-core. An interactive shell on a site, in the browser, running as
that site's Linux user. xterm.js in the UI, a WebSocket to `hpd`, an upgraded
broker connection behind it, and a real PTY hosted by the root broker.

This is the most powerful thing the panel exposes — it is arbitrary command
execution by design — so the interesting content here is the chain of custody
between the browser and the shell, and what bounds it at each hop.

---

## 1. Why the broker has to own the PTY

`hpd` runs unprivileged, as the `heropanel` account. It **cannot** become another
user, so it cannot start a shell as `hps1`. Only the root broker can. That single
fact determines the whole architecture: the PTY is allocated and owned by
`hp-broker`, and `hpd` is a pipe between that and the browser.

The shell is started as a fixed argv — `runuser -u <site-user> -- <shell> -l` —
with no shell string ever constructed or interpolated. `runuser` drops privileges
before the login shell is exec'd, so the shell never runs with root's identity
even for an instant.

## 2. Streaming over a request/response broker

Every other capability is one-shot: one request, one response, connection closed.
A terminal is a long-lived bidirectional byte stream, which does not fit that
shape — and rather than bolt on a second protocol or a second socket, the
connection **upgrades**:

1. The client sends a normal `Request` for `terminal.open`.
2. The broker authorizes it (below). If it accepts, it replies with a `Response`
   carrying `stream: true`.
3. From that frame on, both ends exchange `StreamFrame`s (`in`, `out`, `resize`,
   `exit`, `error`) on the same connection until it closes.

Everything that guards a normal request still applies, because it all happened
before the upgrade: the `SO_PEERCRED` peer check, the token handshake, the
capability policy, and the audit chain. The existing 1 MiB frame cap still
applies too, which is why PTY reads are chunked at 32 KiB.

Because a session is long-lived and mostly idle, it gets its own dedicated
connection (`Client.OpenStream`) and the handshake deadline is cleared once the
stream is established — a read deadline would kill an idle terminal mid-use.

## 3. What authorizes a session

`Broker.OpenTerminal` does not go through the capability registry (whose contract
is request → result), but it goes through the *same* authorization path:

1. **Deny by default** — `terminal.open` must be enabled in policy.
2. **Username validation** — the strict Linux-username allowlist.
3. **Root is refused outright.** Policy path roots already keep sessions inside
   `/srv/heropanel/sites`, but a terminal is the one place where "which user" is
   the entire security question, so the refusal is stated explicitly rather than
   left implied.
4. **Working directory clamped** — via `capability.ConfinedPath`, the *same*
   implementation the File Manager uses (§4 of [17](17-file-manager.md)). A `cwd`
   of `../../../../etc` resolves to `<site-root>/etc`, never `/etc`. If the
   clamped path does not exist, the session falls back to the site home rather
   than failing — still confined either way.
5. **Audit intent, then outcome** — an intent row is written before anything
   privileged happens, and the account the session ran as is recorded.

On the HTTP side the endpoint takes its own permission, **`terminal.use`** — not
`file.write`. Being able to edit a file is a much smaller grant than being able
to run arbitrary commands as that user, and the two should not be bundled. The
handler is force-audited (a WebSocket upgrade is a GET, which the edge would not
audit by default).

## 4. Wire shape, and why binary

Terminal payload travels as **binary** WebSocket frames in both directions.
That is not an optimisation — it is correctness. PTY output is arbitrary bytes
and a 32 KiB read can land in the middle of a UTF-8 sequence; encoding that as a
JSON string would replace the split bytes with U+FFFD and corrupt the stream.
Control messages (`resize` in; `exit`/`error` out) travel as JSON **text** frames,
where they cannot be confused with payload. xterm.js buffers partial escape and
UTF-8 sequences itself, so frame boundaries are safe.

The broker stream is closed when the browser disconnects, and closing it kills
the **process group** — not just the shell — so a backgrounded process cannot
outlive the tab that started it. The e2e asserts nothing is left running.

## 5. The UI

A **Terminal** tab on the site workspace, shown only when the site has a Linux
account and the operator holds `terminal.use`. The session is opened on an
explicit *Start session* click rather than on tab mount: opening a shell is an
audited, privileged act and should be deliberate, not a side effect of clicking
through tabs. The panel states plainly which account the shell will run as.

xterm.js is **code-split** — it and its addons load only when a session is
started, so the site workspace does not pay for it on every page load. The theme
is derived from the app's CSS custom properties, so the terminal follows the
light/dark toggle. Resizes are observed and debounced through `requestAnimationFrame`
and sent as a control frame, so dragging a window does not flood the socket.
Like CodeMirror, xterm styles elements through the CSSOM rather than by parsing
style attributes, so it runs under the strict `default-src 'self'` CSP with no
`'unsafe-inline'` exception.

**Copy and paste** need explicit handling, because a terminal cannot use the
usual shortcuts: `Ctrl-C` is SIGINT and must stay that way. So `Ctrl/⌘-Shift-C`
copies the selection and `Ctrl/⌘-Shift-V` pastes, the long-standing terminal
convention, along with `Shift-Insert`. Both are bound through xterm's custom key
handler and return `false`, which is what stops the keystroke from also reaching
the shell as a control character. Plain `Ctrl/⌘-V` still works untouched —
xterm's hidden textarea receives the browser's native paste event — so the
Clipboard API path is only for the shifted shortcut and the menu, and when a
browser blocks it the operator is told that plain paste works rather than being
left with a dead key. The same right-click menu component as the file manager
provides Copy / Paste / Select all / Find / Clear scrollback.

**Find, zoom, fullscreen.** `Ctrl/⌘-Shift-F` opens a search bar over the
scrollback (`@xterm/addon-search`); a 5000-line scrollback is only useful if you
can search it. Font size is adjustable from the toolbar and **persisted in
localStorage** — whoever needed 16px once needs it every time — and a change
refits and re-sends the window size, or the shell keeps wrapping at the old
width. Fullscreen expands the terminal over the page and `Esc` leaves it, since
the expanded terminal has just covered the button that would otherwise be the
only way out.

## 6. Session recording

The audit chain records *that* a session happened and as whom. A recording
records what was done in it — the transcript for the most powerful thing the
panel hands out.

**Format.** asciicast v2, the format asciinema uses: a JSON header line then one
JSON array per event (`[time, "o"|"i"|"r", data]`). It is a real, documented
format rather than something invented here, so a recording stays readable by
tools that outlive this software — which matters for an audit artifact. The
panel's own player replays it into xterm.js, the same emulator the live terminal
uses, so playback renders exactly as the session did and no second player library
is shipped. Input events are drawn dimmed inline, because a transcript showing
only output leaves you guessing what was typed.

**Storage.** Files on disk under `terminal.recording.dir`, one directory per day
per site, mode `0600`; only metadata is in the database (`terminal_recordings`).
Terminal output is unbounded and a row that can grow to megabytes is the wrong
shape for the datastore the panel also uses on its hot path. A recording is
capped at 8 MiB and marked `truncated` past that, with a visible last event, so
playback ends with an explanation instead of simply stopping. Recording failures
never fail the *session*: the shell is what the operator asked for, and a full
disk must not become an outage — the audit row then says `recorded: false` rather
than leaving it ambiguous.

**Retention** defaults to 30 days, swept hourly, with a sweep on startup so a
panel restarted after an outage does not keep transcripts past their policy.
`expires_at` is stored rather than computed, so lengthening retention does not
resurrect recordings a shorter policy would already have removed.

**Access.** Two permissions, neither of them `terminal.use`: reading a transcript
of what someone *else* typed is a much larger grant than opening your own shell,
and `terminal.recordings.delete` is separate again — destroying an audit artifact
is exactly what an operator under scrutiny would want, so it is grantable to
fewer people. Downloading a recording is force-audited: "who watched whose
session" is precisely the question an audit log exists to answer. The terminal
tab says plainly, *before* the session starts, that it is recorded.

Recordings are therefore a **top-level destination** (`/recordings`), next to the
audit log rather than inside the feature they audit — listing every session
across every site, filterable, each row linking to the site it ran on. That
placement is the whole point and it was originally got wrong: the list lived only
inside a site's Terminal tab, which is gated on `terminal.use`, so the one role
the separate permission exists for — an auditor with `terminal.recordings.read`
and deliberately *no* shell access — could not reach a single recording. Every
backend test passed; the permission was right in the API and wrong in the
navigation, which is a gap only a browser test can see, and `web/e2e/recordings.spec.ts`
now pins it. The site's Terminal tab keeps its own panel as a convenience view
for whoever is already there.

Because `site_id` is never exposed, the site's uid and name are joined into the
listing: "a session on site #7" is not something anyone can act on. The join is a
LEFT one — a site can be deleted while its transcripts are still inside the
retention window, and that is precisely when they matter, so those rows list as
*deleted site* instead of vanishing or failing the page.

### Why redaction keys off ICANON, not ECHO

Recordings capture keystrokes as well as output. That is what makes them useful —
and it is also how a panel accidentally becomes a plaintext password store, since
`sudo`, `mysql -p` and `ssh` all read secrets from the terminal.

A password prompt hides input by clearing the terminal's `ECHO` bit. The obvious
rule — "redact while ECHO is off" — is **wrong**, and the live e2e is what proved
it: an interactive shell runs with ECHO off almost all the time, because readline
puts the terminal in raw mode and echoes each character itself so it can do line
editing. Under that rule every command anyone typed came back as `[redacted]`.

The actual signature of a password prompt is ECHO off *while still canonical*:
`read -s` and getpass(3) clear `ECHO` and leave `ICANON` set, because they want
the kernel's line discipline to do the reading, just silently. readline clears
both.

| Terminal state | Meaning | Recorded? |
|---|---|---|
| `ECHO` on | a plain program | yes |
| `ECHO` off, `ICANON` off | readline / vim — the program echoes it itself | yes |
| `ECHO` off, `ICANON` on | a password prompt | **redacted** |

Only the broker can see this, because only the broker holds the PTY, so it
reports the state to hpd over a `StreamEcho` control frame whenever it changes.
One marker is written per redacted *run*, not per keystroke — one per keystroke
would leak the password's length, which is most of what redaction protects.

The honest limit: this redacts **input**. If a program prints a secret to the
terminal itself, that output is recorded, because the recording cannot tell a
secret from any other output — and stripping output would make transcripts
useless.

## 7. Deferred

- **Multiple concurrent tabs per site** in one browser view, and reattaching to a
  session that outlives the page (today a disconnect ends the session, which is
  the safe default).
- **Idle timeout / max session duration** as policy knobs.
- **Search within a recording** and jumping to a matched moment; today playback
  is play/pause/seek/speed.
- **Server-side search across all history.** The cross-site page loads the 200
  most recent sessions and filters those in the browser; older ones are reached
  by paging. The page says so rather than letting a filter's "nothing matches"
  be read as "no such session" — but a real audit query (by person, by date
  range, across everything) belongs in the API.

## 8. Definition of done

Broker: a PTY implemented on Linux ioctls with no third-party dependency
(`broker/pty`, with a non-Linux stub so the repo still builds everywhere), a
stream upgrade in the transport, and `OpenTerminal` behind policy + audit.
Unit tests cover every refusal — policy disabled, root, invalid username, a home
outside the policy roots — and that intent is recorded before the session starts.
`broker/transport` pins the wire shape of a refusal: it stays a plain `Response`
and must **not** flip the connection into stream mode, or the client would be
handed a stream to a session that was never opened and the refusal would never be
seen. `broker/pty` is unit-tested on Linux against a real PTY — I/O round-trip,
the window size the ioctl actually set, resize, the child's exit code, and that
`Close` kills a *backgrounded* child, not just the process the broker started.

hpd: `internal/terminal` (site resolution + provisioning gate), a streaming
broker client, and a WebSocket endpoint gated by `terminal.use` and force-audited,
with the broker stream opened *before* the upgrade so failures are clean JSON
errors rather than opaque WebSocket closes.

Live proof: **`deploy/docker/e2e/run-terminal.sh`**, driven by a real WebSocket
client, asserts that the shell reports the **site user and never root**, starts in
the site home, round-trips input and output, clamps a traversing `cwd` away from
`/etc`, refuses an unauthenticated upgrade (401), leaves **no orphaned processes**
after disconnect, and records `terminal.open` as a success on the broker's hash
chain. Wired into CI.
