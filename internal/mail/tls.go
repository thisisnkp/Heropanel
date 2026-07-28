package mail

import (
	"context"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Mail TLS on the hpd side: submission/587, imaps/993 and smtps/465 all present
// ONE certificate — the mail host's own (mail.example.com), not a per-domain
// cert. hpd ensures that certificate is installed (delegating to the SSL
// module) and asks the broker to wire it into Postfix and Dovecot. The hostname
// is operator configuration (HP_MAIL_HOSTNAME): a mail server's identity is a
// deployment decision, not something a hosted domain gets to choose.

// CertProvider ensures a server certificate is installed on disk for the mail
// host. Implemented by an adapter over the SSL module: a real Let's Encrypt
// cert if the operator has issued one, otherwise a self-signed fallback so TLS
// works out of the box (the same posture as every other panel-served site).
type CertProvider interface {
	EnsureCert(ctx context.Context, hostname string) error
}

// TLSStatus reports the mail TLS posture for the UI.
type TLSStatus struct {
	Hostname   string `json:"hostname"`
	Ready      bool   `json:"ready"`   // a hostname is configured
	Enabled    bool   `json:"enabled"` // TLS has been wired at least once
	Submission int    `json:"submission_port"`
	IMAPS      int    `json:"imaps_port"`
	SMTPS      int    `json:"smtps_port"`
}

// WithTLS configures the mail host FQDN and the certificate provider used to
// make sure a certificate exists before wiring. An empty hostname leaves TLS
// disabled (the module still delivers mail on port 25 in cleartext, as before).
func (s *Service) WithTLS(hostname string, cp CertProvider) *Service {
	s.hostname = strings.ToLower(strings.TrimSpace(hostname))
	s.certs = cp
	return s
}

// TLSConfigured reports whether a mail hostname has been set.
func (s *Service) TLSConfigured() bool { return s.hostname != "" }

// EnableTLS ensures the mail host's certificate is installed and wires it into
// Postfix + Dovecot, opening submission/587, imaps/993 and smtps/465. It is
// idempotent: re-running re-applies the same fixed configuration.
func (s *Service) EnableTLS(ctx context.Context) (*TLSStatus, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	if s.hostname == "" {
		return nil, errx.Validation("mail_hostname_unset",
			"Set a mail hostname (HP_MAIL_HOSTNAME) before enabling TLS.")
	}
	if !reDomain.MatchString(s.hostname) || len(s.hostname) > 253 {
		return nil, errx.Validation("invalid_mail_hostname", "The mail hostname is not a valid FQDN.")
	}
	if s.certs != nil {
		if err := s.certs.EnsureCert(ctx, s.hostname); err != nil {
			return nil, err
		}
	}
	if _, err := s.broker.Invoke(ctx, "mail.tls", map[string]any{"hostname": s.hostname}); err != nil {
		return nil, err
	}
	s.tlsEnabled = true
	return s.TLSStatus(), nil
}

// TLSStatus returns the current mail TLS posture (for the UI status card).
func (s *Service) TLSStatus() *TLSStatus {
	return &TLSStatus{
		Hostname:   s.hostname,
		Ready:      s.hostname != "",
		Enabled:    s.tlsEnabled,
		Submission: 587,
		IMAPS:      993,
		SMTPS:      465,
	}
}

// maybeEnableTLS wires TLS best-effort after a domain is provisioned, so a
// freshly created mail host is reachable by real mail clients without a second
// call. A failure here never fails the domain create — TLS can be (re)enabled
// explicitly, and the status card shows it is not yet on.
func (s *Service) maybeEnableTLS(ctx context.Context) {
	if s.hostname == "" {
		return
	}
	_, _ = s.EnableTLS(ctx)
}
