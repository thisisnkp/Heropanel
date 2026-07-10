# 05 — Security Architecture

Security is the product's spine, not a feature. The design assumes the network-facing code **will** eventually have a vulnerability, and ensures that a compromise there does not equal root or cross-site data exposure.

## 1. Threat Model (summary)

| Adversary | Capability assumed | Primary defense |
|-----------|--------------------|-----------------|
| Remote unauthenticated attacker | Reaches `:8443` | Authn, rate limiting, brute-force lockout, WAF |
| Authenticated low-priv user (client/reseller) | Valid session | RBAC scoping, per-site isolation, quota enforcement |
| Compromised `hpd` process (RCE in API) | Runs as `heropanel`, can talk to broker | **Privilege broker** allowlist — cannot get arbitrary root |
| Malicious hosted site (attacker owns a site) | Code exec as its Linux user | Per-site users, cgroups, mount/namespace isolation, no shared tmp |
| Compromised module | Runs as its module user | Module least-privilege, still must go through broker for root ops |
| Insider / stolen admin creds | Full admin | MFA, WebAuthn, audit log (hash-chained), session controls, impersonation logging |

## 2. Privilege Separation — the core invariant

```
 network ──► hpd (unpriv, heropanel) ──gRPC──► hp-broker (root) ──► OS
                    │                              │
             largest attack surface        tiny, audited, allowlisted
             NEVER runs as root            ONLY component that is root
```

### `hp-broker` design rules (non-negotiable)
1. **Tiny surface.** No HTTP, no DB, no business logic. Only a gRPC server on `/run/heropanel/broker.sock` (mode `0660`, `root:heropanel`).
2. **Capability allowlist.** The broker exposes a *fixed enum* of operations. There is **no** "run arbitrary command" capability. Adding a capability is a code change + review, not config.
3. **No shell, ever.** All execution uses `exec.Command(bin, args...)` with argument arrays — no `sh -c`, no string interpolation, no user input concatenated into a command line. Paths to binaries are absolute and pinned.
4. **Strict input validation** per capability: usernames match `^[a-z][a-z0-9_-]{0,31}$`, domains validated as FQDNs, paths canonicalized and confined to allowlisted roots (reject `..`, symlinks escaping the site root, absolute paths outside `/srv/heropanel`).
5. **Caller authentication.** The broker verifies the peer credentials of the Unix socket (`SO_PEERCRED`: uid/gid must be `heropanel`) **and** a rotating broker token from `secrets.env`. Double gate.
6. **Per-call audit.** Every invocation is written to `broker-audit.log` *before* execution (intent) and after (outcome), hash-chained, with actor correlation passed from `hpd`.
7. **Resource-bounded.** Broker runs under systemd hardening (see §8) with its own limits; a runaway capability can't exhaust the box.

### Representative broker capabilities (the *complete* set is enumerated in code)
```
SystemUser.Create / Delete / SetQuota / Lock
Service.Restart(name ∈ allowlist: openlitespeed, php-fpm@*, mariadb, postfix, dovecot, docker, nftables…)
WebServer.WriteVhost(siteID, renderedConfig)  # config templated by hpd, validated by broker, tested before reload
PhpFpm.WritePool / Reload
File.Op(scope=siteRoot, op∈{chown,chmod,mkdir,move,delete})   # confined to /srv/heropanel/sites/<id>
Firewall.ApplyRuleset(renderedNftables)       # validated, atomic swap w/ rollback timer
Cert.Issue / Install / Renew
Cron.Install(user, entry)  # to that user's crontab, not root's
Mail.ProvisionAccount / Dkim
Docker.Compose(projectDir, action)            # only when docker module enabled
Module.SystemctlStartStop(unit ∈ heropanel-mod@*)
Backup.SnapshotPath(scope)                     # read-only tar of allowlisted roots
```
Every capability: validates → (optionally) writes a rendered config → **tests config** (e.g. `litespeed -t`, `nft -c`) → applies → verifies → on failure, **auto-rolls back** and returns a typed error.

## 3. Website Isolation (the multi-tenant boundary)

Each site is a security domain. On creation the broker provisions:

| Isolation dimension | Mechanism |
|---------------------|-----------|
| Identity | Dedicated **Linux user + group** (`site<id>` / uid ≥ 20000), no login shell unless SSH/terminal explicitly enabled, home = site root |
| Filesystem | Site root `0750 siteuser:sitegroup`; **panel user is *not* in the site's group**; document root, tmp, sessions, logs all **per-site** (never `/tmp` shared — kills session-fixation & symlink races) |
| PHP | **Dedicated PHP-FPM pool** running as the site user (`user=site<id>`), `open_basedir` locked to the site root, `disable_functions` baseline (exec, system, passthru, proc_open…) tunable per plan |
| Process limits | systemd slice / cgroup v2 per site: `CPUQuota`, `MemoryMax`, `TasksMax` (pids), `IOReadBandwidthMax` — from `site_limits` |
| Disk | Filesystem project quotas (XFS prjquota / ext4 quota) per site user: disk + inode caps |
| Network | Optional egress firewall per site; mail/DB access via local sockets with per-user grants |
| Runtime (node/py/go) | Runs under the site user via a dedicated systemd unit in the site's slice; env + secrets scoped to that unit |
| Namespace hardening (optional, plan-gated) | `ProtectHome`, `PrivateTmp`, `NoNewPrivileges`, mount namespace confining the process tree to the site subtree |

**Invariant:** no code path lets Site A read or write Site B's files, DB, sessions, or secrets. The panel enforces this by *never* running site code itself and by scoping every broker file-op to a single validated site root.

## 4. Authentication

- **Password hashing:** Argon2id (tuned memory/time cost, per-hash salt), upgrade-on-login if params change. Never MD5/SHA-unsalted.
- **MFA:** TOTP (RFC 6238) + **WebAuthn/passkeys** (roaming + platform authenticators). Recovery codes (hashed, single-use). Admins can be *required* to have MFA.
- **Sessions:** server-side session records (`sessions` table) + `HttpOnly/Secure/SameSite=Strict` cookie; short-lived access JWT (5–15 min) with refresh rotation and reuse-detection (revoke family on replay).
- **Brute force:** per-account + per-IP throttling, exponential lockout, `login_attempts` logging, optional CAPTCHA after N failures, fail2ban jail on the panel's auth log.
- **API keys:** prefixed, hashed at rest, scoped, revocable, per-key rate limits, last-used tracking.

## 5. Authorization (RBAC)
- Permission-based (`site.create`, `dns.zone.edit`, …) grouped into roles, roles optionally **resource-scoped** (a reseller's role scoped to their tenant subtree; a client scoped to their own sites).
- Enforced in the **service layer** (not just handlers) so gRPC/CLI paths are covered too.
- **Deny by default.** Absent permission = 403. Scope mismatch = 404 (don't reveal existence).
- **Impersonation** (admin → user) requires a distinct permission, is time-boxed, banner-visible, and every action during impersonation is double-attributed in the audit log.

## 6. Secrets & Cryptography
- **Envelope encryption:** a master key (from `/etc/heropanel/secrets.env` `0600`, or OS keyring / age file) wraps rotating **data keys**; `*_enc` columns use **AES-256-GCM** with per-record nonces and AAD binding (row id + column) to prevent swap attacks.
- **Key rotation** re-wraps data keys without bulk re-encryption; rotation is audited.
- **TLS:** panel serves only TLS 1.2+ (prefer 1.3), modern cipher suites, HSTS. Panel cert auto-provisioned (Let's Encrypt) or self-signed on first boot with a clear rotation path.
- **Signing:** update artifacts and module packages are **cosign/minisign-signed**; the installer/updater verify signatures against a pinned public key before install (see [08](08-deployment-architecture.md)).
- **Internal transport:** gRPC over Unix sockets (no network exposure); future multi-node agents use **mTLS** with a panel-managed CA.

## 7. Application Firewall & Intrusion Defense (Security module)
- **Firewall abstraction** over nftables (primary) / ufw, CSF-compatible concepts; rules rendered by `hpd`, validated + applied atomically by the broker with a **rollback timer** (if the operator doesn't confirm connectivity, rules revert — prevents lock-out).
- **Fail2Ban** managed jails (SSH, panel auth, OLS, mail); **CrowdSec** optional bouncer.
- **ModSecurity + OWASP CRS** in front of hosted sites (OLS/Nginx), per-site toggle + paranoia level.
- **Malware/rootkit:** ClamAV + LMD (maldet), rkhunter, Lynis audits — scheduled + on-demand, results in `scan_runs`, hits go to **quarantine** (moved, permission-stripped, hash-recorded) not deleted.
- **File Integrity Monitoring:** baseline hashes of site + system-critical paths; drift raises `security:events`.
- **Automatic security updates:** unattended-upgrades / dnf-automatic managed + reported; panel self-updates via signed channel.
- **SSH hardening:** key-only auth toggle, port change, `AllowUsers`, disable root login, 2FA-for-SSH option — applied via broker with the same rollback safety.
- **Geo/IP allow-block lists** enforced at firewall + app edge.

## 8. Process & Host Hardening (systemd)
Every unit ships hardened. Example directives applied to `hpd`, modules (and a *stricter* subset to `hp-broker`, which needs some privileges but is otherwise locked down):
```
NoNewPrivileges=yes          ProtectSystem=strict         ProtectHome=yes
PrivateTmp=yes               PrivateDevices=yes           ProtectKernelTunables=yes
ProtectKernelModules=yes     ProtectControlGroups=yes     RestrictNamespaces=yes
RestrictSUIDSGID=yes         RestrictRealtime=yes         LockPersonality=yes
MemoryDenyWriteExecute=yes   SystemCallArchitectures=native
CapabilityBoundingSet=…      (broker: only the caps it truly needs, e.g. CAP_CHOWN, CAP_SETUID/SETGID, CAP_DAC_OVERRIDE — nothing more)
ReadWritePaths=…             (each unit gets an explicit, minimal RW path list)
```
Optional **AppArmor/SELinux** profiles per binary as a second layer. `hpd` gets `ProtectSystem=strict` with only `/var/lib/heropanel`, `/var/log/heropanel`, `/run/heropanel` writable.

## 9. Audit Logging (tamper-evident)
- Separate append-only `audit_log` (+ mirrored file) covering: auth events, every privileged/mutating API call, every broker capability invocation, impersonation, config changes, module lifecycle, security actions.
- **Hash chaining:** `row_hash = H(prev_hash || canonical(row))`; a periodic job can verify chain integrity and alert on breaks. Optional export to external SIEM / append-only remote sink.
- Records `who (user/apikey/system/broker) · what · target · outcome · ip · correlation_id · when`.

## 10. Input Handling & App Security Baseline
- All input validated at the edge (struct validation) *and* re-validated in services for non-HTTP callers.
- Path traversal, SSRF (webhook/URL fetchers use allowlists + no redirects to link-local/metadata IPs), template injection, and command injection are addressed structurally (no shell; parameterized SQL; no dynamic template eval).
- SQL strictly parameterized (sqlx named/positional args) — no string-built queries.
- Strict CSP (nonce-based), no inline eval; SPA served with subresource integrity where feasible.
- Uploads: type/size limits, stored outside web roots, never executed by the panel; optional AV scan on upload.

## 11. Compliance-friendly features
Session management UI, IP allow/block, geo-blocking, exportable audit trail, data-at-rest encryption, forced-MFA policies, password policies, and role separation give operators the controls needed for SOC2/ISO-style postures (the panel doesn't claim certification; it provides the primitives).

---
Next: [06 — Plugin Architecture](06-plugin-architecture.md)
