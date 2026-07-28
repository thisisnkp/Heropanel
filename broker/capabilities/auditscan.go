package capabilities

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Host audit scanners: rkhunter (rootkit hunter) and lynis (system auditor).
// Like the ClamAV/AIDE integrations, this shells out to the real tool with a
// fixed argv and parses its output — a rootkit warning count for rkhunter, a
// hardening index + warning/suggestion counts for lynis. maldet is deliberately
// not wired: it is another ClamAV-style malware scanner and would duplicate the
// malware module, whereas rkhunter and lynis add genuinely different signal
// (rootkit heuristics and a configuration audit).

const (
	rkhunterPath = "/usr/bin/rkhunter"
	lynisPath    = "/usr/sbin/lynis"
)

// AuditScan runs a named host-audit tool and returns its parsed findings.
type AuditScan struct{}

type auditScanInput struct {
	Tool string `json:"tool"` // rkhunter | lynis
}

// Name implements capability.Capability.
func (AuditScan) Name() string { return "audit.scan" }

// Execute implements capability.Capability.
func (AuditScan) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in auditScanInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for audit.scan.")
	}
	switch in.Tool {
	case "rkhunter":
		return runRkhunter(c)
	case "lynis":
		return runLynis(c)
	default:
		return capability.Result{}, errx.Validation("invalid_tool", "Tool must be rkhunter or lynis.")
	}
}

func runRkhunter(c capability.Context) (capability.Result, error) {
	// Refresh the file-property baseline first so the check does not flag every
	// binary as "properties changed" on a fresh system; failures here are not
	// fatal to the scan.
	_, _ = c.Runner.Run(c.Ctx, exec.Command{
		Path: rkhunterPath, Args: []string{"--propupd", "--nocolors"}, Timeout: 5 * time.Minute,
	})
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: rkhunterPath, Args: []string{"--check", "--sk", "--nocolors"}, Timeout: 10 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "rkhunter_failed", "The rkhunter scan could not run.")
	}
	out := string(res.Stdout)
	// rkhunter marks each finding "[ Warning ]"; the tail also prints a summary.
	warnings := strings.Count(out, "[ Warning ]")
	if n, ok := grepInt(out, "Warnings found"); ok {
		warnings = n
	}
	return capability.Result{Data: map[string]any{
		"tool": "rkhunter", "warnings": warnings, "report": clampReport(out),
	}}, nil
}

func runLynis(c capability.Context) (capability.Result, error) {
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: lynisPath, Args: []string{"audit", "system", "--quick", "--no-colors"}, Timeout: 10 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "lynis_failed", "The lynis audit could not run.")
	}
	out := string(res.Stdout)
	score, hasScore := grepInt(out, "Hardening index")
	warnings := strings.Count(out, "Warning: ")
	suggestions := strings.Count(out, "Suggestion: ")
	data := map[string]any{
		"tool": "lynis", "warnings": warnings, "suggestions": suggestions,
		"report": clampReport(out),
	}
	if hasScore {
		data["hardening_index"] = score
	}
	return capability.Result{Data: data}, nil
}

// grepInt finds the first line containing label and returns the first integer
// after it (e.g. "Hardening index : 67 [######...]" -> 67).
func grepInt(out, label string) (int, bool) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, label) {
			continue
		}
		i := strings.Index(line, label) + len(label)
		for _, f := range strings.FieldsFunc(line[i:], func(r rune) bool { return r < '0' || r > '9' }) {
			if n, err := strconv.Atoi(f); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}
