package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// installPath is the absolute path to coreutils `install`, used to create
// directories with owner/group/mode atomically.
const installPath = "/usr/bin/install"

// siteDir is one directory in a site's isolated tree, with its private mode.
type siteDir struct {
	sub  string
	mode string
}

// siteTree is the standard per-site layout. tmp/sessions are 0700 so PHP session
// files and temporary data are never exposed across sites (docs/05 §3).
var siteTree = []siteDir{
	{"", "0750"},
	{"public", "0750"},
	{"logs", "0750"},
	{"tmp", "0700"},
	{"sessions", "0700"},
}

// SiteCreateDirs creates a site's isolated directory tree, owned by the site's
// dedicated Linux user.
type SiteCreateDirs struct{}

type siteCreateDirsInput struct {
	Username string `json:"username"`
	Root     string `json:"root"`
}

// Name implements capability.Capability.
func (SiteCreateDirs) Name() string { return "site.create_dirs" }

// Execute implements capability.Capability.
func (SiteCreateDirs) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in siteCreateDirsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for site.create_dirs.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.Root, c.Policy); err != nil {
		return capability.Result{}, err
	}

	created := 0
	for _, d := range siteTree {
		dir := in.Root
		if d.sub != "" {
			dir = in.Root + "/" + d.sub
		}
		// Defense in depth: every path must remain within an allowed root.
		if err := capability.ValidatePath(dir, c.Policy); err != nil {
			return capability.Result{}, err
		}
		res, err := c.Runner.Run(c.Ctx, exec.Command{
			Path: installPath,
			Args: []string{
				"-d",
				"-m", d.mode,
				"-o", in.Username,
				"-g", in.Username,
				dir,
			},
			Timeout: 20 * time.Second,
		})
		if err != nil {
			return capability.Result{}, errx.Upstream(err, "mkdir_failed", "Failed to create a site directory.")
		}
		if res.ExitCode != 0 {
			return capability.Result{}, errx.New(errx.KindUpstream, "mkdir_failed",
				"Creating a site directory returned a non-zero exit code.")
		}
		created++
	}

	return capability.Result{Data: map[string]any{
		"root":    in.Root,
		"created": created,
	}}, nil
}
