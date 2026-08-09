package webmail

import (
	"fmt"
	"strings"
)

// The Roundcube config renderer. A pure function over validated settings: the
// same settings always produce the same config.inc.php, so an install is
// reproducible and the broker writes a pinned path. No mailbox password ever
// appears here — Roundcube authenticates each user against Dovecot at login;
// the panel only wires it to talk to the local MTAs over TLS.

// Config is the rendered Roundcube configuration input.
type Config struct {
	// IMAPHost/SMTPHost are always the local MTAs — the whole point is that the
	// panel's own Dovecot/Postfix back the webmail. TLS is used so credentials
	// never cross even the loopback in cleartext.
	IMAPHost string // e.g. "tls://127.0.0.1"
	IMAPPort int    // 143 (STARTTLS) — Roundcube upgrades on tls:// scheme
	SMTPHost string // e.g. "tls://127.0.0.1"
	SMTPPort int    // 587 (submission)
	// DESKey is a 24-char secret Roundcube uses to encrypt session data. The
	// panel generates it (crypto/rand) so it is never a template default.
	DESKey string
	// DBPath is the sqlite database file (Roundcube's own metadata store — no
	// mail, just contacts/prefs). sqlite keeps webmail self-contained.
	DBPath string
	// TempDir/LogDir are writable paths outside the read-only app tree.
	TempDir string
	LogDir  string
	// ProductName is shown in the Roundcube UI title bar.
	ProductName string
	// SkipCertVerify tolerates the self-signed fallback certificate on the
	// loopback IMAP/SMTP connection (a real cert verifies normally).
	SkipCertVerify bool
}

// RenderConfig renders a complete Roundcube config.inc.php.
func RenderConfig(c Config) string {
	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString("// NexPanel webmail configuration (rendered; do not edit).\n")
	b.WriteString("$config = [];\n")
	// Roundcube stores only its own metadata (contacts, prefs) — sqlite is
	// plenty and keeps webmail from needing a database server of its own.
	fmt.Fprintf(&b, "$config['db_dsnw'] = 'sqlite:///%s?mode=0640';\n", c.DBPath)
	// The local MTAs, over TLS.
	fmt.Fprintf(&b, "$config['imap_host'] = '%s:%d';\n", c.IMAPHost, c.IMAPPort)
	fmt.Fprintf(&b, "$config['smtp_host'] = '%s:%d';\n", c.SMTPHost, c.SMTPPort)
	b.WriteString("$config['smtp_user'] = '%u';\n")
	b.WriteString("$config['smtp_pass'] = '%p';\n")
	// Session encryption key (generated per install).
	fmt.Fprintf(&b, "$config['des_key'] = '%s';\n", c.DESKey)
	fmt.Fprintf(&b, "$config['temp_dir'] = '%s';\n", c.TempDir)
	fmt.Fprintf(&b, "$config['log_dir'] = '%s';\n", c.LogDir)
	fmt.Fprintf(&b, "$config['product_name'] = '%s';\n", c.ProductName)
	b.WriteString("$config['enable_installer'] = false;\n")
	b.WriteString("$config['log_driver'] = 'file';\n")
	// The self-signed fallback cert on the loopback: Roundcube must still
	// connect. A real cert verifies normally; this only relaxes verification of
	// the panel's own local certificate.
	if c.SkipCertVerify {
		b.WriteString("$ssl = ['verify_peer' => false, 'verify_peer_name' => false, 'allow_self_signed' => true];\n")
		b.WriteString("$config['imap_conn_options'] = ['ssl' => $ssl];\n")
		b.WriteString("$config['smtp_conn_options'] = ['ssl' => $ssl];\n")
	}
	// A sane default plugin set that ships with Roundcube (no extra downloads).
	b.WriteString("$config['plugins'] = ['archive', 'zipdownload', 'newmail_notifier'];\n")
	return b.String()
}
