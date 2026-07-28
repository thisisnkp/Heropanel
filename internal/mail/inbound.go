package mail

import (
	"context"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Inbound verification policy (what the host does with mail it receives). DKIM
// is already verified on the way in (OpenDKIM Mode "sv" stamps an
// Authentication-Results header); this adds the postfix reject-side policy for
// HELO/sender/recipient hygiene. Three levels: off, standard, strict.

// InboundStatus reports the applied inbound policy.
type InboundStatus struct {
	Level              string `json:"level"`
	SenderRestrictions string `json:"sender_restrictions"`
}

var validInboundLevels = map[string]bool{"off": true, "standard": true, "strict": true}

// SetInboundPolicy applies the inbound restriction policy through the broker.
func (s *Service) SetInboundPolicy(ctx context.Context, level string) (*InboundStatus, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	level = strings.ToLower(strings.TrimSpace(level))
	if !validInboundLevels[level] {
		return nil, errx.Validation("invalid_level", "Level must be off, standard or strict.")
	}
	if _, err := s.broker.Invoke(ctx, "mail.inbound", map[string]any{"level": level}); err != nil {
		return nil, err
	}
	return s.InboundStatus(ctx)
}

// InboundStatus returns the effective inbound policy (the sender-restriction
// line postfix would enforce) and infers the level from it.
func (s *Service) InboundStatus(ctx context.Context) (*InboundStatus, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := s.broker.Invoke(ctx, "mail.inbound.status", map[string]any{})
	if err != nil {
		return nil, err
	}
	sr, _ := res["sender_restrictions"].(string)
	return &InboundStatus{Level: inferLevel(sr), SenderRestrictions: strings.TrimSpace(sr)}, nil
}

// inferLevel maps the effective sender-restriction line back to a level.
func inferLevel(sr string) string {
	switch {
	case strings.Contains(sr, "reject_unverified_sender"):
		return "strict"
	case strings.Contains(sr, "reject_unknown_sender_domain"):
		return "standard"
	default:
		return "off"
	}
}
