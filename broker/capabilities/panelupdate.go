package capabilities

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Self-update is the one operation where the broker cannot simply do the work.
//
// Replacing the panel means replacing np-broker — this process — and npd, which
// is what carries the request asking for it. Anything that swapped the binaries
// inline would be killing itself halfway through: the restart would tear down
// the connection before the reply, leaving npd unable to tell whether the update
// had happened, and a failed swap with no live broker to undo it.
//
// So the broker does not swap anything. It starts np-installer as a **transient
// systemd unit**, which systemd owns and supervises. That unit survives both
// services being restarted underneath it, because it is not a child of either —
// the only arrangement in which the thing performing the swap is not also the
// thing being swapped.
//
// The capability is correspondingly narrow. It takes no command, no unit body
// and no destination: only a staged directory, which it re-validates against a
// fixed shape. Everything about *what* to install is decided by the signed
// release manifest that npd already verified and that np-installer verifies
// again — never by an argument to this call.

const (
	systemdRunPath   = "/usr/bin/systemd-run"
	installerPath    = "/opt/nexpanel/bin/np-installer"
	updateStageRoot  = "/var/lib/nexpanel/updates"
	selfUpdateUnit   = "np-selfupdate"
	updateStartLimit = 20 * time.Second
)

// PanelUpdate starts a self-update by launching np-installer detached.
type PanelUpdate struct{}

type panelUpdateInput struct {
	Source  string `json:"source"`
	Version string `json:"version"`
	UID     string `json:"uid"`
}

// Name implements capability.Capability.
func (PanelUpdate) Name() string { return "panel.update" }

// Execute implements capability.Capability.
func (PanelUpdate) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in panelUpdateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for panel.update.")
	}
	if err := validUpdateVersion(in.Version); err != nil {
		return capability.Result{}, err
	}
	// The source is not trusted as a path. It must be exactly the staging
	// directory for the named version — not merely "inside" it, which a suffix
	// check would allow a symlinked sibling to satisfy.
	want := path.Join(updateStageRoot, in.Version)
	if path.Clean(in.Source) != want {
		return capability.Result{}, errx.Validation("bad_source",
			"The update source must be the staged release directory for that version.")
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
		return capability.Result{}, errx.Validation("source_missing",
			"No staged release was found for that version.")
	}
	// Refuse early rather than leave a transient unit failing in a loop: without
	// the installer there is nothing that can perform the swap.
	if fi, err := os.Stat(installerPath); err != nil || fi.IsDir() {
		return capability.Result{}, errx.New(errx.KindUnavailable, "installer_missing",
			"np-installer is not installed, so the panel cannot update itself.")
	}

	// --collect makes systemd discard the unit once it finishes, so a second
	// update is not blocked by the remains of the first.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemdRunPath,
		Args: []string{
			"--unit=" + selfUpdateUnit,
			"--collect",
			"--service-type=oneshot",
			"--property=TimeoutStartSec=600",
			installerPath,
			"--update",
			"--source", want,
			"--update-uid", in.UID,
		},
		Timeout: updateStartLimit,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "update_start_failed",
			"Could not start the updater.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "update_start_failed",
			"The updater could not be started.")
	}
	return capability.Result{Data: map[string]any{
		"started": true,
		"unit":    selfUpdateUnit,
		"version": in.Version,
	}}, nil
}

// validUpdateVersion keeps the version safe to use as a path segment. It is the
// same rule npd applies before staging, restated here because the broker must
// never rely on its caller having validated anything.
func validUpdateVersion(v string) error {
	if v == "" || len(v) > 64 {
		return errx.Validation("bad_version", "A release version is required.")
	}
	if strings.Contains(v, "..") || strings.ContainsAny(v, "/\\") {
		return errx.Validation("bad_version", "That release version is not a valid path segment.")
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '.' || r == '-' || r == '+' || r == '_':
		default:
			return errx.Validation("bad_version", "That release version contains characters that are not allowed.")
		}
	}
	return nil
}
