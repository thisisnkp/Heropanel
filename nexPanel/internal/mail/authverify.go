package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Full inbound SPF + DMARC verification with rejection, on top of the daemon-free
// HELO/sender/recipient hygiene the inbound *level* already provides. This layer
// runs two extra daemons:
//
//   - policyd-spf: a postfix policy service that evaluates the sender's SPF
//     record and (in reject mode) turns away a hard SPF fail at RCPT time.
//   - OpenDMARC: a milter that evaluates DMARC *alignment* (does an SPF- or
//     DKIM-authenticated identifier line up with the From: domain?) against the
//     From: domain's published policy, and (in reject mode) rejects a failure.
//
// The two integration points are surfaces other capabilities own — policyd-spf
// slots into smtpd_recipient_restrictions (the inbound level owns that) and the
// DMARC milter into smtpd_milters (DKIM owns that) — so the broker composes
// them read-modify-write rather than overwriting, and OpenDMARC is ordered
// AFTER OpenDKIM so a DKIM pass is visible to the alignment check.
//
// Three states: off (neither daemon in the path), monitor (both evaluate and
// stamp Authentication-Results but never reject), enforce (a hard SPF fail or a
// DMARC failure with the domain's policy asking for it is rejected).

// AuthVerifyState is the applied SPF/DMARC posture.
type AuthVerifyState struct {
	Mode string `json:"mode"` // off | monitor | enforce
}

var validAuthVerifyModes = map[string]bool{"off": true, "monitor": true, "enforce": true}

// SetAuthVerify applies the SPF/DMARC verification posture through the broker.
// npd renders both daemon configs (pure, over the mode and the mail hostname as
// the DMARC AuthservID); the broker wires them into postfix and (de)activates
// OpenDMARC. off removes both from the path.
func (s *Service) SetAuthVerify(ctx context.Context, mode string) (*AuthVerifyState, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !validAuthVerifyModes[mode] {
		return nil, errx.Validation("invalid_mode", "Mode must be off, monitor or enforce.")
	}
	reject := mode == "enforce"
	authservID := s.hostname
	if authservID == "" {
		authservID = "NexPanel"
	}
	if _, err := s.broker.Invoke(ctx, "mail.authverify", map[string]any{
		"enabled":    mode != "off",
		"spf_conf":   RenderPolicydSPFConf(reject),
		"dmarc_conf": RenderOpenDMARCConf(authservID, reject),
	}); err != nil {
		return nil, err
	}
	return s.AuthVerifyStatus(ctx)
}

// AuthVerifyStatus infers the posture from the effective postfix + OpenDMARC
// state the broker reads back (the honest source of truth, like the inbound
// level).
func (s *Service) AuthVerifyStatus(ctx context.Context) (*AuthVerifyState, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := s.broker.Invoke(ctx, "mail.authverify.status", map[string]any{})
	if err != nil {
		return nil, err
	}
	milters, _ := res["milters"].(string)
	restrictions, _ := res["recipient_restrictions"].(string)
	dmarcReject, _ := res["dmarc_reject"].(bool)
	spfReject, _ := res["spf_reject"].(bool)

	inPath := strings.Contains(milters, "8893") && strings.Contains(restrictions, "policyd-spf")
	switch {
	case !inPath:
		return &AuthVerifyState{Mode: "off"}, nil
	case dmarcReject || spfReject:
		return &AuthVerifyState{Mode: "enforce"}, nil
	default:
		return &AuthVerifyState{Mode: "monitor"}, nil
	}
}

// RenderPolicydSPFConf renders the policyd-spf config. In reject mode a HELO or
// MAIL FROM that hard-fails SPF is rejected; otherwise the check only stamps a
// Received-SPF header (monitor). PermError/TempError never reject or defer, so a
// broken sender DNS zone cannot bounce legitimate mail. Local submission is
// skipped so authenticated clients are never SPF-checked against this host.
func RenderPolicydSPFConf(reject bool) string {
	var b strings.Builder
	b.WriteString("# NexPanel SPF policy (rendered; do not edit).\n")
	b.WriteString("debugLevel = 1\n")
	if reject {
		b.WriteString("TestOnly = 0\n")
		b.WriteString("HELO_reject = Fail\n")
		b.WriteString("Mail_From_reject = Fail\n")
	} else {
		b.WriteString("TestOnly = 1\n")
		b.WriteString("HELO_reject = False\n")
		b.WriteString("Mail_From_reject = False\n")
	}
	b.WriteString("PermError_reject = False\n")
	b.WriteString("TempError_Defer = False\n")
	b.WriteString("skip_addresses = 127.0.0.0/8,::ffff:127.0.0.0/104,::1\n")
	return b.String()
}

// RenderOpenDMARCConf renders the OpenDMARC milter config. It evaluates DMARC
// alignment and, in reject mode, honours a From: domain policy that asks for
// rejection (RejectFailures). Authenticated (local) clients are ignored so the
// panel's own outbound is never DMARC-rejected; SPFSelfValidate lets OpenDMARC
// evaluate SPF itself when no upstream SPF result is present.
func RenderOpenDMARCConf(authservID string, reject bool) string {
	var b strings.Builder
	b.WriteString("# NexPanel DMARC configuration (rendered; do not edit).\n")
	fmt.Fprintf(&b, "AuthservID %s\n", authservID)
	fmt.Fprintf(&b, "TrustedAuthservIDs %s\n", authservID)
	b.WriteString("Syslog true\n")
	b.WriteString("Socket inet:8893@localhost\n")
	b.WriteString("PidFile /run/opendmarc/opendmarc.pid\n")
	b.WriteString("UserID opendmarc\n")
	b.WriteString("UMask 0002\n")
	b.WriteString("IgnoreAuthenticatedClients true\n")
	b.WriteString("RequiredHeaders true\n")
	b.WriteString("SPFSelfValidate true\n")
	if reject {
		b.WriteString("RejectFailures true\n")
	} else {
		b.WriteString("RejectFailures false\n")
	}
	return b.String()
}
