package capabilities

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Full inbound SPF + DMARC verification with rejection (policyd-spf + OpenDMARC),
// the deeper follow-up to the daemon-free inbound level. Both integration points
// are surfaces other capabilities own — policyd-spf slots into
// smtpd_recipient_restrictions (the inbound level owns it) and the DMARC milter
// into smtpd_milters (DKIM owns it) — so this composes read-modify-write:
// it reads the live postconf value and inserts/removes just its own token,
// leaving everything else intact and ordering OpenDMARC AFTER OpenDKIM.

const (
	policydSPFConfPath = "/etc/postfix-policyd-spf-python/policyd-spf.conf"
	policydSPFBin      = "/usr/bin/policyd-spf"
	policydSPFService  = "policyd-spf/unix"
	policydSPFPolicy   = "check_policy_service unix:private/policyd-spf"
	// The master.cf spawn entry policyd-spf needs, as postconf -M expects it.
	policydSPFMaster = "policyd-spf unix - n n - 0 spawn user=policyd-spf argv=/usr/bin/policyd-spf"

	opendmarcConfPath = "/etc/opendmarc.conf"
	opendmarcBin      = "/usr/sbin/opendmarc"
	opendmarcMilter   = "inet:localhost:8893"
)

// MailAuthVerify applies (or removes) the SPF/DMARC verification path.
type MailAuthVerify struct{}

type mailAuthVerifyInput struct {
	Enabled   bool   `json:"enabled"`
	SPFConf   string `json:"spf_conf"`
	DMARCConf string `json:"dmarc_conf"`
}

// Name implements capability.Capability.
func (MailAuthVerify) Name() string { return "mail.authverify" }

// Execute implements capability.Capability.
func (MailAuthVerify) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailAuthVerifyInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.authverify.")
	}
	if in.Enabled {
		if len(in.SPFConf) == 0 || len(in.SPFConf) > mailMapMax || strings.ContainsRune(in.SPFConf, 0) ||
			len(in.DMARCConf) == 0 || len(in.DMARCConf) > mailMapMax || strings.ContainsRune(in.DMARCConf, 0) {
			return capability.Result{}, errx.Validation("bad_config", "A rendered SPF/DMARC config is invalid.")
		}
		// Daemon configs first, so the daemons are correct before postfix routes to
		// them.
		if err := c.FS.WriteFile(policydSPFConfPath, []byte(in.SPFConf), 0o644); err != nil {
			return capability.Result{}, errx.Upstream(err, "spf_write_failed", "Could not write the SPF policy config.")
		}
		if err := c.FS.WriteFile(opendmarcConfPath, []byte(in.DMARCConf), 0o644); err != nil {
			return capability.Result{}, errx.Upstream(err, "dmarc_write_failed", "Could not write the DMARC config.")
		}
		// The master.cf spawn service for the SPF policy daemon (idempotent).
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: postconfPath, Args: []string{"-M", policydSPFService + "=" + policydSPFMaster}, Timeout: 30 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed", "Could not register the SPF policy service.")
		}
		if res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: postconfPath, Args: []string{"-e", "policyd-spf_time_limit=3600"}, Timeout: 30 * time.Second,
		}); err != nil || res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed", "Could not set the SPF policy time limit.")
		}
	}

	// Compose the milter chain and recipient restrictions read-modify-write.
	if err := setPostconf(c, "smtpd_milters", ensureToken(readPostconf(c, "smtpd_milters"), opendmarcMilter, in.Enabled, tokenAppend)); err != nil {
		return capability.Result{}, err
	}
	if err := setPostconf(c, "smtpd_recipient_restrictions", ensureToken(readPostconf(c, "smtpd_recipient_restrictions"), policydSPFPolicy, in.Enabled, tokenBeforePermit)); err != nil {
		return capability.Result{}, err
	}

	// (De)activate OpenDMARC. Best-effort like the other mail daemons: on a host
	// without systemd this is a no-op and the config still stands.
	dmarcActive := false
	action := "restart"
	if !in.Enabled {
		action = "stop"
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{action, "opendmarc"}, Timeout: 60 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		dmarcActive = in.Enabled
	}
	reloaded := reloadMailDaemons(c)
	return capability.Result{Data: map[string]any{
		"enabled": in.Enabled, "dmarc_active": dmarcActive, "reloaded": reloaded,
	}}, nil
}

// MailAuthVerifyStatus reports the effective SPF/DMARC state.
type MailAuthVerifyStatus struct{}

// Name implements capability.Capability.
func (MailAuthVerifyStatus) Name() string { return "mail.authverify.status" }

// Execute implements capability.Capability.
func (MailAuthVerifyStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	milters := readPostconf(c, "smtpd_milters")
	restrictions := readPostconf(c, "smtpd_recipient_restrictions")
	// Reject-ness is read from the daemon configs the panel wrote.
	spfReject := false
	if b, err := c.FS.ReadFile(policydSPFConfPath); err == nil {
		spfReject = strings.Contains(string(b), "Mail_From_reject = Fail")
	}
	dmarcReject := false
	if b, err := c.FS.ReadFile(opendmarcConfPath); err == nil {
		dmarcReject = strings.Contains(string(b), "RejectFailures true")
	}
	return capability.Result{Data: map[string]any{
		"milters":                milters,
		"recipient_restrictions": restrictions,
		"spf_reject":             spfReject,
		"dmarc_reject":           dmarcReject,
	}}, nil
}

// tokenPlacement selects where a token is inserted when made present.
type tokenPlacement int

const (
	tokenAppend       tokenPlacement = iota // add at the end (milters: after DKIM)
	tokenBeforePermit                       // insert before the trailing permit (restrictions)
)

// readPostconf returns a postfix parameter's live value ("" on any error).
func readPostconf(c capability.Context, key string) string {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: []string{"-h", key}, Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

// setPostconf writes a postfix parameter value.
func setPostconf(c capability.Context, key, value string) error {
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: []string{"-e", key + "=" + value}, Timeout: 30 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return errx.New(errx.KindUpstream, "postconf_failed", "Could not update the postfix configuration.")
	}
	return nil
}

// ensureToken adds or removes a single comma-separated token in a postfix list
// value, preserving the other entries and their order. It is idempotent: adding
// a present token or removing an absent one is a no-op. Pure, so the fragile
// composition logic is unit-tested without a live postfix.
func ensureToken(current, token string, present bool, placement tokenPlacement) string {
	parts := splitList(current)
	idx := -1
	for i, p := range parts {
		if p == token {
			idx = i
			break
		}
	}
	if present && idx == -1 {
		switch placement {
		case tokenBeforePermit:
			// Insert before a trailing bare "permit" if there is one, else append.
			if n := len(parts); n > 0 && parts[n-1] == "permit" {
				parts = append(parts[:n-1], token, "permit")
			} else {
				parts = append(parts, token)
			}
		default:
			parts = append(parts, token)
		}
	} else if !present && idx != -1 {
		parts = append(parts[:idx], parts[idx+1:]...)
	}
	return strings.Join(parts, ",")
}

// splitList splits a postfix list value on commas, trimming surrounding
// whitespace and dropping empties. Splitting on commas ONLY (not whitespace) is
// deliberate: a single restriction can contain a space — `check_policy_service
// unix:private/policyd-spf` is one entry, not two — and the panel writes all its
// postfix lists comma-separated.
func splitList(s string) []string {
	out := make([]string, 0, 8)
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
