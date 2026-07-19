package capabilities

import (
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Copying one site's content into another (the Clone flow).

// chownPath is shared with dbdump.go.
const cpPath = "/bin/cp"

// SiteCopyTree copies the *contents* of one site's public directory into
// another's, then re-owns the result to the destination's Linux user.
type SiteCopyTree struct{}

type siteCopyTreeInput struct {
	SrcRoot  string `json:"src_root"` // source site home
	DstRoot  string `json:"dst_root"` // destination site home
	Username string `json:"username"` // destination site's Linux user
}

// Name implements capability.Capability.
func (SiteCopyTree) Name() string { return "site.copy_tree" }

// Execute implements capability.Capability.
func (SiteCopyTree) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in siteCopyTreeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for site.copy_tree.")
	}
	if err := capability.ValidateUsername(in.Username); err != nil {
		return capability.Result{}, err
	}
	// Both ends are confined. Without this the capability would copy any file on
	// the box into a place the caller controls and can then read over HTTP.
	if err := capability.ValidatePath(in.SrcRoot, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(in.DstRoot, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if in.SrcRoot == in.DstRoot {
		return capability.Result{}, errx.Validation("same_site", "A site cannot be cloned onto itself.")
	}

	src := in.SrcRoot + "/public/."
	dst := in.DstRoot + "/public"
	if err := capability.ValidatePath(in.SrcRoot+"/public", c.Policy); err != nil {
		return capability.Result{}, err
	}
	if err := capability.ValidatePath(dst, c.Policy); err != nil {
		return capability.Result{}, err
	}

	// `-a` preserves modes and, crucially, does not follow symlinks: a symlink in
	// the source pointing at /etc/shadow is copied as a symlink, not as the
	// secret it points to. The trailing "/." copies the directory's contents
	// rather than nesting it as public/public.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path:    cpPath,
		Args:    []string{"-a", "--", src, dst},
		Timeout: 10 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "copy_failed", "Failed to copy the site content.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "copy_failed",
			"Copying the site content returned a non-zero exit code.")
	}

	// Re-own everything to the destination's user. Skipped, this would hand the
	// clone a tree owned by the *source's* user — two sites able to read and
	// write each other's files, which is the exact isolation the per-site user
	// exists to provide. -h re-owns symlinks themselves rather than their targets.
	res, err = c.Runner.Run(c.Ctx, exec.Command{
		Path:    chownPath,
		Args:    []string{"-R", "-h", "--", in.Username + ":" + in.Username, dst},
		Timeout: 5 * time.Minute,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "chown_failed", "Failed to re-own the cloned content.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "chown_failed",
			"Re-owning the cloned content returned a non-zero exit code.")
	}

	return capability.Result{Data: map[string]any{
		"src":    in.SrcRoot,
		"dst":    dst,
		"copied": true,
	}}, nil
}
