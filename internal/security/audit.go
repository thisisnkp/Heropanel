package security

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Host audit scanners (rkhunter, lynis). Thin orchestration over the broker,
// plus the result shape the UI shows. Tool selection is validated here so an
// unknown tool never reaches the broker.

// AuditResult is one scanner run.
type AuditResult struct {
	Tool           string `json:"tool"`
	Warnings       int    `json:"warnings"`
	Suggestions    int    `json:"suggestions,omitempty"`
	HardeningIndex int    `json:"hardening_index,omitempty"`
	Report         string `json:"report"`
}

// Audit orchestrates the host audit scanners.
type Audit struct {
	broker broker.Gateway
}

// NewAudit constructs the service.
func NewAudit(gw broker.Gateway) *Audit { return &Audit{broker: gw} }

// Available reports whether audit scans can run.
func (a *Audit) Available() bool { return a != nil && a.broker != nil }

var auditTools = map[string]bool{"rkhunter": true, "lynis": true}

// Scan runs the named audit tool and returns its parsed findings.
func (a *Audit) Scan(ctx context.Context, tool string) (*AuditResult, error) {
	if !a.Available() {
		return nil, errx.New(errx.KindUnavailable, "audit_unavailable", "Audit scans need the broker.")
	}
	if !auditTools[tool] {
		return nil, errx.Validation("invalid_tool", "Tool must be rkhunter or lynis.")
	}
	res, err := a.broker.Invoke(ctx, "audit.scan", map[string]any{"tool": tool})
	if err != nil {
		return nil, err
	}
	return &AuditResult{
		Tool:           stringOf(res["tool"]),
		Warnings:       intOf(res["warnings"]),
		Suggestions:    intOf(res["suggestions"]),
		HardeningIndex: intOf(res["hardening_index"]),
		Report:         stringOf(res["report"]),
	}, nil
}
