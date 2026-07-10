package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// useraddPath is the absolute path to useradd on the target systems.
const useraddPath = "/usr/sbin/useradd"

// defaultShell denies interactive login for site users unless explicitly
// enabled elsewhere.
const defaultShell = "/usr/sbin/nologin"

// SystemUserCreate creates a dedicated, isolated Linux user + group for a site.
type SystemUserCreate struct{}

type systemUserCreateInput struct {
	Username string `json:"username"`
	Home     string `json:"home"`
	Shell    string `json:"shell,omitempty"`
}

// Name implements capability.Capability.
func (SystemUserCreate) Name() string { return "system_user.create" }

// Execute implements capability.Capability.
func (SystemUserCreate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in systemUserCreateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for system_user.create.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	// The home directory must live under a policy-allowed root so a site user
	// cannot be rooted anywhere on the filesystem.
	if err := capability.ValidatePath(in.Home, c.Policy); err != nil {
		return capability.Result{}, err
	}
	shell := in.Shell
	if shell == "" {
		shell = defaultShell
	}

	// useradd --system-ish site user: create home, set home dir + shell, and a
	// matching user-private group (-U). No shell interpolation — arg array only.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: useraddPath,
		Args: []string{
			"--create-home",
			"--home-dir", in.Home,
			"--shell", shell,
			"--user-group",
			in.Username,
		},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "useradd_failed", "Failed to create the system user.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "useradd_failed",
			"The useradd command returned a non-zero exit code.")
	}
	return capability.Result{Data: map[string]any{
		"username": in.Username,
		"home":     in.Home,
		"shell":    shell,
		"created":  true,
	}}, nil
}
