package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Mail TLS: the MTAs present ONE server certificate for the mail host's own
// FQDN (mail.example.com), regardless of how many virtual mail domains they
// carry — a client connects to the mail host, not to each hosted domain. This
// capability wires that certificate (issued through the SSL module and written
// to sslRoot/<hostname> like every other panel cert) into Postfix and Dovecot,
// and opens the two ports a real mail client needs: submission/587 (STARTTLS,
// authenticated) and imaps/993 (implicit TLS). smtps/465 is opened too, since
// Apple Mail and others still prefer it.
//
// The panel's database is never involved: this is fixed configuration keyed by
// a validated hostname, applied idempotently, so a re-run is a no-op.

const (
	// dovecotSSLDropin is NexPanel's dovecot TLS drop-in. 96- sorts after the
	// 95- base drop-in, so its ssl_* settings win under dovecot's last-wins.
	dovecotSSLDropin = "/etc/dovecot/conf.d/96-nexpanel-ssl.conf"
)

// MailTLS wires a mail-host certificate into Postfix + Dovecot and opens the
// submission/imaps/smtps ports.
type MailTLS struct{}

type mailTLSInput struct {
	Hostname string `json:"hostname"`
}

// Name implements capability.Capability.
func (MailTLS) Name() string { return "mail.tls" }

// Execute implements capability.Capability.
func (MailTLS) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailTLSInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.tls.")
	}
	if err := capability.ValidateFQDN(in.Hostname); err != nil {
		return capability.Result{}, err
	}

	// The certificate must already be installed for this hostname — the SSL
	// module (self-signed, upload, or Let's Encrypt) writes it to this pinned
	// path. Deriving the path from a validated FQDN keeps it inside sslRoot.
	certPath := sslRoot + "/" + in.Hostname + "/fullchain.pem"
	keyPath := sslRoot + "/" + in.Hostname + "/privkey.pem"
	if ok, _ := c.FS.Exists(certPath); !ok {
		return capability.Result{}, errx.New(errx.KindConflict, "mail_cert_missing",
			"No certificate is installed for the mail hostname; issue one first.")
	}
	if ok, _ := c.FS.Exists(keyPath); !ok {
		return capability.Result{}, errx.New(errx.KindConflict, "mail_key_missing",
			"The mail hostname's private key is missing.")
	}

	// 1. Postfix main.cf: server certificate, opportunistic TLS on port 25 both
	// ways, and the Dovecot SASL socket that submission authenticates against.
	// Fixed keys; the only variable is the pinned, validated cert path.
	mainCF := []string{
		"smtpd_tls_cert_file=" + certPath,
		"smtpd_tls_key_file=" + keyPath,
		"smtpd_tls_security_level=may",
		"smtpd_tls_auth_only=yes",
		"smtp_tls_security_level=may",
		"smtpd_sasl_type=dovecot",
		"smtpd_sasl_path=private/auth",
		"smtpd_tls_loglevel=1",
		"tls_server_sni_maps=",
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: append([]string{"-e"}, mainCF...), Timeout: 30 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed",
			"Could not apply the postfix TLS settings.")
	}

	// 2. master.cf services via `postconf -M` (idempotent overwrite of the whole
	// service line, so a re-run cannot duplicate or corrupt it).
	for _, svc := range []struct{ key, line string }{
		{"submission/inet", "submission inet n - y - - smtpd"},
		{"smtps/inet", "smtps inet n - y - - smtpd"},
	} {
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: postconfPath, Args: []string{"-M", svc.key + "=" + svc.line}, Timeout: 30 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "postconf_master_failed",
				"Could not define the "+svc.key+" service.")
		}
	}

	// 3. Per-service overrides via `postconf -P`. Both authenticated-only:
	// submission demands STARTTLS (encrypt), smtps is implicit TLS (wrappermode),
	// and only SASL-authenticated clients may relay — an open relay is the one
	// mistake a mail server must never make.
	overrides := []string{
		"submission/inet/syslog_name=postfix/submission",
		"submission/inet/smtpd_tls_security_level=encrypt",
		"submission/inet/smtpd_sasl_auth_enable=yes",
		"submission/inet/smtpd_sasl_type=dovecot",
		"submission/inet/smtpd_sasl_path=private/auth",
		"submission/inet/smtpd_client_restrictions=permit_sasl_authenticated,reject",
		"submission/inet/smtpd_relay_restrictions=permit_sasl_authenticated,reject",
		"smtps/inet/syslog_name=postfix/smtps",
		"smtps/inet/smtpd_tls_wrappermode=yes",
		"smtps/inet/smtpd_sasl_auth_enable=yes",
		"smtps/inet/smtpd_sasl_type=dovecot",
		"smtps/inet/smtpd_sasl_path=private/auth",
		"smtps/inet/smtpd_client_restrictions=permit_sasl_authenticated,reject",
		"smtps/inet/smtpd_relay_restrictions=permit_sasl_authenticated,reject",
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: append([]string{"-P"}, overrides...), Timeout: 30 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "postconf_override_failed",
			"Could not apply the submission/smtps service overrides.")
	}

	// 4. Dovecot TLS drop-in: the same certificate, TLS required, and the
	// postfix-private auth socket submission talks to. imaps/993 (and pop3s/995
	// where dovecot-pop3d is present) come up automatically once ssl is set.
	dropin := "# NexPanel mail TLS (rendered; do not edit).\n" +
		"ssl = required\n" +
		"ssl_cert = <" + certPath + "\n" +
		"ssl_key = <" + keyPath + "\n" +
		"ssl_min_protocol = TLSv1.2\n" +
		"service auth {\n" +
		"  unix_listener /var/spool/postfix/private/auth {\n" +
		"    mode = 0660\n" +
		"    user = postfix\n" +
		"    group = postfix\n" +
		"  }\n" +
		"}\n"
	if err := c.FS.WriteFile(dovecotSSLDropin, []byte(dropin), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "dovecot_ssl_failed",
			"Could not write the dovecot TLS drop-in.")
	}

	reloaded := reloadMailDaemons(c)
	return capability.Result{Data: map[string]any{
		"tls":      true,
		"hostname": in.Hostname,
		"reloaded": reloaded,
		"ports":    []int{587, 993, 465},
	}}, nil
}
