# 19 — Docker (module design)

Phase 5, in-core. Containers on the host — list, inspect, logs, stats, and
lifecycle — from the panel.

Docker is the sharpest privilege HeroPanel touches. Everything below follows
from one fact: **the Docker daemon socket is root**. Anyone who can reach it can
run `docker run -v /:/host --privileged` and own the machine, without ever
appearing in the panel's audit log. So the interesting content here is not the
feature list; it is where that privilege is allowed to live and what stops it
leaking.

---

## 1. Why this is not a `docker`-group module

[06](06-plugin-architecture.md)'s manifest example shows the Docker module
running as `heropanel` with `groups: [docker]`. **That is not what was built,
and the example is wrong.**

Membership of the `docker` group is root by another name. Putting `hpd` — the
network-facing process, the one exposed to the internet — in that group would
make a compromise of the HTTP layer a compromise of the host, and would retire
the privilege separation every other module in this panel is built on
([05](05-security-architecture.md), [ADR-0002](adr/0002-module-isolation-hybrid.md)).
A dedicated module user in the `docker` group is better — it is not `hpd` — but
it still creates a second root-equivalent principal outside the audited boundary,
which is precisely the thing the broker exists to avoid.

So Docker went where every other privileged operation already lives: **named
broker capabilities**. The socket is reachable only by the root broker. `hpd`
asks for `docker.container.stop` and gets an answer; it cannot ask for anything
that is not on the list. The peer-credential check, the policy gate, and the
hash-chained audit apply exactly as they do to `file.write` or `db.export`.

The cost is real — eleven capabilities instead of one import — and it is the
right cost. An operator who wants the panel to keep its hands off containers
entirely turns them off in policy, and the feature disappears rather than merely
being hidden in the UI.

## 2. The CLI, not the Engine SDK

Capabilities shell out to `/usr/bin/docker` with an argv array, never a shell
string. The official Engine SDK was considered and rejected: it is a very large
dependency for what amounts to a dozen calls, and using the CLI produces the
same argv shape every other capability in this broker already has — one reviewer
habit ("read the arguments") covers all of them.

## 3. The two boundaries

### 3.1 Ownership — the panel will not touch what it did not create

This is the equivalent of the File Manager's path confinement ([17](17-file-manager.md) §4),
and it is the whole security story of the module. Every **mutating** capability
first inspects the target and requires the label `io.heropanel.managed=1`.

Without it, `docker.container.stop` is a remote off-switch for every container on
the host: the site's database, a CI runner, the monitoring agent, another
tenant's workload. The caller chooses the name, so nothing else bounds it.

The check is a **live inspect**, not a name convention. A name is something the
caller supplies; a label is something the daemon reports. A container called
`hp-site1-web` that the panel never created would sail through a prefix check —
it cannot fake the label. Containers also carry `io.heropanel.site=<uid>`, which
is what attributes a container to a site in the UI.

**Reading is deliberately not restricted this way.** The listing shows every
container on the host, each flagged `managed`. Visibility is not the dangerous
half — mutation is, and that is guarded separately. An admin looking at a host
whose memory has vanished needs to see the container eating it even if the panel
did not start it; hiding it would make the panel lie about the machine it
administers.

### 3.2 Flag injection — what an argv array does *not* fix

An argv array defeats *shell* injection, and every capability here builds one.
It does nothing about a value that is itself a flag: a container named
`--privileged`, or an image called `-v=/:/host`, is parsed by docker as an
option, not an operand.

So every user-supplied value is matched against an allowlist pattern that
**cannot begin with `-`**, and `--` terminates option parsing wherever docker
supports it. Both, not either — the pattern is the guarantee, the `--` is the
belt.

## 4. Permissions

Two, and neither is `site.write`: **`docker.read`** (containers, logs, stats,
images) and **`docker.write`** (start, stop, restart, remove, pull). Being able
to edit a site is a much smaller grant than being able to stop the container
serving it, and the container view is host-wide rather than scoped to a site,
so folding it into the site permissions would have widened both.

Reading a container's **logs is force-audited**. Container logs routinely carry
connection strings, tokens and customer data; handing them over is a disclosure
worth the same log line a file download gets.

Pulling an image is a **write**, not a read: it places someone else's code on the
host and consumes disk.

## 5. Two decisions that look like details

**`docker rm` never passes `--volumes`.** Removing a container is routine;
removing its data is not. A delete button that also destroyed the database volume
would be indistinguishable from the safe one right up until it was not. The UI
says the volumes are kept.

**The stop grace is 10 seconds, and that number is coupled to the HTTP layer.**
hpd's write timeout is 30s. A container that ignores `SIGTERM` — `sleep`, and
plenty of real images — consumes the entire grace, so a 30s grace consumed the
entire request budget: the connection closed before the handler answered and the
operator saw a failed request for an operation that had in fact succeeded. The
live e2e caught it as a bare `000` from curl. 10s is docker's own default and
leaves the budget intact; `run-docker.sh` now asserts the timing so it cannot
regress.

## 6. Absence is a state, not an error

`docker.info` reports an unreachable daemon as `available: false` with the
daemon's own message, and never as an error. "Docker is not installed" and
"permission denied" need completely different fixes and only the daemon can tell
them apart, so the reason is passed through to the operator verbatim.

The module registers with the capability registry **only when a daemon actually
answers**, so on a host without Docker the feature greys out instead of offering
buttons that cannot work. Presence is cached for 30 seconds: every page asks, and
the answer changes about as often as a package is installed.

Stats are sampled **one at a time, never streamed**. `docker stats` without
`--no-stream` never returns, and a capability that never returns holds a broker
connection open forever; the client polls instead.

## 7. Creating containers — where the hardening lives

Everything above reads or acts on containers that already exist. Creating one is
where the module starts placing workloads on the host, so it is where the
hardening is, and the rule is: **the caller describes what it wants in typed
fields, and the broker decides the argv**. hpd never hands over a flag.

Most of what is refused is refused *by construction* — there is no input that
produces it:

- `--privileged`, `--cap-add`, `--device`, `--security-opt`, `--userns`, and the
  host `--pid`/`--ipc`/`--uts` namespaces have **no field**.
- **Host bind mounts are unrepresentable.** Only *named volumes* mount, and the
  volume-name pattern admits no `/` — so `-v /:/host` or mounting the docker
  socket is not rejected, it cannot be named. This is the single most important
  one: a host bind mount inside a container is a complete escape to host root.
- **Ports publish to `127.0.0.1` only**, and the bind address is not a field.
  Docker writes its firewall rules ahead of the host's, so a container published
  on `0.0.0.0` is reachable from the internet even when the host firewall denies
  the port — which is how databases end up exposed. A reverse proxy on the same
  host fronts it over loopback.
- `no-new-privileges` is **added**, so a setuid binary in the image cannot raise
  privilege beyond what the container started with.

Environment travels to docker **through stdin as an env-file, never argv**: argv
is world-readable through `/proc`, and a generated database password is exactly
what would be there. A newline in a value is refused, because it could forge an
extra line of the env-file — injection by another door.

## 8. A shell inside a container

`docker exec` is a long-lived bidirectional byte stream, the same shape as the
web terminal, so it **reuses the terminal's machinery outright** — the same PTY,
the same connection upgrade ([18](18-web-terminal.md) §2), the same pump. A
second streaming protocol for the same thing would have meant a second place for
the framing to be subtly wrong.

The authorization differs, and that is the point: a site terminal is bounded by
*which Linux user*, a container exec by *which container*. A shell inside a
container the panel did not create would bypass every other refusal in this
module — you could stop the process from within — so the ownership label is
checked exactly as it is for stop and remove. The shell program is an allowlist
(`/bin/sh`, `/bin/bash`, `/bin/ash`), because it is an argv element in a
privileged command, and the endpoint takes `docker.write` rather than
`docker.read`: a shell can stop the process, read its secrets and edit its data.

Container sessions are **audited but not transcribed**. Recording is tied to a
site's retention and permissions ([18](18-web-terminal.md) §6), and a container
shell has no site to hang that on; inventing one would put transcripts where
nothing sweeps them. The audit row records who opened a shell in which container.

## 9. Compose stacks, and the honest boundary

A compose stack is different in kind from a container the panel builds. The
create capability hardens *because it builds the argv*; a compose file is
user-authored YAML and can ask for `privileged: true`, a host mount,
`network_mode: host` — anything compose understands. The broker cannot make that
safe without parsing and rewriting arbitrary compose, which is a losing game. So
compose is treated as what it is: an **advanced, explicit escape hatch**.

What the broker still guarantees is narrower but real: the project name is
validated (no flag, no path); the operator's file is written to a
**broker-chosen** directory, never a path hpd names; and the stack is
**labelled**, so tear-down and the ownership boundary reach its containers. That
last one takes work — `docker compose up` has no per-container label flag, so the
stack is deployed with a generated override file that stamps the managed label on
every service. The service names for that override come from `docker compose
config --services`, so the broker lets compose parse the YAML and only weaves in
labels. Tear-down removes containers and networks but never volumes, and is
refused for a stack whose containers do not carry the managed label.

## 10. One-click apps

An app is a **labelled compose stack**, so the Apps module (`internal/apps`)
adds no privilege — it sits entirely on the compose capability. What it adds is
the parts that make a one-click deploy safe rather than merely quick:

- **Generated secrets.** A secret field is generated with `crypto/rand`, never
  taken from operator input and never defaulted in the template. It is returned
  once, on success, because it is not stored in a form the panel can hand back —
  and it is deliberately kept out of the audit log.
- **Memory feasibility, up front.** Each template declares a realistic memory
  floor; the catalog compares it against the host's `MemAvailable` and marks an
  app the host cannot run *before* it is chosen, so a deploy is refused with a
  clear message rather than discovered by the OOM killer. Unknown memory (a
  non-Linux host) allows the deploy rather than blocking every one.
- **Loopback and volumes by convention.** Every template publishes to
  `127.0.0.1` and keeps data in a named volume, so an app is fronted by a reverse
  proxy and survives a redeploy.

The templates are small, real compose files reviewed here rather than fetched
from upstream, so what the panel deploys is what is read. The catalog includes
the phase's exit-criteria apps (Ghost, Uptime Kuma) and others (NocoDB,
Vaultwarden, Gitea, Postgres, Redis, a demo Nginx).

## 11. The deferred set, now built

The four items this module first shipped without are now in, each keeping the two
boundaries above rather than working around them.

**Image removal and pruning.** Images carry no per-panel label — the same
`postgres:16` backs a panel app and, plausibly, something run by hand, so there
is no honest way to call one copy "ours". The ownership boundary is therefore
absent here and a different guarantee takes its place: docker itself refuses to
remove an image a container (running *or* stopped) still references, and that
refusal is passed straight through rather than papered over. `docker.image.remove`
never orphans a running app; `docker.image.prune` is dangling-only unless `all`
is explicitly asked for, and even then docker's in-use check still protects
anything a container needs.

**Volumes and networks, first-class.** `docker.volume.inspect` returns a volume's
record *and the containers that mount it* — the latter from a live `ps` filtered
by the volume, because a name proves nothing and the daemon's own view is the
answer. `docker.network.inspect` passes docker's payload, which already carries
the connected containers. Both are read-only and, like every read in this module,
deliberately *not* ownership-gated: an operator deciding whether a volume is safe
to delete must see everything attached to it, panel-owned or not.

**Live log streaming.** The one-way twin of the container shell. It reuses the
same connection upgrade, but a log follow only ever produces output, so it runs
as a plain child whose stdout and stderr are interleaved into one pipe and
streamed until either side hangs up — no PTY, no input. Authorization matches the
*polled* read, not exec: following logs is allowed on any container (an admin
diagnosing a host must read whatever is misbehaving), gated by `docker.read` and
force-audited because logs carry secrets.

**Reverse-proxy auto-wiring.** An app publishes on `127.0.0.1:<port>` and is not
reachable from the internet until it is given a domain. Exposing it creates a real
**proxy site** whose vhost reverse-proxies to the app — so the app inherits the
entire site machinery (domains, TLS, redirects, suspension) for free, and is
managed on the Sites page from then on. The link is stored as `sites.app_project`;
the upstream is **resolved live at render time** from the app's published loopback
port, never baked in, so an app redeployed on a new port is followed and a
torn-down app simply stops resolving (the vhost falls back to a static wall rather
than proxying to a dead port). Port allocation reads docker's live published set
rather than an in-memory counter, so a fresh deploy never collides with a running
one — including across a restart, which the old counter could not survive.
Exposing is a `site.write` operation, because it stands up a site; the app itself
keeps running on loopback, and unexposing removes only the front door.

## 8. Definition of done

Broker: eleven capabilities in `broker/capabilities/docker.go`, all policy-gated
and audited. Unit tests cover the ownership refusal for **every** lifecycle verb
(and that the refusal happens *before* any action is run), that flag-shaped
values are rejected without executing anything, that an absent daemon is reported
rather than thrown, that a log tail is clamped whatever is asked, that stats never
stream, and that a remove never carries `--volumes`.

hpd: `internal/docker` — daemon probe with caching, CLI-output parsing (a
malformed row costs that row, not the page), and a verb→capability map that
refuses an unknown verb before it reaches the broker.

UI: a top-level **Docker** page gated on `docker.read`, containers and images,
per-container logs, an opt-in usage column, and lifecycle buttons rendered only
for containers the panel manages — matching what the broker will actually allow,
so the buttons do not lie.

Live proof: **`deploy/docker/e2e/run-docker.sh`**, against a **real dockerd**
(80 assertions, in CI). It starts one labelled container and one the panel knows
nothing about, and asserts that all four lifecycle verbs are refused with 403 on
the foreign one **and that it is still running afterwards**, that it is
nonetheless listed, that the managed one obeys and answers inside the HTTP write
budget, that logs and stats come back, that a flag-shaped name is refused, and
that the action lands on the broker's hash chain. It then **creates** a container
and asserts it has no host bind mounts, publishes only to loopback, is not
privileged and carries the memory limit; that four host-path volumes are refused
through the API with **no container created**; and that an environment secret
never reaches the broker log. It opens a **shell** in a managed container and
confirms one in a foreign container is refused. It **removes** an image and
confirms docker's in-use refusal reaches the caller, and **prunes** dangling
layers without touching an image a container still needs; **inspects** a volume
and gets back the container that mounts it, and inspects an unmanaged network to
prove reads are not ownership-gated; and **follows** a container's logs live,
upgrading to a WebSocket and landing on the broker's audit chain. And it runs the
**full app pipeline** — deploy, confirm running and loopback-published, then
**expose it on a domain** (a proxy site linked to the app, resolving its upstream
live, refusing a second front door) and **unexpose** it (the app keeps running,
only the vhost is dropped) — and confirms a generated app secret is not written to
the broker log.

---
Back to [index](README.md).
