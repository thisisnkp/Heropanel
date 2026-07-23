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

// DKIM signing via OpenDKIM. hpd renders the KeyTable/SigningTable and unseals
// each domain's private key; this capability writes them to pinned paths
// (key files 0600, opendkim-owned), owns the opendkim.conf (constant — nothing
// in it is user input), wires postfix's milter settings (constant), and
// reloads. Declarative render-all: the input is the COMPLETE signer state.

const (
	opendkimDir        = "/etc/opendkim/heropanel"
	opendkimKeysDir    = opendkimDir + "/keys"
	opendkimConfPath   = "/etc/opendkim.conf"
	opendkimUser       = "opendkim"
	opendkimSocketAddr = "inet:8891@localhost"
)

// reDKIMSelector bounds a selector name.
var reDKIMSelector = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// maxDKIMKey bounds one PEM private key (an RSA-4096 PEM is ~3 KiB).
const maxDKIMKey = 16 << 10

// opendkimConf is the complete opendkim configuration. Constant.
const opendkimConf = `# HeroPanel DKIM configuration (rendered; do not edit).
Syslog                  yes
UMask                   007
Mode                    sv
Canonicalization        relaxed/simple
Socket                  ` + opendkimSocketAddr + `
PidFile                 /run/opendkim/opendkim.pid
UserID                  ` + opendkimUser + `
KeyTable                ` + opendkimDir + `/keytable
SigningTable            refile:` + opendkimDir + `/signingtable
InternalHosts           ` + opendkimDir + `/trustedhosts
`

// opendkimTrusted is who may send unauthenticated mail for signing: this host.
const opendkimTrusted = "127.0.0.1\n::1\nlocalhost\n"

// dkimMilterSettings wires postfix to the signer. Fixed keys, fixed values
// (localhost:8891 is opendkim's Socket above, in postfix's own notation).
var dkimMilterSettings = []string{
	"smtpd_milters=inet:localhost:8891",
	"non_smtpd_milters=inet:localhost:8891",
	"milter_default_action=accept",
	"milter_protocol=6",
}

// MailDKIMApply writes the complete DKIM signer state and reloads.
type MailDKIMApply struct{}

type dkimKeyEntry struct {
	Domain     string `json:"domain"`
	Selector   string `json:"selector"`
	PrivatePEM string `json:"private_pem"`
}

type mailDKIMApplyInput struct {
	Keys         []dkimKeyEntry `json:"keys"`
	KeyTable     string         `json:"keytable"`
	SigningTable string         `json:"signingtable"`
}

// Name implements capability.Capability.
func (MailDKIMApply) Name() string { return "mail.dkim.apply" }

// Execute implements capability.Capability.
func (MailDKIMApply) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailDKIMApplyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.dkim.apply.")
	}
	if len(in.KeyTable) > mailMapMax || len(in.SigningTable) > mailMapMax {
		return capability.Result{}, errx.Validation("bad_input", "A rendered DKIM table is invalid.")
	}
	for _, k := range in.Keys {
		if err := capability.ValidateFQDN(k.Domain); err != nil {
			return capability.Result{}, err
		}
		if !reDKIMSelector.MatchString(k.Selector) {
			return capability.Result{}, errx.Validation("invalid_selector", "Invalid DKIM selector.")
		}
		if len(k.PrivatePEM) > maxDKIMKey || !strings.HasPrefix(k.PrivatePEM, "-----BEGIN") {
			return capability.Result{}, errx.Validation("invalid_key", "Invalid DKIM private key.")
		}
	}

	// 1. Key files, one directory per domain, private to opendkim. The path is
	// derived from the validated domain + selector, never taken from input.
	for _, k := range in.Keys {
		dir := opendkimKeysDir + "/" + k.Domain
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path:    installPath,
			Args:    []string{"-d", "-m", "0750", "-o", opendkimUser, "-g", opendkimUser, dir},
			Timeout: 20 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "dkim_dir_failed", "Could not create the DKIM key directory.")
		}
		keyPath := dir + "/" + k.Selector + ".private"
		if err := c.FS.WriteFile(keyPath, []byte(k.PrivatePEM), 0o600); err != nil {
			return capability.Result{}, errx.Upstream(err, "dkim_key_failed", "Could not write a DKIM key.")
		}
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: chownPath, Args: []string{opendkimUser + ":" + opendkimUser, keyPath}, Timeout: 20 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "dkim_key_failed", "Could not hand a DKIM key to opendkim.")
		}
	}

	// 2. Tables, trusted hosts, and the signer's own configuration.
	for path, content := range map[string]string{
		opendkimDir + "/keytable":     in.KeyTable,
		opendkimDir + "/signingtable": in.SigningTable,
		opendkimDir + "/trustedhosts": opendkimTrusted,
		opendkimConfPath:              opendkimConf,
	} {
		if err := c.FS.WriteFile(path, []byte(content), 0o644); err != nil {
			return capability.Result{}, errx.Upstream(err, "dkim_write_failed", "Could not write the DKIM configuration.")
		}
	}

	// 3. Postfix hands mail through the milter — fixed settings.
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: append([]string{"-e"}, dkimMilterSettings...), Timeout: 30 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed", "Could not wire the DKIM milter into postfix.")
	}

	// 4. Restart the signer (it has no reload) and reload postfix; both
	// tolerate a daemon that is not running yet.
	restarted := false
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"restart", "opendkim"}, Timeout: 60 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		restarted = true
	}
	reloaded := reloadMailDaemons(c)
	return capability.Result{Data: map[string]any{
		"keys_applied": len(in.Keys), "opendkim_restarted": restarted, "reloaded": reloaded,
	}}, nil
}
