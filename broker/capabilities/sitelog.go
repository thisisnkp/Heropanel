package capabilities

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Reading a site's web-server logs.
//
// This needs the broker at all because the logs are the site's own: the tree is
// 0750 owned by the site's dedicated Linux user, and hpd runs unprivileged as
// the panel user, in none of those groups. It cannot read them, and the fix is
// emphatically not to loosen the mode — the isolation those 0750s buy is the
// whole point of a per-site user.

const tailPath = "/usr/bin/tail"

// logKinds is an allowlist, and it is doing security work, not tidiness. The
// kind is concatenated into a path; anything unbounded here ("../../etc/shadow")
// would turn this capability into an arbitrary-file-read for root. An allowlist
// means the only reachable filenames are these two, whatever the caller sends.
var logKinds = map[string]string{
	"access": "access.log",
	"error":  "error.log",
}

const (
	minLogLines     = 1
	maxLogLines     = 5000
	defaultLogLines = 200
)

// SiteReadLog returns the last N lines of one of a site's log files.
type SiteReadLog struct{}

type siteReadLogInput struct {
	Root  string `json:"root"`  // the site's home directory
	Kind  string `json:"kind"`  // access | error
	Lines int    `json:"lines"` // tail depth
}

// Name implements capability.Capability.
func (SiteReadLog) Name() string { return "site.read_log" }

// Execute implements capability.Capability.
func (SiteReadLog) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in siteReadLogInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for site.read_log.")
	}
	if err := capability.ValidatePath(in.Root, c.Policy); err != nil {
		return capability.Result{}, err
	}
	file, ok := logKinds[in.Kind]
	if !ok {
		return capability.Result{}, errx.Validation("bad_log_kind", "Unknown log kind.")
	}
	lines := in.Lines
	if lines == 0 {
		lines = defaultLogLines
	}
	if lines < minLogLines || lines > maxLogLines {
		return capability.Result{}, errx.Validation("bad_log_lines",
			"lines must be between "+strconv.Itoa(minLogLines)+" and "+strconv.Itoa(maxLogLines)+".")
	}

	path := in.Root + "/logs/" + file
	// Defense in depth: the joined path must still sit under an allowed root.
	if err := capability.ValidatePath(path, c.Policy); err != nil {
		return capability.Result{}, err
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    tailPath,
		Args:    []string{"-n", strconv.Itoa(lines), "--", path},
		Timeout: 20 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "log_read_failed", "Failed to read the site log.")
	}
	if res.ExitCode != 0 {
		// A log file that does not exist yet is the normal state of a site nobody
		// has visited. Reporting that as an error would have the UI show a fault
		// where the truth is "no requests yet".
		return capability.Result{Data: map[string]any{
			"kind": in.Kind, "path": path, "lines": 0, "content": "", "exists": false,
		}}, nil
	}

	content := string(res.Stdout)
	return capability.Result{Data: map[string]any{
		"kind":    in.Kind,
		"path":    path,
		"lines":   countLines(content),
		"content": content,
		"exists":  true,
	}}, nil
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	// A trailing line without a newline still counts.
	if len(s) > 0 && s[len(s)-1] != '\n' {
		n++
	}
	return n
}
