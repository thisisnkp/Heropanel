package security

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// File-integrity monitoring (AIDE): build a baseline of the panel's
// security-critical files and detect tampering. The broker owns the tool; the
// service is a thin orchestration + the check-result shape for the UI.

// FIMResult is the outcome of a check against the baseline.
type FIMResult struct {
	Changed      bool   `json:"changed"`
	Added        int    `json:"added"`
	Removed      int    `json:"removed"`
	ChangedCount int    `json:"changed_count"`
	Scope        string `json:"scope"`
	Report       string `json:"report"`
}

// FIMStatus reports whether a baseline exists, and at what scope.
type FIMStatus struct {
	Baseline    bool   `json:"baseline"`
	ToolPresent bool   `json:"tool_present"`
	Scope       string `json:"scope"` // panel | host | "" (no baseline)
}

// FIM orchestrates file-integrity monitoring.
type FIM struct {
	broker broker.Gateway
}

// NewFIM constructs the service.
func NewFIM(gw broker.Gateway) *FIM { return &FIM{broker: gw} }

// Available reports whether FIM can run.
func (f *FIM) Available() bool { return f != nil && f.broker != nil }

func (f *FIM) requireAvailable() error {
	if f.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "fim_unavailable", "File-integrity monitoring needs the broker.")
}

// Init builds (or rebuilds) the baseline database at the given scope
// ("panel" watches the panel's own files; "host" extends to the wider system).
// An empty or unknown scope falls back to "panel" at the broker. It returns the
// scope the broker actually applied.
func (f *FIM) Init(ctx context.Context, scope string) (string, error) {
	if err := f.requireAvailable(); err != nil {
		return "", err
	}
	res, err := f.broker.Invoke(ctx, "fim.init", map[string]any{"scope": scope})
	if err != nil {
		return "", err
	}
	return stringOf(res["scope"]), nil
}

// Check compares the filesystem against the baseline.
func (f *FIM) Check(ctx context.Context) (*FIMResult, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := f.broker.Invoke(ctx, "fim.check", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &FIMResult{
		Changed:      boolOf(res["changed"]),
		Added:        intOf(res["added"]),
		Removed:      intOf(res["removed"]),
		ChangedCount: intOf(res["changed_count"]),
		Scope:        stringOf(res["scope"]),
		Report:       stringOf(res["report"]),
	}, nil
}

// Status reports whether a baseline exists.
func (f *FIM) Status(ctx context.Context) (*FIMStatus, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := f.broker.Invoke(ctx, "fim.status", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &FIMStatus{Baseline: boolOf(res["baseline"]), ToolPresent: boolOf(res["tool_present"]), Scope: stringOf(res["scope"])}, nil
}

// small coercions for the broker's any-typed map (JSON numbers arrive float64).
func boolOf(v any) bool { b, _ := v.(bool); return b }
func stringOf(v any) string {
	s, _ := v.(string)
	return s
}
func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
