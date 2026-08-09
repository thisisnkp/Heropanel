package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/broker/capabilities"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/fsys"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// mail.tls wires the mail host's certificate into Postfix (main.cf TLS + the
// submission/smtps services and their overrides) and Dovecot (the SSL drop-in +
// the postfix-private SASL socket). The hostname is validated; the cert path is
// derived from it, so nothing outside sslRoot can be named.
func TestMailTLSWiresPostfixAndDovecot(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()
	// The cert must already be installed for the hostname.
	_ = fs.WriteFile("/etc/nexpanel/ssl/mail.example.com/fullchain.pem", []byte("CERT"), 0o644)
	_ = fs.WriteFile("/etc/nexpanel/ssl/mail.example.com/privkey.pem", []byte("KEY"), 0o600)

	res, err := (capabilities.MailTLS{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"hostname": "mail.example.com",
	}))
	if err != nil {
		t.Fatalf("mail.tls: %v", err)
	}
	if res.Data["tls"] != true {
		t.Error("mail.tls did not report success")
	}

	var mainCF, master, overrides bool
	for _, call := range fr.Calls {
		if call.Path != "/usr/sbin/postconf" {
			continue
		}
		argv := strings.Join(call.Args, " ")
		switch {
		case strings.HasPrefix(argv, "-e "):
			mainCF = strings.Contains(argv, "smtpd_tls_cert_file=/etc/nexpanel/ssl/mail.example.com/fullchain.pem") &&
				strings.Contains(argv, "smtpd_sasl_path=private/auth")
		case strings.HasPrefix(argv, "-M "):
			if strings.Contains(argv, "submission inet") || strings.Contains(argv, "smtps inet") {
				master = true
			}
		case strings.HasPrefix(argv, "-P "):
			overrides = strings.Contains(argv, "submission/inet/smtpd_tls_security_level=encrypt") &&
				strings.Contains(argv, "smtps/inet/smtpd_tls_wrappermode=yes") &&
				strings.Contains(argv, "permit_sasl_authenticated,reject")
		}
	}
	if !mainCF {
		t.Error("main.cf TLS/SASL settings were not applied")
	}
	if !master {
		t.Error("the submission/smtps services were not defined in master.cf")
	}
	if !overrides {
		t.Error("the submission/smtps per-service overrides were not applied")
	}

	conf, ok := fs.Written("/etc/dovecot/conf.d/96-nexpanel-ssl.conf")
	if !ok {
		t.Fatal("the dovecot TLS drop-in was not written")
	}
	for _, want := range []string{
		"ssl = required",
		"ssl_cert = </etc/nexpanel/ssl/mail.example.com/fullchain.pem",
		"ssl_key = </etc/nexpanel/ssl/mail.example.com/privkey.pem",
		"unix_listener /var/spool/postfix/private/auth",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("dovecot TLS drop-in missing %q", want)
		}
	}
}

// Without a certificate on disk, mail.tls refuses rather than pointing the MTAs
// at files that do not exist (which would break every TLS handshake silently).
func TestMailTLSRefusesWhenCertMissing(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	_, err := (capabilities.MailTLS{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
		"hostname": "mail.example.com",
	}))
	if err == nil {
		t.Fatal("mail.tls succeeded with no certificate installed")
	}
	if !errx.IsKind(err, errx.KindConflict) {
		t.Errorf("want conflict, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Error("postconf ran despite the missing certificate")
	}
}

// A hostname that is not a valid FQDN never reaches the filesystem or postconf.
func TestMailTLSRejectsBadHostname(t *testing.T) {
	fr := &exec.FakeRunner{}
	fs := fsys.NewFake()

	for _, bad := range []string{"", "../etc", "mail example.com", "mail/../../etc"} {
		if _, err := (capabilities.MailTLS{}).Execute(appCtx(fr, fs), raw(t, map[string]any{
			"hostname": bad,
		})); err == nil {
			t.Errorf("bad hostname %q was accepted", bad)
		}
	}
	if len(fr.Calls) != 0 {
		t.Error("postconf ran for a rejected hostname")
	}
}
