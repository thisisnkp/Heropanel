package capabilities

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// ServiceStatus reports whether allowlisted system services are running.
//
// It is the read twin of ServiceRestart, and it shares that capability's
// allowlist: a caller may ask about exactly the services it may act on, and no
// others. `systemctl is-active` is not privileged — a non-root user can read it —
// but it is routed through the broker anyway so hpd never execs a system binary
// itself, keeping the "unprivileged process asks, privileged process acts" line
// unbroken. The states drive the dashboard's service tiles.
type ServiceStatus struct{}

func (ServiceStatus) Name() string { return "service.status" }

// maxServiceQuery bounds a single status request so a caller cannot ask about an
// unbounded list.
const maxServiceQuery = 32

func (ServiceStatus) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Services []string `json:"services"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for service.status.")
	}
	if len(in.Services) == 0 {
		return capability.Result{Data: map[string]any{"statuses": []any{}}}, nil
	}
	if len(in.Services) > maxServiceQuery {
		return capability.Result{}, errx.Validation("too_many_services",
			"Too many services in a single status request.")
	}
	for _, s := range in.Services {
		if err := capability.ValidateServiceName(s, c.Policy); err != nil {
			return capability.Result{}, err
		}
	}

	// `is-active a b c` prints one state per unit, in order, and exits non-zero if
	// any is not active — which is not an error here: "inactive" and "failed" are
	// exactly the answers a status check exists to surface, so the exit code is
	// ignored and stdout is parsed regardless.
	args := append([]string{"is-active"}, in.Services...)
	res, err := c.Runner.Run(c.Ctx, exec.Command{Path: systemctlPath, Args: args, Timeout: 15 * time.Second})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "status_failed", "Could not read service status.")
	}

	lines := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	statuses := make([]map[string]any, 0, len(in.Services))
	for i, s := range in.Services {
		state := "unknown"
		if i < len(lines) {
			if v := strings.TrimSpace(lines[i]); v != "" {
				state = v
			}
		}
		statuses = append(statuses, map[string]any{"service": s, "state": state})
	}
	return capability.Result{Data: map[string]any{"statuses": statuses}}, nil
}
