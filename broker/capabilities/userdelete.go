package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

const userdelPath = "/usr/sbin/userdel"

// userdelNoSuchUser is userdel's exit code when the user does not exist; we
// treat it as success so deletion is idempotent.
const userdelNoSuchUser = 6

// SystemUserDelete removes a site's dedicated Linux user (its directory tree is
// removed separately via site.remove_dirs).
type SystemUserDelete struct{}

type systemUserDeleteInput struct {
	Username string `json:"username"`
}

// Name implements capability.Capability.
func (SystemUserDelete) Name() string { return "system_user.delete" }

// Execute implements capability.Capability.
func (SystemUserDelete) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in systemUserDeleteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for system_user.delete.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}

	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    userdelPath,
		Args:    []string{in.Username},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "userdel_failed", "Failed to delete the system user.")
	}
	if res.ExitCode != 0 && res.ExitCode != userdelNoSuchUser {
		return capability.Result{}, errx.New(errx.KindUpstream, "userdel_failed",
			"Deleting the system user returned a non-zero exit code.")
	}
	return capability.Result{Data: map[string]any{"username": in.Username, "deleted": true}}, nil
}
