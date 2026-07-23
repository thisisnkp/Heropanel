package capabilities

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Scheduled jobs as real systemd timers.
//
// A cron job is a `.timer` that triggers a oneshot `.service`, not a crontab
// line. systemd is the scheduler the panel already relies on, and it gives three
// things a crontab does not: a **calendar** far richer than five fields, captured
// **logs**, and an **overlap policy** for free — a oneshot service that is still
// running when its timer fires again is not started a second time, so a slow job
// never stacks up on itself. `Persistent=true` also runs a missed job once after
// downtime, which crond cannot.
//
// Every job is **site-scoped**: it runs as the site's unprivileged user, in the
// site home, inside the site's cgroup slice, with the same hardening an app unit
// gets. This is the safety story — a scheduled command is arbitrary code, and it
// is bounded to exactly what the site user can already do, never root. The
// command's output is appended to a log file in the site's logs directory (by the
// launcher itself), so logs work without depending on the journal.

func cronBase(uid string) string        { return "heropanel-cron-" + uid }
func cronServiceName(uid string) string { return cronBase(uid) + ".service" }
func cronTimerName(uid string) string   { return cronBase(uid) + ".timer" }
func cronServicePath(uid string) string { return unitDir + "/" + cronServiceName(uid) }
func cronTimerPath(uid string) string   { return unitDir + "/" + cronTimerName(uid) }

var (
	// reCronUID is a ULID: Crockford base32, so it is always a safe filename.
	reCronUID = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	// reCalendar bounds an OnCalendar expression to the characters systemd
	// calendar syntax uses. systemd itself rejects a malformed-but-in-charset
	// value at enable time; this stops anything that is not a calendar at all.
	reCalendar = regexp.MustCompile(`^[A-Za-z0-9 :*/,.~-]{1,128}$`)
)

func validateCronUID(uid string) error {
	if !reCronUID.MatchString(uid) {
		return errx.Validation("invalid_cron_uid", "Invalid schedule id.")
	}
	return nil
}

// ── cron.apply ───────────────────────────────────────────────────────────────

// CronApply writes (or rewrites) a site's scheduled job and enables its timer.
type CronApply struct{}

func (CronApply) Name() string { return "cron.apply" }

type cronApplyInput struct {
	UID      string `json:"uid"`
	Vhost    string `json:"vhost"`
	Username string `json:"username"`
	Home     string `json:"home"`
	Command  string `json:"command"`
	Schedule string `json:"schedule"`
}

func (CronApply) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in cronApplyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for cron.apply.")
	}
	if err := validateCronUID(in.UID); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if in.Command == "" || len(in.Command) > 2000 || strings.ContainsAny(in.Command, "\x00\n\r") {
		return capability.Result{}, errx.Validation("invalid_command", "Invalid scheduled command.")
	}
	if !reCalendar.MatchString(in.Schedule) {
		return capability.Result{}, errx.Validation("invalid_schedule", "Invalid schedule expression.")
	}

	home := strings.TrimRight(in.Home, "/")
	logFile := home + "/logs/cron-" + in.UID + ".log"
	launcher := home + "/.hp-cron-" + in.UID
	// The launcher carries the command and redirects its output to the site's log
	// dir, so `cron.logs` can read it without the systemd journal — and the shim
	// harness, which is not systemd, captures output the same way.
	script := "#!/bin/sh\nexec " + in.Command + " >> " + logFile + " 2>&1\n"
	if err := c.FS.WriteFile(launcher, []byte(script), 0o755); err != nil {
		return capability.Result{}, errx.Upstream(err, "launcher_write_failed", "Could not write the cron launcher.")
	}
	if err := c.FS.WriteFile(cronServicePath(in.UID), []byte(renderCronService(in, home, launcher)), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "unit_write_failed", "Could not write the cron service unit.")
	}
	if err := c.FS.WriteFile(cronTimerPath(in.UID), []byte(renderCronTimer(in)), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "unit_write_failed", "Could not write the cron timer unit.")
	}

	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", "--now", cronTimerName(in.UID)},
	} {
		res, err := c.Runner.Run(c.Ctx, exec.Command{Path: systemctlPath, Args: args, Timeout: 30 * time.Second})
		if err != nil {
			return capability.Result{}, errx.Upstream(err, "systemctl_failed", "systemctl "+args[0]+" failed.")
		}
		if res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "systemctl_failed",
				"systemctl "+args[0]+" returned non-zero: "+string(res.Stderr))
		}
	}
	return capability.Result{Data: map[string]any{"uid": in.UID, "timer": cronTimerName(in.UID), "applied": true}}, nil
}

// renderCronService builds the hardened oneshot service. It is identical in
// posture to an app unit — same user, slice and confinement — because a scheduled
// command is arbitrary code and must be bounded to exactly the site user's reach.
func renderCronService(in cronApplyInput, home, launcher string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=HeroPanel cron " + in.UID + "\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	b.WriteString("User=" + in.Username + "\n")
	b.WriteString("Group=" + in.Username + "\n")
	b.WriteString("WorkingDirectory=" + home + "\n")
	b.WriteString("ExecStart=" + launcher + "\n")
	b.WriteString("Slice=" + SiteSliceName(in.Vhost) + "\n")
	b.WriteString("NoNewPrivileges=true\n")
	b.WriteString("PrivateTmp=true\n")
	b.WriteString("ProtectSystem=strict\n")
	b.WriteString("ProtectHome=true\n")
	b.WriteString("ReadWritePaths=" + home + "\n")
	b.WriteString("UMask=0027\n")
	return b.String()
}

// renderCronTimer builds the timer. Persistent=true runs a job missed during
// downtime once on boot, which crond cannot do.
func renderCronTimer(in cronApplyInput) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=HeroPanel cron timer " + in.UID + "\n\n")
	b.WriteString("[Timer]\n")
	b.WriteString("OnCalendar=" + in.Schedule + "\n")
	b.WriteString("Persistent=true\n")
	b.WriteString("Unit=" + cronServiceName(in.UID) + "\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")
	return b.String()
}

// ── cron.remove ──────────────────────────────────────────────────────────────

// CronRemove disables and deletes a scheduled job (both units). Idempotent.
type CronRemove struct{}

func (CronRemove) Name() string { return "cron.remove" }

func (CronRemove) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for cron.remove.")
	}
	if err := validateCronUID(in.UID); err != nil {
		return capability.Result{}, err
	}
	// Best-effort disable; a job that is already gone is not an error.
	_, _ = c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"disable", "--now", cronTimerName(in.UID)}, Timeout: 30 * time.Second})
	_ = c.FS.Remove(cronTimerPath(in.UID))
	_ = c.FS.Remove(cronServicePath(in.UID))
	_, _ = c.Runner.Run(c.Ctx, exec.Command{Path: systemctlPath, Args: []string{"daemon-reload"}, Timeout: 30 * time.Second})
	return capability.Result{Data: map[string]any{"uid": in.UID, "removed": true}}, nil
}

// ── cron.run ─────────────────────────────────────────────────────────────────

// CronRun triggers a scheduled job immediately (systemctl start on the service),
// so an operator can test it without waiting for the timer.
type CronRun struct{}

func (CronRun) Name() string { return "cron.run" }

func (CronRun) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for cron.run.")
	}
	if err := validateCronUID(in.UID); err != nil {
		return capability.Result{}, err
	}
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"start", cronServiceName(in.UID)}, Timeout: 5 * time.Minute})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "cron_run_failed", "Could not run the scheduled job.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "cron_run_failed",
			"The scheduled job returned non-zero: "+string(res.Stderr))
	}
	return capability.Result{Data: map[string]any{"uid": in.UID, "ran": true}}, nil
}

// ── cron.logs ────────────────────────────────────────────────────────────────

// CronLogs returns a bounded tail of a job's captured output.
type CronLogs struct{}

func (CronLogs) Name() string { return "cron.logs" }

// maxCronLog bounds a single log read.
const maxCronLog = 64 * 1024

func (CronLogs) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		UID  string `json:"uid"`
		Home string `json:"home"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for cron.logs.")
	}
	if err := validateCronUID(in.UID); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	logFile := strings.TrimRight(in.Home, "/") + "/logs/cron-" + in.UID + ".log"
	data, err := c.FS.ReadFile(logFile)
	if err != nil {
		// No log yet (never run) is not an error — it is empty output.
		return capability.Result{Data: map[string]any{"log": ""}}, nil
	}
	if len(data) > maxCronLog {
		data = data[len(data)-maxCronLog:]
	}
	return capability.Result{Data: map[string]any{"log": string(data)}}, nil
}
