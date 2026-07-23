package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
)

const dkimPEM = "-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----\n"

func dkimInput(extra map[string]any) map[string]any {
	in := map[string]any{
		"keys": []map[string]any{
			{"domain": "example.com", "selector": "hp1", "private_pem": dkimPEM},
		},
		"keytable":     "hp1._domainkey.example.com example.com:hp1:/etc/opendkim/heropanel/keys/example.com/hp1.private\n",
		"signingtable": "*@example.com hp1._domainkey.example.com\n",
	}
	for k, v := range extra {
		in[k] = v
	}
	return in
}

// The signer state lands on pinned paths: the key private to opendkim, the
// tables + constant conf, the milter wired with fixed settings, both daemons
// prodded.
func TestMailDKIMApplyWritesSignerState(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	res, err := (capabilities.MailDKIMApply{}).Execute(appCtx(fr, fs), raw(t, dkimInput(nil)))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Data["keys_applied"] != 1 {
		t.Errorf("keys_applied = %v", res.Data["keys_applied"])
	}

	key, ok := fs.Written("/etc/opendkim/heropanel/keys/example.com/hp1.private")
	if !ok || key != dkimPEM {
		t.Error("the private key was not written to its derived path")
	}
	conf, _ := fs.Written("/etc/opendkim.conf")
	for _, want := range []string{"Socket                  inet:8891@localhost", "KeyTable", "SigningTable            refile:"} {
		if !strings.Contains(conf, want) {
			t.Errorf("opendkim.conf missing %q", want)
		}
	}

	var sawChown, sawMilter, sawRestart bool
	for _, call := range fr.Calls {
		argv := strings.Join(call.Args, " ")
		switch call.Path {
		case "/bin/chown":
			if strings.Contains(argv, "opendkim:opendkim") && strings.Contains(argv, "hp1.private") {
				sawChown = true
			}
		case "/usr/sbin/postconf":
			if strings.Contains(argv, "smtpd_milters=inet:localhost:8891") &&
				strings.Contains(argv, "milter_default_action=accept") {
				sawMilter = true
			}
		case "/usr/bin/systemctl":
			if argv == "restart opendkim" {
				sawRestart = true
			}
		}
	}
	if !sawChown {
		t.Error("the key was not handed to opendkim")
	}
	if !sawMilter {
		t.Error("postfix's milter settings were not applied")
	}
	if !sawRestart {
		t.Error("opendkim was not restarted")
	}
}

// Bad domains, selectors and keys are refused before anything is written.
func TestMailDKIMApplyRefusesBadInput(t *testing.T) {
	for _, keys := range [][]map[string]any{
		{{"domain": "not_a_domain", "selector": "hp1", "private_pem": dkimPEM}},
		{{"domain": "example.com", "selector": "../hp1", "private_pem": dkimPEM}},
		{{"domain": "example.com", "selector": "hp1", "private_pem": "not a pem"}},
	} {
		fr := &exec.FakeRunner{}
		fs := fsys.NewFake()
		if _, err := (capabilities.MailDKIMApply{}).Execute(appCtx(fr, fs),
			raw(t, dkimInput(map[string]any{"keys": keys}))); err == nil {
			t.Errorf("bad keys %v were accepted", keys)
		}
		if len(fr.Calls) != 0 {
			t.Error("commands ran for refused input")
		}
	}
}
