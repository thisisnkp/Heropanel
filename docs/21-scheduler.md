# 21 — Scheduler

The Scheduler runs commands on a calendar — the panel's cron. It is in-core, and
its jobs are **real systemd timers**, not crontab lines.

Back to [index](README.md).

## 1. Why systemd timers, not a crontab

systemd is the supervisor the panel already relies on for app runtimes and
slices, and a timer gives three things a crontab line cannot:

- **A real calendar.** `OnCalendar=` speaks "daily", "Mon *-*-* 09:00:00",
  "*-*-01 04:00:00" — richer than five fields, and validated by the thing that
  will execute it.
- **An overlap policy, for free.** Each job is a `.timer` triggering a
  `Type=oneshot` `.service`. A oneshot that is still running when its timer fires
  again is simply not started a second time — a slow job never stacks up on
  itself. That is the overlap policy, and it is systemd's own semantics rather
  than a lock file the panel would have to get right.
- **Catch-up after downtime.** `Persistent=true` runs a job that was missed while
  the host was off, once, on the next boot. crond silently skips it.

## 2. The safety story: never root

A scheduled job is an **arbitrary command** — that is its purpose — so the module
is built so that the command's reach is bounded before anything else is decided.

Every job is **site-scoped**. The generated service runs as the site's
unprivileged user, in the site's home, **inside the site's cgroup slice** (so the
site's CPU/memory/task limits bound it), with the same hardening an app unit
gets: `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, `ProtectHome`,
write access only to the site's own tree. There is no input on any API that
produces a root cron job.

The schedule string is matched against systemd's calendar charset before it goes
anywhere near a unit file — `daily; rm -rf /` is refused as a schedule, not
quoted into one — and the job id in every unit filename is a ULID, so a filename
is never attacker-shaped.

## 3. Logs without the journal

The unit's command is placed in a launcher script that appends the job's
stdout+stderr to `logs/cron-<uid>.log` inside the site's own logs directory. The
API reads that file back (`GET …/cron/{jid}/logs`, force-audited). Doing it in
the launcher rather than with `journalctl` means logs work identically on a host
with a journal, without one, and in the shim-based e2e harness.

## 4. Broker capabilities and permissions

Four capabilities, all policy-gated and audited: `cron.apply` (write both units,
`daemon-reload`, `enable --now` the timer), `cron.remove` (disable, delete,
reload — idempotent), `cron.run` (start the service now, so a job can be tested
without waiting), `cron.logs` (bounded tail of the captured output).

Jobs ride on the site permissions — `site.read` to list and read logs,
`site.write` to create, toggle, run and delete — because whoever may change the
site may schedule work inside it, and nobody else.

Disabling a job **removes its timer but keeps its definition**: the units are
deleted from disk (nothing left for systemd to fire) while the row stays, so
enabling later re-renders exactly what was configured.

## 5. Definition of done

Broker: `cronunit.go`, unit-tested with the fake runner/fs — the rendered service
carries `Type=oneshot`, the site user, the slice and the hardening set; the timer
carries the schedule, `Persistent=true` and its service binding; flag-shaped and
shell-shaped inputs are refused before any systemctl runs; remove is idempotent;
logs read the captured file and an unrun job reads as empty.

hpd: `internal/cron` (validation mirroring the broker's, site resolution, and an
ownership check so one site's UID can never act on another site's job),
`repository/cron_repo.go`, six routes under `/sites/{uid}/cron`, and a Cron tab
on the site page.

Live proof: **`deploy/docker/e2e/run-cron.sh`** (in CI). It schedules a job whose
command is `id -un` — output that *is* the identity it ran as — and asserts the
timer+service exist on disk with the site user and schedule, `cron.apply` lands
on the broker's audit chain, run-now executes and **the log reads back `hps1`,
not root**, a shell-shaped schedule is refused with 400, disable removes the
timer while the definition survives, and delete removes everything. The one thing
the harness cannot prove is the timer *firing on its own schedule* — that is
systemd itself, and the unit-rendering tests pin what systemd is given.

---
Back to [index](README.md).
