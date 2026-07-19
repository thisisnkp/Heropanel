package capabilities

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Per-site cgroup slices.
//
// A slice is the unit of resource accounting: everything HeroPanel supervises for
// a site is placed inside `heropanel-site-<vhost>.slice`, so a runaway app is
// bounded by CPUQuota/MemoryMax/TasksMax instead of taking the node down with it.
// Placing the app in a slice is what makes "supervised in the site slice" true;
// the limits are then just properties of that slice.

// sliceParent is the top of the hierarchy. systemd creates implicit parents, so
// `heropanel-site-hps1.slice` nests under `heropanel-site.slice` under
// `heropanel.slice` with no extra unit files.
const sliceParent = "heropanel-site"

// SiteSliceName returns the slice unit name for a vhost.
//
// The `-` in a slice name is systemd's *hierarchy separator*, not a literal
// character: `a-b-c.slice` means slice `c` inside `a-b` inside `a`. A vhost may
// legally contain `-` (reVhost allows it), so an unescaped `my-site` would silently
// nest as heropanel/site/my/site — a different cgroup tree than intended, and one
// that would collide with a site actually named `my`. systemd's own escaping maps a
// literal `-` to `\x2d`; we do the same.
func SiteSliceName(vhost string) string {
	return sliceParent + "-" + escapeSliceComponent(vhost) + ".slice"
}

func siteSlicePath(vhost string) string { return unitDir + "/" + SiteSliceName(vhost) }

// escapeSliceComponent renders a vhost as a single, literal slice component.
func escapeSliceComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			// Covers '-' (hierarchy separator) and '.' — both legal in a vhost.
			b.WriteString(fmt.Sprintf(`\x%02x`, r))
		}
	}
	return b.String()
}

// ── site.apply_slice ─────────────────────────────────────────────────────────

// SiteApplySlice writes (or rewrites) a site's cgroup slice and reloads systemd.
type SiteApplySlice struct{}

func (SiteApplySlice) Name() string { return "site.apply_slice" }

type siteSliceInput struct {
	Vhost string `json:"vhost"`
	// Zero means unlimited for each of these.
	CPUQuotaPct   int   `json:"cpu_quota_pct"`
	MemLimitBytes int64 `json:"mem_limit_bytes"`
	PidsMax       int   `json:"pids_max"`
}

func (SiteApplySlice) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in siteSliceInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for site.apply_slice.")
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	if err := validateLimits(in); err != nil {
		return capability.Result{}, err
	}

	unit := renderSiteSlice(in)
	if err := c.FS.WriteFile(siteSlicePath(in.Vhost), []byte(unit), 0o644); err != nil {
		return capability.Result{}, errx.Upstream(err, "slice_write_failed", "Could not write the site slice.")
	}
	// daemon-reload re-applies the slice's cgroup attributes to the live tree, so
	// a limit change takes effect without restarting the app inside it.
	res, err := c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"daemon-reload"}, Timeout: 30 * time.Second,
	})
	if err != nil {
		return capability.Result{}, errx.Upstream(err, "systemctl_failed", "systemctl daemon-reload failed.")
	}
	if res.ExitCode != 0 {
		return capability.Result{}, errx.New(errx.KindUpstream, "systemctl_failed",
			"systemctl daemon-reload returned non-zero.")
	}
	return capability.Result{Data: map[string]any{
		"vhost": in.Vhost, "slice": SiteSliceName(in.Vhost), "applied": true,
	}}, nil
}

// validateLimits bounds the numbers before they become cgroup attributes.
func validateLimits(in siteSliceInput) error {
	// CPUQuota is a percentage of ONE core, so >100 is legitimate on a multi-core
	// box (200% = two cores). Cap it well above any sane node.
	if in.CPUQuotaPct < 0 || in.CPUQuotaPct > 64*100 {
		return errx.Validation("invalid_cpu_quota", "CPU quota must be between 0 and 6400 percent.")
	}
	// A MemoryMax below a few MB cannot start any real process; it would look like
	// a mysterious instant crash rather than a limit.
	if in.MemLimitBytes < 0 || (in.MemLimitBytes > 0 && in.MemLimitBytes < 8<<20) {
		return errx.Validation("invalid_mem_limit", "Memory limit must be 0 (unlimited) or at least 8 MiB.")
	}
	if in.PidsMax < 0 || in.PidsMax > 1<<20 {
		return errx.Validation("invalid_pids_max", "Task limit is out of range.")
	}
	return nil
}

// renderSiteSlice builds the slice unit. Only non-zero limits are emitted:
// omitting a property leaves it at systemd's default (unlimited), whereas writing
// `MemoryMax=0` would mean "no memory at all".
func renderSiteSlice(in siteSliceInput) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=HeroPanel site slice " + in.Vhost + "\n")
	b.WriteString("Before=slices.target\n\n")
	b.WriteString("[Slice]\n")
	// Accounting on regardless of limits: it is what makes per-site CPU/memory
	// readable at all, which the Monitor module will want.
	b.WriteString("CPUAccounting=true\n")
	b.WriteString("MemoryAccounting=true\n")
	b.WriteString("TasksAccounting=true\n")
	if in.CPUQuotaPct > 0 {
		b.WriteString("CPUQuota=" + strconv.Itoa(in.CPUQuotaPct) + "%\n")
	}
	if in.MemLimitBytes > 0 {
		b.WriteString("MemoryMax=" + strconv.FormatInt(in.MemLimitBytes, 10) + "\n")
	}
	if in.PidsMax > 0 {
		b.WriteString("TasksMax=" + strconv.Itoa(in.PidsMax) + "\n")
	}
	return b.String()
}

// ── site.remove_slice ────────────────────────────────────────────────────────

// SiteRemoveSlice stops and deletes a site's slice. Idempotent.
type SiteRemoveSlice struct{}

func (SiteRemoveSlice) Name() string { return "site.remove_slice" }

func (SiteRemoveSlice) Execute(c capability.Context, raw json.RawMessage) (capability.Result, error) {
	var in struct {
		Vhost string `json:"vhost"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return capability.Result{}, errx.Validation("bad_input", "Invalid input for site.remove_slice.")
	}
	if err := capability.ValidateVhostName(in.Vhost); err != nil {
		return capability.Result{}, err
	}
	// Best-effort: the slice may already be gone, or never have existed.
	_, _ = c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"stop", SiteSliceName(in.Vhost)}, Timeout: 30 * time.Second,
	})
	_ = c.FS.Remove(siteSlicePath(in.Vhost))
	_, _ = c.Runner.Run(c.Ctx, exec.Command{
		Path: systemctlPath, Args: []string{"daemon-reload"}, Timeout: 30 * time.Second,
	})
	return capability.Result{Data: map[string]any{"vhost": in.Vhost, "removed": true}}, nil
}
