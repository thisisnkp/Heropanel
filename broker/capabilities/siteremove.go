package capabilities

import (
	"encoding/json"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// SiteRemoveDirs recursively removes a site's directory tree. The root must be
// confined to an allowed policy root, so this can never delete outside the
// sites area.
type SiteRemoveDirs struct{}

type siteRemoveDirsInput struct {
	Root string `json:"root"`
}

// Name implements capability.Capability.
func (SiteRemoveDirs) Name() string { return "site.remove_dirs" }

// Execute implements capability.Capability.
func (SiteRemoveDirs) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in siteRemoveDirsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for site.remove_dirs.")
	}
	if err := capability.ValidatePath(in.Root, c.Policy); err != nil {
		return capability.Result{}, err
	}
	if err := c.FS.RemoveAll(in.Root); err != nil {
		return capability.Result{}, errx.Upstream(err, "rmdir_failed", "Failed to remove the site directory tree.")
	}
	return capability.Result{Data: map[string]any{"root": in.Root, "removed": true}}, nil
}
