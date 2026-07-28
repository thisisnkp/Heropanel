package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Passwordless webmail SSO: the panel renders the COMPLETE set of live one-time
// Dovecot master users into the master passwd-file and this capability writes it
// (dovecot-owned 0600) and reloads dovecot. Declarative render-all, exactly like
// the mailbox users file — the input is the entire desired master-user set, so a
// grant or a revoke is just a re-render, and a crash never leaves a half-written
// file. The master passdb block itself already lives in the dovecot drop-in
// (inert while this file is empty).
type MailSSOApply struct{}

type mailSSOApplyInput struct {
	// Master is the full contents of the master passwd-file: one
	// `user:{BLF-CRYPT}$…` line per live session, or empty to disable all SSO.
	Master string `json:"master"`
}

// Name implements capability.Capability.
func (MailSSOApply) Name() string { return "mail.sso.apply" }

// Execute implements capability.Capability.
func (MailSSOApply) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailSSOApplyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.sso.apply.")
	}
	if len(in.Master) > mailMapMax {
		return capability.Result{}, errx.Validation("bad_input", "The rendered master file is too large.")
	}
	if err := c.FS.WriteFile(dovecotMasterPath, []byte(in.Master), 0o600); err != nil {
		return capability.Result{}, errx.Upstream(err, "mail_sso_failed", "Could not write the SSO master file.")
	}
	// Hand it to dovecot's auth process (which runs as the dovecot user).
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: chownPath, Args: []string{dovecotUser + ":" + dovecotUser, dovecotMasterPath}, Timeout: 20 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "mail_sso_failed",
			"Could not hand the SSO master file to dovecot.")
	}
	// passwd-file passdbs are re-read per lookup, but a reload makes a config or
	// permission change take effect immediately and tolerates dovecot not running.
	reloaded := false
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: doveadmPath, Args: []string{"reload"}, Timeout: 30 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		reloaded = true
	}
	return capability.Result{Data: map[string]any{"applied": true, "reloaded": reloaded}}, nil
}
