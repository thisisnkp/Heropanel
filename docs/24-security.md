# 24 — Security suite

Host hardening: an **nftables firewall with a self-reverting apply**, **ClamAV
malware scanning with quarantine**, a **panel IP-allowlist**, **Fail2Ban**
surfacing, and **passkeys (WebAuthn)** for the panel's own login. In-core,
satellite-ready like its siblings.

Back to [index](README.md).

## 1. The firewall reverts itself

A firewall carries a danger no other capability does: a wrong rule can lock the
operator out of the very box they are configuring, with no way back in. So an
apply is **two-phase and self-reverting**. `firewall.apply` snapshots the live
`nft` ruleset, applies the rendered one, and leaves the snapshot *pending*. The
apply returns a token and a deadline; unless `firewall.confirm` is called with
that token before the deadline, the change **reverts itself** to the snapshot.
The operator who locked themselves out simply waits.

The timer lives in unprivileged npd, not the broker: npd runs **on the box**,
so the local npd→broker socket is unaffected by a rule that cuts off *remote*
access — npd can always fire the revert. The deadline is persisted, so an npd
that restarted mid-window still honours it (a startup + ticker guard), and a
stale confirm (from an already-reverted apply) is refused. The rendered ruleset
is **default-drop** but always keeps established/related and loopback, so even a
ruleset that forgets to allow anything leaves existing connections and local
traffic alive while the timer counts down. The broker owns only three tiny
verbs (snapshot+apply, discard, restore); rendering, the timer and the token
live in npd. Comments are operator metadata and are deliberately not rendered
into the ruleset — one less injection surface.

Deferred (honest): IPv6 source rules (`ip6 saddr`; the renderer is v4 today),
port ranges and named sets, and an OS-level belt-and-suspenders timer
(`systemd-run`) in addition to npd's.

## 2. Malware scanning + quarantine

**Two scanners, one quarantine.** `malware.scan` runs **ClamAV** (`clamscan`)
and `maldet.scan` runs **Linux Malware Detect**, both over a confined site tree,
both returning the same shape — each detection as a path and a signature — so
everything downstream is engine-agnostic.

Two of them because they look for different things. ClamAV's signatures are a
general antivirus corpus; maldet's are built from what actually lands on shared
hosting: web shells, injected PHP droppers, obfuscated backdoors, harvested from
edge intrusion data and from ClamAV's own misses. On a panel whose job is
hosting other people's PHP, the second list is the one that catches the thing
that got in. A clean result from one is therefore **not evidence about the
other**, which is why every scan row records which engine produced it
(migration `0046`) — a history that does not name the scanner cannot be read.

maldet has a quarantine of its own and it is switched **off**, explicitly, on
every scan's command line (`--config-option quarantine_hits=0,quarantine_clean=0`)
rather than being left to whatever the host's `conf.maldet` says. Two
quarantines would mean a file the operator can see in one and not the other, and
a "restore" that puts back something the other tool already moved.

**maldet is installed by the panel, because nothing else will.** It is in no
distribution repository, so `system.provision` cannot resolve it to a package
name; `maldet.install` downloads its tarball and runs its installer. That is the
one place in NexPanel that fetches code and executes it as root, and it is
bounded three ways: the **host is a constant** in the broker (an
operator-supplied URL would turn a config file into arbitrary root execution),
the download **path** must be a plain `/downloads/<name>.tar.gz`, and a
**configured SHA-256 is enforced** before anything is unpacked. The observed
hash is always returned and shown in the UI, so an operator who installs once
unverified can pin it and have every install after that checked.

**Honest limit, stated because it is real:** this is TLS-and-a-pinned-hostname
trust, weaker than the signed, key-pinned chain the panel uses for its own
releases ([26](26-self-update.md)) and for marketplace modules. It is the
strongest thing maldet's distribution allows — rfxn publishes no signature and
no stable checksum for the current tarball. During first-run setup the install
is **attempted and its failure is not fatal**: every other baseline component
comes from the host's own repositories, and a third-party download being briefly
unreachable must not hold a whole install hostage. The gap is visible — the
malware screen shows maldet as not installed, with a one-click Install.

The scan itself returns each detection (path + signature). Scanning is read-only and can run
anywhere confined; the dangerous verb is `malware.quarantine`, which **moves**
a detected file out of its site tree into a root-only holding area
(`/var/lib/nexpanel/quarantine`, 0600 root) where it can neither be served nor
executed — an infected file that stays in place is still a live threat. The
quarantine path is validated to lie within the named site, and derived from a
ULID npd supplies, so it cannot be aimed at an arbitrary file. Restore (a false
positive) returns the file to its original path and owner; delete removes it.
`site_uid` and `original_user` are stored denormalised, so the quarantine
history survives the deletion of the site the file came from.

## 3. Passkeys (WebAuthn)

Passkeys are a **hand-rolled, dependency-free** WebAuthn verifier (stdlib
crypto + a ~200-line CBOR reader). We verify the thing that actually
authenticates a login — the **assertion signature** (ES256/RS256), over
`authenticatorData ‖ SHA256(clientDataJSON)` — plus the challenge, the origin,
the RP-ID binding, user presence, and **signature-counter clone detection**.
We deliberately do **not** verify attestation statements: the panel needs a key
it can trust for future assertions, not proof of which vendor made the token
(a common, documented posture that also lets any passkey work). Registration
happens while signed in (like MFA setup); login is passwordless — enter the
email, sign the challenge, get a session, with no shared secret to phish. The
verifier is proven end-to-end by a **virtual authenticator** in Go tests
(register→assert round trip, plus rejection of a tampered signature, a replayed
challenge, a wrong origin, a rolled-back counter, and a different key) — pure
crypto, so a Go integration test is the honest end-to-end, not a container.

Passkeys are enabled only when `webauthn.rp_id` (and origin) are configured —
the relying-party id must match the panel's domain exactly and cannot be
guessed safely.

## 4. Panel IP-allowlist and Fail2Ban

A **panel IP-allowlist** (`security.panel_ip_allowlist` /
`NP_PANEL_IP_ALLOWLIST`) restricts the panel/API to a set of CIDRs at the
application layer, sitting right after RealIP so a disallowed address never
reaches a handler or an audit entry — defence in depth alongside the host
firewall, and it works even behind a managed load balancer where nftables is
not the operator's to change. Empty = open (the default); a malformed entry is
dropped rather than silently widening access.

**Fail2Ban** is surfaced read-only (jails and their banned IPs, parsed from
`fail2ban-client`) with manual ban/unban, each taking a validated jail name and
a parsed IP.

## 5. Definition of done

Broker capabilities unit-tested with the fake runner/fs: the firewall snapshots
then applies, refuses a second apply while one is pending, cleans up on a
rejected ruleset, and confirm/rollback are idempotent; malware scan confines
its path and parses `FOUND` lines, quarantine moves-and-locks-down (0600 root)
and refuses traversal, restore/delete act only on quarantined ids; fail2ban
refuses a shell-shaped jail name and a non-IP. The firewall service's timer,
guard, stale-token refusal and restart recovery are unit-tested with a mock
gateway and a controllable clock. The WebAuthn verifier and the passkey service
round-trip are proven with a virtual authenticator. The IP-allowlist middleware
is table-tested.

Live proof: **`deploy/docker/e2e/run-security.sh`** (in CI) — the two
demonstrable exit criteria end to end. The firewall: an applied rule is live in
`nft`, then, left unconfirmed past its window, **auto-reverts** to the previous
ruleset; applied again and confirmed, it **sticks**. Malware: a **real
clamscan** (with a one-line custom EICAR signature — no database download)
detects the EICAR test file in a site tree; quarantining it **removes it from
the site** and holds it 0600-root where the site user cannot read it; restoring
returns it to the site user. Every capability is on the broker's audit chain.
WebAuthn's exit criterion ("WebAuthn login works") is met by the Go integration
tests, which drive a full register→passwordless-login through the service.

## SSH hardening

A panel-owned sshd drop-in (`/etc/ssh/sshd_config.d/50-nexpanel.conf`),
rendered by npd from a small **fixed, validated** field set — port, root-login
policy (`no`/`prohibit-password`/`yes`), password auth (default off = key-only),
public-key auth, an auth-try budget, an optional login allow-list — plus a
block of **fixed hardening** that is never a knob (empty passwords off,
keyboard-interactive/challenge-response off, X11 and agent forwarding off, a
tight login grace and client-alive). The broker's **`ssh.harden`** writes the
one pinned path, **config-tests the whole config with `sshd -t` before it can
take effect**, and **reloads (not restarts)** so the operator's current session
survives — the same reload-first discipline as `php.write_pool`; a config sshd
would reject is rolled back to the prior drop-in and never reaches a reload. A
**self-lockout is refused**: disabling both password and key auth is a 400.
`ssh.status` reads the effective config back with **`sshd -T`** — what the
daemon would actually enforce, not what a file appears to say.

Live proof (`run-security.sh`): hardening applied, the drop-in written; `sshd
-T` then shows the new port, `PermitRootLogin no`, `PasswordAuthentication no`
and `PermitEmptyPasswords no` in effect; disabling both auth methods is refused
with a 400; `ssh.harden` and `ssh.status` are on the broker's audit chain.

## Automatic security updates

On Debian/Ubuntu, a panel-owned `unattended-upgrades` apt drop-in
(`/etc/apt/apt.conf.d/52nexpanel-unattended`), rendered by npd from validated
options — enable, security-origin-only (default), automatic reboot + reboot
time. The broker's **`updates.configure`** writes the one pinned path,
**validates it with `apt-config dump`** (a malformed apt.conf is caught and
rolled back before it can wedge apt), and enables the apt timers so the policy
actually fires. **`updates.status`** reads the **effective, merged** apt config
back with `apt-config dump` — the honest source of truth across every
`apt.conf.d` file — and reports whether the drop-in is present and the
`unattended-upgrades` tool installed. Rocky/Alma (`dnf-automatic`) follows the
panel's Debian-first posture.

Live proof (`run-security.sh`): the policy is applied, the drop-in written
scoped to the security origin; `apt-config dump` then shows
`APT::Periodic::Unattended-Upgrade "1"` in effect, disabling flips it to `"0"`,
and both capabilities are on the broker's audit chain.

## Web application firewall (per site)

A per-site toggle turns on **ModSecurity + the OWASP Core Rule Set** for a
site's OpenLiteSpeed vhost. The state is a `waf_enabled` column on the site; the
vhost render emits a `module mod_security` block referencing a pinned rules file
(`/etc/nexpanel/waf/main.conf`), and — because OLS only activates the module
when it is declared at **server** level — a server-level `module mod_security {
ls_enabled 1 }` is emitted whenever any site has the WAF on. Enabling a site's
WAF first writes the rules file through the broker's **`waf.provision`**
(npd renders the content; the broker writes the one pinned path).

The rules file is shaped for **libmodsecurity v3** (what OLS embeds), which is
stricter than the v2 the distro's `owasp-crs.load` targets: it supports only
`Include` (not `IncludeOptional`), and a single unsupported directive fails the
*entire* file — so the base engine settings are stated inline and the CRS is
pulled in by its concrete pieces (`crs-setup.conf`, then every rule), never via
the distro load file. `SecDefaultAction` is left to `crs-setup.conf` (which may
set it only once). The WAF module (`ols-modsecurity`) and the CRS
(`modsecurity-crs`) are host prerequisites the installer provides.

Live proof (`run-waf.sh`): with the WAF **off**, a SQL-injection probe in the
query string is allowed (200); with it **on**, the *same* request is **blocked
(403)** by the CRS while a normal request still serves (200); disabling it
allows the attack through again. `waf.provision` is on the broker's audit chain.

## File-integrity monitoring + host audit scanners

**FIM (AIDE):** a baseline of the panel's security-critical paths (configs,
`/etc/ssh`, the `npd`/`np-broker`/`sshd` binaries) built by `fim.init`;
`fim.check` compares the filesystem against it and reports what was added,
removed or changed. It shells out to real AIDE (fixed argv) and parses the
summary — carefully ignoring AIDE's bare detail-section headers that repeat the
count labels without a number. `fim.status` reports whether a baseline exists.

**Audit scanners (`audit.scan`):** **rkhunter** (rootkit hunter) and **lynis**
(system auditor) run on demand and their output is parsed — rkhunter's warning
count, lynis's hardening index + warning/suggestion counts, plus the report.
These are host scanners, distinct from the two file scanners in §2: rkhunter and
lynis add rootkit heuristics and a configuration audit rather than more
signatures. These scans take minutes, so
their client-side broker timeout and the server write timeout
(`NP_SERVER_WRITE_TIMEOUT`) are raised accordingly.

Live proof (`run-fim.sh`): a FIM check without a baseline is refused (409);
after init the check is **clean**; **tampering a watched file makes the next
check report `changed=true`** with a non-zero count; a real **lynis** audit
returns a parsed hardening index and a real **rkhunter** scan returns its
warning count; an unknown scanner is refused (400); every capability is audited.

Intentionally NOT built: **CrowdSec** — Fail2Ban (above) already provides the
intrusion-prevention baseline and CrowdSec overlaps it (an either/or, not
additive); it can be added later behind the same `cscli`-surfacing pattern.

Honest gaps / deferred: full **SPF/DMARC** rejection daemons (mail §), host-wide
FIM coverage, `dnf-automatic` for Rocky/Alma. Each is a substantial feature in
its own right, sequenced as a follow-up rather than half-built here.

---
Back to [index](README.md).
