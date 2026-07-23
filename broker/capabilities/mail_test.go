package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
)

// Provision prepares a host idempotently: vmail user (exit 9 = exists is
// fine), directories, empty maps postmapped, fixed postconf settings, and the
// dovecot drop-in. Nothing in any argv comes from input — there is no input.
func TestMailProvisionSeedsAHostIdempotently(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/sbin/useradd" {
			return exec.Result{ExitCode: 9}, nil // already exists — the normal re-run case
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	fs := fsys.NewFake()

	res, err := (capabilities.MailProvision{}).Execute(appCtx(fr, fs), raw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.Data["provisioned"] != true {
		t.Error("provision did not report success")
	}

	var sawUseradd, sawVmailDir, sawPostconf bool
	postmapped := map[string]bool{}
	for _, call := range fr.Calls {
		argv := strings.Join(call.Args, " ")
		switch call.Path {
		case "/usr/sbin/useradd":
			sawUseradd = strings.Contains(argv, "--system") && strings.Contains(argv, "vmail") &&
				strings.Contains(argv, "/usr/sbin/nologin")
		case "/usr/bin/install":
			if strings.Contains(argv, "-o vmail") && strings.Contains(argv, "/var/lib/heropanel/mail") {
				sawVmailDir = strings.Contains(argv, "0770")
			}
		case "/usr/sbin/postconf":
			sawPostconf = strings.Contains(argv, "virtual_transport=lmtp:unix:private/dovecot-lmtp") &&
				strings.Contains(argv, "virtual_mailbox_domains=hash:/etc/postfix/heropanel/domains")
		case "/usr/sbin/postmap":
			postmapped[call.Args[len(call.Args)-1]] = true
		}
	}
	if !sawUseradd {
		t.Error("vmail user was not created as a system nologin user")
	}
	if !sawVmailDir {
		t.Error("the Maildir root was not created vmail-owned 0770")
	}
	if !sawPostconf {
		t.Error("the fixed postfix virtual settings were not applied")
	}
	for _, m := range []string{"/etc/postfix/heropanel/domains", "/etc/postfix/heropanel/mailboxes", "/etc/postfix/heropanel/aliases"} {
		if !postmapped[m] {
			t.Errorf("%s was not postmapped", m)
		}
	}

	conf, ok := fs.Written("/etc/dovecot/conf.d/95-heropanel.conf")
	if !ok {
		t.Fatal("the dovecot drop-in was not written")
	}
	for _, want := range []string{
		"passwd-file", "/etc/dovecot/heropanel-users",
		"unix_listener /var/spool/postfix/private/dovecot-lmtp",
		"maildir:/var/lib/heropanel/mail/%d/%n/Maildir",
		"quota = maildir",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("dovecot drop-in missing %q", want)
		}
	}
	if _, ok := fs.Written("/etc/dovecot/heropanel-users"); !ok {
		t.Error("the empty users file was not seeded")
	}
}

// Apply writes the four rendered files (users file private), postmaps every
// map, and reloads both daemons.
func TestMailApplyWritesMapsAndReloads(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	res, err := (capabilities.MailApply{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"domains":   "example.com OK\n",
		"mailboxes": "info@example.com example.com/info/Maildir/\n",
		"aliases":   "sales@example.com info@example.com\n",
		"users":     "info@example.com:{BLF-CRYPT}$2a$hash:::::: userdb_quota_rule=*:storage=1024M\n",
	}))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Data["applied"] != true {
		t.Error("apply did not report success")
	}
	if got, _ := fs.Written("/etc/postfix/heropanel/domains"); !strings.Contains(got, "example.com OK") {
		t.Errorf("domains map = %q", got)
	}
	if got, _ := fs.Written("/etc/dovecot/heropanel-users"); !strings.Contains(got, "{BLF-CRYPT}") {
		t.Errorf("users file = %q", got)
	}

	var postmaps, reloads []string
	var ownedUsers bool
	for _, call := range fr.Calls {
		switch call.Path {
		case "/usr/sbin/postmap":
			postmaps = append(postmaps, call.Args[len(call.Args)-1])
		case "/usr/sbin/postfix", "/usr/bin/doveadm":
			reloads = append(reloads, call.Path+" "+strings.Join(call.Args, " "))
		case "/bin/chown":
			if strings.Join(call.Args, " ") == "dovecot:dovecot /etc/dovecot/heropanel-users" {
				ownedUsers = true
			}
		}
	}
	if len(postmaps) != 3 {
		t.Errorf("postmap ran %d times, want 3: %v", len(postmaps), postmaps)
	}
	// The auth process runs as dovecot; a root-owned 0600 passwd-file would
	// break every IMAP login while looking perfectly configured.
	if !ownedUsers {
		t.Error("the users file was not handed to dovecot")
	}
	joined := strings.Join(reloads, ";")
	if !strings.Contains(joined, "/usr/sbin/postfix reload") || !strings.Contains(joined, "/usr/bin/doveadm reload") {
		t.Errorf("daemons were not reloaded: %v", reloads)
	}
}

// A failed postmap rolls every file back to its prior content — postfix keeps
// serving the last good maps.
func TestMailApplyRollsBackOnPostmapFailure(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if cmd.Path == "/usr/sbin/postmap" {
			return exec.Result{ExitCode: 1}, nil
		}
		return exec.Result{ExitCode: 0}, nil
	}}
	fs := fsys.NewFake()
	_ = fs.WriteFile("/etc/postfix/heropanel/domains", []byte("old.example OK\n"), 0o644)
	_ = fs.WriteFile("/etc/dovecot/heropanel-users", []byte("old@old.example:{BLF-CRYPT}x\n"), 0o600)

	_, err := (capabilities.MailApply{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"domains": "new.example OK\n", "mailboxes": "", "aliases": "", "users": "",
	}))
	if err == nil {
		t.Fatal("a failed postmap reported success")
	}
	if got, _ := fs.Written("/etc/postfix/heropanel/domains"); got != "old.example OK\n" {
		t.Errorf("domains map was not rolled back: %q", got)
	}
	if got, _ := fs.Written("/etc/dovecot/heropanel-users"); got != "old@old.example:{BLF-CRYPT}x\n" {
		t.Errorf("users file was not rolled back: %q", got)
	}
}

// mail.purge derives the path from validated parts; bad input never reaches rm.
func TestMailPurgeDerivesThePathAndRefusesBadInput(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	if _, err := (capabilities.MailPurge{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"domain": "example.com", "local_part": "info",
	})); err != nil {
		t.Fatalf("purge: %v", err)
	}
	last, _ := fr.Last()
	if last.Path != "/bin/rm" || strings.Join(last.Args, " ") != "-rf -- /var/lib/heropanel/mail/example.com/info" {
		t.Errorf("rm argv = %s %v", last.Path, last.Args)
	}

	before := len(fr.Calls)
	for _, bad := range []map[string]any{
		{"domain": "example.com", "local_part": "../../etc"},
		{"domain": "not_a_domain", "local_part": "info"},
		{"domain": "example.com/..", "local_part": "info"},
	} {
		if _, err := (capabilities.MailPurge{}).Execute(appCtx(fr, fs), raw(t, bad)); err == nil {
			t.Errorf("bad input %v was accepted", bad)
		}
	}
	if len(fr.Calls) != before {
		t.Error("rm ran for refused input")
	}
}
