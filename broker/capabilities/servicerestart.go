// Package capabilities holds the concrete privileged operations the broker can
// perform. Each type implements capability.Capability and is registered into the
// broker's registry via All().
package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// systemctlPath is the absolute path to systemctl on the target systems.
const systemctlPath = "/usr/bin/systemctl"

// ServiceRestart restarts an allowlisted system service via systemctl.
type ServiceRestart struct{}

type serviceRestartInput struct {
	Service string `json:"service"`
}

// Name implements capability.Capability.
func (ServiceRestart) Name() string { return "service.restart" }

// Execute implements capability.Capability.
func (ServiceRestart) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in serviceRestartInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for service.restart.")
	}
	if err := capability.ValidateServiceName(in.Service, c.Policy); err != nil {
		return capability.Result{}, err
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    systemctlPath,
		Args:    []string{"restart", in.Service},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "restart_failed", "Failed to restart the service.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "restart_failed",
			"The service restart command returned a non-zero exit code.")
	}
	return capability.Result{Data: map[string]any{
		"service":   in.Service,
		"restarted": true,
	}}, nil
}
