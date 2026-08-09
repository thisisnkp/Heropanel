package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Inbound mail verification policy: what Postfix does with mail it RECEIVES.
// DKIM is already verified on the way in (OpenDKIM runs in Mode "sv" and stamps
// an Authentication-Results header); this capability adds the reject-side policy
// — HELO, sender and recipient hygiene that turns unauthenticated, forged-domain
// and open-relay attempts away at the SMTP transaction. The level is a fixed
// enum; only pinned postconf keys/values are written, no free input.
//
// Full SPF and DMARC *rejection* (policyd-spf / OpenDMARC daemons) is a deeper
// follow-up; this is the daemon-free layer that reuses what is already running.

// MailInbound applies the inbound restriction policy.
type MailInbound struct{}

type mailInboundInput struct {
	Level string `json:"level"` // off | standard | strict
}

// Name implements capability.Capability.
func (MailInbound) Name() string { return "mail.inbound" }

// The restriction sets per level. permit_mynetworks stays first so local
// submission (authenticated clients, loopback) is never caught by these.
var inboundPolicies = map[string][]string{
	"off": {
		"smtpd_helo_required=no",
		"smtpd_helo_restrictions=",
		"smtpd_sender_restrictions=",
		"smtpd_recipient_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_unauth_destination,permit",
	},
	"standard": {
		"smtpd_helo_required=yes",
		"smtpd_helo_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_invalid_helo_hostname,reject_non_fqdn_helo_hostname,permit",
		"smtpd_sender_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_non_fqdn_sender,reject_unknown_sender_domain,permit",
		"smtpd_recipient_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_non_fqdn_recipient,reject_unknown_recipient_domain,reject_unauth_destination,permit",
	},
	"strict": {
		"smtpd_helo_required=yes",
		"smtpd_helo_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_invalid_helo_hostname,reject_non_fqdn_helo_hostname,reject_unknown_helo_hostname,permit",
		"smtpd_sender_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_non_fqdn_sender,reject_unknown_sender_domain,reject_unverified_sender,permit",
		"smtpd_recipient_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_non_fqdn_recipient,reject_unknown_recipient_domain,reject_unauth_destination,reject_unauth_pipelining,permit",
	},
}

// Execute implements capability.Capability.
func (MailInbound) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in mailInboundInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for mail.inbound.")
	}
	settings, ok := inboundPolicies[in.Level]
	if !ok {
		return capability.Result{}, errx.Validation("invalid_level", "Level must be off, standard or strict.")
	}
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: append([]string{"-e"}, settings...), Timeout: 30 * time.Second,
	}); err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed",
			"Could not apply the inbound restriction policy.")
	}
	// Reload postfix so the restrictions take effect (tolerant if not running).
	reloaded := false
	if res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postfixPath, Args: []string{"reload"}, Timeout: 30 * time.Second,
	}); err == nil && res.ExitCode == 0 {
		reloaded = true
	}
	return capability.Result{Data: map[string]any{"level": in.Level, "reloaded": reloaded}}, nil
}

// MailInboundStatus reports the effective sender-restriction line (from
// `postconf -h`), which is the honest source of truth for the applied policy.
type MailInboundStatus struct{}

// Name implements capability.Capability.
func (MailInboundStatus) Name() string { return "mail.inbound.status" }

// Execute implements capability.Capability.
func (MailInboundStatus) Execute(c capability.Context, _ json.RawMessage) (capability.Result, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: postconfPath, Args: []string{"-h", "smtpd_sender_restrictions"}, Timeout: 20 * time.Second,
	})
	if err != nil || res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "postconf_failed",
			"Could not read the inbound policy.")
	}
	return capability.Result{Data: map[string]any{"sender_restrictions": string(res.Stdout)}}, nil
}
