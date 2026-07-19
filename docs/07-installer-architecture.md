# 07 — Installer Architecture

Goal: a **single command** that turns a fresh Linux box into a running HeroPanel install, safely, on any supported arch/OS, with detection, backups, and automatic rollback on failure.

> **Implementation status.** The **execute path is implemented and verified**
> ([`internal/installer/execute.go`](../internal/installer/execute.go),
> [`cmd/hp-installer`](../cmd/hp-installer)). `hp-installer --detect` / `--plan`
> report the host profile, compatibility verdict, and ordered action list;
> `--execute` runs the install, journaling each step to
> `/var/lib/heropanel/install-journal.json` so `--resume` continues an
> interrupted run and `--rollback` walks the completed steps in reverse and runs
> each one's inverse. The package-manager split (apt/dnf) is the only
> distro-specific code ([`pkgmgr.go`](../internal/installer/pkgmgr.go)); every
> other step is identical across distributions. **Verified live** (`run-installer.sh`,
> in CI) on a **fresh `ubuntu:24.04` (apt)** *and* **`rockylinux:9` (dnf)** image:
> a real execute installs packages, creates the `heropanel` user, renders the
> config + hardened systemd units, migrates a SQLite datastore as the service
> user, starts the broker + daemon, and the installer's own verify step confirms
> `/healthz` answers — with the broker socket group-owned by the panel group so
> the unprivileged daemon can reach it (the privilege-separation contract). A
> re-run resumes as a no-op; `--rollback` removes the user, files, and units and
> the panel stops answering.
>
> **Integrity:** the binaries step verifies every artifact against a `SHA256SUMS`
> manifest before anything lands in place — a mismatch fails the step and rolls
> back (`run-installer.sh` asserts "binaries verified against SHA256SUMS"; the
> tamper-rejection path is unit-tested). **arm64** is a first-class target:
> `run-arch-smoke.sh` runs the cross-compiled binaries under qemu on aarch64
> (both distro families) — detection, the SQLite driver + every migration, and
> the broker self-check.
>
> *Deferred:* backup/restore of pre-existing web/db/firewall configs before
> touching them; a **cryptographic signature** over the manifest (checksum
> verification is done; artifact *signing* is not, §6); `hpctl`; the uninstall
> subcommand; coexistence handling for a pre-existing Apache/Nginx/MySQL/Docker;
> the OLS panel reverse-proxy vhost (the panel is served by `hpd` directly for
> now, and the Docker verification runs `--no-webserver`); and package-level
> rollback (OS packages are intentionally **not** removed on rollback — other
> software may now depend on them, so that step records itself as not-reversible).

```
curl -fsSL https://get.heropanel.io/install.sh | bash
# or, pinned + verified:
curl -fsSL https://get.heropanel.io/install.sh -o install.sh \
  && sha256sum -c install.sh.sha256 && bash install.sh --channel stable
```

## 1. Two-stage design

The public `install.sh` is a **thin, auditable bootstrap** (POSIX sh, no bashisms beyond what's guarded). Its only jobs:
1. Refuse to run in unsafe conditions (non-root when root needed, unsupported OS/arch, missing curl/tar).
2. Detect **arch + OS + libc** just enough to fetch the correct **`hp-installer`** Go binary.
3. **Verify signature** of `hp-installer` against a pinned public key.
4. Hand off: `exec hp-installer install --channel <c> [flags]`.

Everything intelligent lives in the **`hp-installer` Go binary** (same codebase, testable, no fragile 2000-line shell script). Rationale: shell is fine for bootstrap, terrible for the complex, stateful, rollback-capable logic that follows.

```
install.sh (shell, ~150 lines)          hp-installer (Go, tested, stateful)
──────────────────────────────          ─────────────────────────────────────
detect arch/os/libc  ──────────────►    Preflight → Plan → Backup → Execute →
fetch + verify installer binary         Verify → Finalize   (Rollback on any failure)
exec hp-installer
```

## 2. Detection phase (Preflight)

`hp-installer preflight` gathers a full **system profile** (also exposed later at `/api/v1/system/info`):

| Category | Detected | How |
|----------|----------|-----|
| CPU arch | amd64 / arm64 / x86(386) | `uname -m` + Go runtime; drives every binary download |
| CPU | cores, model, flags (AES-NI, etc.) | `/proc/cpuinfo` |
| RAM / swap | total/available | `/proc/meminfo` → gates module recommendations & FPM sizing |
| OS/distro | Ubuntu/Debian/Rocky/Alma/Oracle + version | `/etc/os-release` |
| Package mgr | apt / dnf / yum | derived from distro |
| libc | glibc / musl | `ldd --version` |
| Virtualization | kvm/vmware/xen/lxc/openvz/bare-metal | `systemd-detect-virt` |
| Init | systemd present & version | required; hard fail if absent |
| Firewall | ufw / firewalld / nftables / iptables / csf | binary + service probe |
| Existing web servers | Apache, Nginx, **OpenLiteSpeed**, LiteSpeed | ports + binaries + services |
| Existing DB | MySQL, **MariaDB**, PostgreSQL | ports 3306/5432 + services |
| Existing runtimes | PHP, Python, Node, Go | `which` + versions |
| Docker | engine + compose | `docker version` |
| Ports | 80/443/8443/3306/6379/25/587/993/53… | `ss -tlnp` |
| Disk | free space, filesystem (XFS/ext4 → quota strategy) | `statfs`, `/proc/mounts` |
| SELinux/AppArmor | enforcing? | `getenforce` / `aa-status` |

Output: a machine profile + a human summary + a **compatibility verdict** (proceed / warn / block) with specific remediation.

## 3. Compatibility & conflict resolution

| Situation | Behavior |
|-----------|----------|
| Unsupported OS/arch, no systemd | **Block** with a clear reason |
| RAM below floor (e.g. < 1 GB) | **Warn**, offer SQLite-mode + minimal module set |
| Port 8443 (panel) in use | Offer alternate port via `--port` |
| Apache/Nginx already on 80/443 | Detect, ask: coexist (panel on 8443 only), or let HeroPanel manage the web server, or abort. Never silently kill someone's stack |
| Existing MySQL/MariaDB | Reuse it (prompt for/create a scoped panel DB user) instead of installing a second engine |
| Existing Docker | Reuse; don't reinstall |
| CSF/firewalld active | Integrate (add panel rules) rather than replace; warn before changes |

All conflicts are surfaced with a chosen-default and can be pre-answered via flags/`--config answers.yaml` for **unattended installs**.

## 4. Execution phases (atomic, journaled)

`hp-installer` maintains an on-disk **install journal** (`/var/lib/heropanel/install/journal.json`) so every step is resumable and reversible.

```
1. PLAN        Resolve versions per arch/OS; compute action list; show/confirm (unless --yes)
2. BACKUP      Snapshot anything it may modify: existing web/db configs, firewall rules,
               /etc/hosts, PHP configs → /var/lib/heropanel/install/backup/<ts>/
3. DEPS        Install base deps via native pkg mgr (idempotent): ca-certs, tar, zstd,
               and — per chosen options — OLS, PHP, MariaDB, Redis. Arch-correct repos.
4. USERS       Create heropanel system user/group; create /opt,/etc,/var,/run dirs with modes
5. BINARIES    Place hpd, hp-broker, hpctl (arch-correct, verified) into /opt/heropanel/bin
6. CONFIG      Generate config.yaml + secrets.env (random DB pw, JWT key, broker token,
               master enc key); set 0600/0640 ownership
7. DATABASE    Create panel DB + user; run golang-migrate migrations
8. SERVICES    Install systemd units (hardened); enable hpd + hp-broker; start
9. WEBSERVER   Configure OLS to serve the panel on :8443 (self-signed cert now; LE later);
               optional: register HeroPanel as the site web server
10. FIREWALL   Add rules for 8443 (and 80/443 if managing sites) with rollback timer
11. VERIFY     Health probe hpd /readyz; broker socket handshake; DB reachable; port open
12. FINALIZE   Print URL + one-time admin bootstrap token; write journal 'success'
```

Each step is **idempotent** (safe re-run) and records its inverse in the journal.

## 5. Rollback

If any step fails (or `--rollback` is invoked):
- Walk the journal **in reverse**, executing recorded inverses: stop/remove services, remove created users/dirs/binaries, **restore backed-up configs** (web/db/firewall) verbatim, drop the panel DB if we created it.
- Firewall changes have a **dead-man timer**: if verification doesn't confirm within N seconds, rules auto-revert (prevents SSH lock-out).
- End state after rollback ≈ pre-install state; the journal + logs are preserved under `/var/lib/heropanel/install/` for diagnosis.

## 6. Multi-arch / multi-OS binary resolution
- Never hardcode URLs/versions. A signed **release manifest** (`releases/<channel>/manifest.json`) maps `{component, version} → {os, arch, libc} → {url, sha256, sig}`.
- The installer picks the row matching the detected profile; downloads; verifies checksum **and** signature; only then installs.
- Third-party deps (OLS, PHP, MariaDB, Redis) are installed via the **distro's native repositories** (apt/dnf) using the distro's own arch handling — we don't ship those binaries, we orchestrate their packages. Where a vendor repo is needed (e.g. OLS, ondemand PHP), the correct repo for the arch/distro is added.

## 7. Modes & flags
```
--channel stable|beta|nightly     --yes (non-interactive)   --port 8443
--db mariadb|sqlite               --reuse-mariadb           --no-webserver
--modules docker,monitor,mail     --minimal (low-RAM preset)
--config answers.yaml             --dry-run (plan only)     --rollback
--offline /path/to/bundle.tar     (air-gapped install from a prefetched bundle)
```

## 8. Post-install
- Prints the panel URL (`https://<host>:8443`) and a **one-time bootstrap token**; first browser visit creates the admin account (with forced MFA setup optional).
- `hpctl doctor` runs the same preflight anytime for health/repair.
- Uninstaller (`hp-installer uninstall`) reuses the journal + backups to cleanly remove and restore.

## 9. Testing the installer
- Matrix CI across {Ubuntu LTS, Debian, Rocky, Alma, Oracle} × {amd64, arm64} in containers/VMs.
- Idempotency test (run twice), rollback test (inject failure at each phase), coexistence tests (pre-existing Apache/Nginx/MySQL/Docker), offline-bundle test.

---
Next: [08 — Deployment Architecture](08-deployment-architecture.md)
