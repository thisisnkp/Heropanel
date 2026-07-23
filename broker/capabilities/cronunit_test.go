package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

const cronUID = "01HXXXXXXXXXXXXXXXXXXXXXXX"

func cronOK(extra map[string]any) map[string]any {
	in := map[string]any{
		"uid": cronUID, "vhost": "hps1", "username": "hps1",
		"home": "/srv/heropanel/sites/1", "command": "php artisan schedule:run",
		"schedule": "*-*-* 02:00:00",
	}
	for k, v := range extra {
		in[k] = v
	}
	return in
}

// The scheduled command runs as the site user, in the site slice, hardened like
// an app unit — never root. This is the module's whole safety story.
func TestCronApplyWritesHardenedTimerAndService(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	if _, err := (capabilities.CronApply{}).Execute(appCtx(fr, fs), raw(t, cronOK(nil))); err != nil {
		t.Fatalf("apply: %v", err)
	}

	svc, _ := fs.Written("/etc/systemd/system/heropanel-cron-" + cronUID + ".service")
	for _, want := range []string{
		"Type=oneshot",
		"User=hps1",
		"WorkingDirectory=/srv/heropanel/sites/1",
		"Slice=heropanel-site-hps1.slice",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=/srv/heropanel/sites/1",
	} {
		if !strings.Contains(svc, want) {
			t.Errorf("service unit missing %q:\n%s", want, svc)
		}
	}

	timer, _ := fs.Written("/etc/systemd/system/heropanel-cron-" + cronUID + ".timer")
	for _, want := range []string{
		"OnCalendar=*-*-* 02:00:00",
		// Persistent runs a job missed during downtime once — crond cannot.
		"Persistent=true",
		"Unit=heropanel-cron-" + cronUID + ".service",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer unit missing %q:\n%s", want, timer)
		}
	}

	// The launcher carries the command and captures output to the site's log dir,
	// so logs work without the journal.
	launcher, _ := fs.Written("/srv/heropanel/sites/1/.hp-cron-" + cronUID)
	if !strings.Contains(launcher, "exec php artisan schedule:run >> /srv/heropanel/sites/1/logs/cron-"+cronUID+".log 2>&1") {
		t.Errorf("launcher wrong:\n%s", launcher)
	}

	// daemon-reload then enable --now the timer.
	if len(fr.Calls) != 2 {
		t.Fatalf("made %d systemctl calls, want 2", len(fr.Calls))
	}
	if got := strings.Join(fr.Calls[1].Args, " "); got != "enable --now heropanel-cron-"+cronUID+".timer" {
		t.Errorf("second call = %q, want enable --now of the timer", got)
	}
}

func TestCronApplyValidation(t *testing.T) {
	bad := []map[string]any{
		{"uid": "not-a-ulid"},
		{"uid": "../../../etc/passwd"},
		{"command": ""},
		{"command": "evil\nsecond line"},
		{"schedule": ""},
		{"schedule": "daily; rm -rf /"},        // ';' is not calendar syntax
		{"home": "/etc"},                       // outside the sites root
		{"username": "root; touch /tmp/pwned"}, // not a username
	}
	for _, spoil := range bad {
		fr := &exec.FakeRunner{}
		fs := fsys.NewFake()
		if _, err := (capabilities.CronApply{}).Execute(appCtx(fr, fs), raw(t, cronOK(spoil))); err == nil {
			t.Errorf("accepted bad input %v", spoil)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("ran systemctl despite bad input %v", spoil)
		}
	}
}

func TestCronRunAndRemove(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.CronRun{}).Execute(appCtx(fr, fsys.NewFake()),
		raw(t, map[string]any{"uid": cronUID})); err != nil {
		t.Fatalf("run: %v", err)
	}
	last, _ := fr.Last()
	if got := strings.Join(last.Args, " "); got != "start heropanel-cron-"+cronUID+".service" {
		t.Errorf("run = %q, want a start of the service", got)
	}

	// Remove is idempotent: a job already gone is not an error.
	fr2 := &exec.FakeRunner{Result: exec.Result{ExitCode: 1}}
	if _, err := (capabilities.CronRemove{}).Execute(appCtx(fr2, fsys.NewFake()),
		raw(t, map[string]any{"uid": cronUID})); err != nil {
		t.Errorf("remove of a missing job errored: %v", err)
	}

	// A bad uid never reaches systemctl in either.
	for _, cap := range []interface {
		Name() string
	}{capabilities.CronRun{}, capabilities.CronRemove{}} {
		_ = cap
	}
	fr3 := &exec.FakeRunner{}
	if _, err := (capabilities.CronRun{}).Execute(appCtx(fr3, fsys.NewFake()),
		raw(t, map[string]any{"uid": "-rf"})); errx.KindOf(err) != errx.KindValidation {
		t.Errorf("run accepted a bad uid")
	}
	if len(fr3.Calls) != 0 {
		t.Errorf("ran systemctl with a bad uid")
	}
}

func TestCronLogsReadsTheCapturedFile(t *testing.T) {
	fs := fsys.NewFake()
	_ = fs.WriteFile("/srv/heropanel/sites/1/logs/cron-"+cronUID+".log", []byte("job output\n"), 0o644)
	res, err := (capabilities.CronLogs{}).Execute(appCtx(&exec.FakeRunner{}, fs),
		raw(t, map[string]any{"uid": cronUID, "home": "/srv/heropanel/sites/1"}))
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if res.Data["log"] != "job output\n" {
		t.Errorf("log = %q", res.Data["log"])
	}

	// Never run → empty log, not an error.
	res2, err := (capabilities.CronLogs{}).Execute(appCtx(&exec.FakeRunner{}, fsys.NewFake()),
		raw(t, map[string]any{"uid": cronUID, "home": "/srv/heropanel/sites/1"}))
	if err != nil || res2.Data["log"] != "" {
		t.Errorf("unrun job should read as empty, got %v err %v", res2.Data["log"], err)
	}
}
