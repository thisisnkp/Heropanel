package site

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Per-site resource limits.
//
// Every site gets a systemd **slice** (`heropanel-site-<user>.slice`) at
// provisioning time, and everything HeroPanel supervises for that site runs
// inside it. The slice is what makes a limit real: without it a runaway app is
// bounded only by the size of the node, and one tenant can take the box down for
// everyone.
//
// A site with no limits set still gets a slice — with accounting on and no caps.
// That is deliberate: the cgroup has to exist before it can be constrained, and
// the accounting is what lets an operator see who is actually using the node
// before deciding what to cap.

// Limits is a site's resource envelope. Zero means unlimited for each field,
// which is also the default: a site nobody has limited must behave exactly as it
// did before limits existed.
type Limits struct {
	// CPUQuotaPct is a percentage of ONE core. 100 = one full core, 200 = two.
	CPUQuotaPct int `json:"cpu_quota_pct" db:"cpu_quota_pct"`
	// MemLimitBytes is a hard ceiling — the kernel OOM-kills above it, it does
	// not throttle.
	MemLimitBytes int64 `json:"mem_limit_bytes" db:"mem_limit_bytes"`
	// PidsMax caps tasks: the fork-bomb ceiling.
	PidsMax int `json:"pids_max" db:"pids_max"`
}

// minMemLimitBytes mirrors the broker's floor. A ceiling below a few MiB cannot
// start any real process, so it would present as a mysterious instant crash
// rather than as a limit.
const minMemLimitBytes = 8 << 20

// maxCPUQuotaPct allows for a large multi-core node (6400% = 64 cores) while
// still rejecting a typo'd 640000.
const maxCPUQuotaPct = 64 * 100

// maxPidsMax is far above any sane per-site process count.
const maxPidsMax = 1 << 20

func validateLimits(l Limits) error {
	if l.CPUQuotaPct < 0 || l.CPUQuotaPct > maxCPUQuotaPct {
		return errx.Validation("invalid_cpu_quota",
			"CPU quota must be between 0 (unlimited) and 6400 percent of one core.",
			errx.Field{Field: "cpu_quota_pct", Code: "invalid", Message: "out of range"})
	}
	if l.MemLimitBytes < 0 || (l.MemLimitBytes > 0 && l.MemLimitBytes < minMemLimitBytes) {
		return errx.Validation("invalid_mem_limit",
			"Memory limit must be 0 (unlimited) or at least 8 MiB.",
			errx.Field{Field: "mem_limit_bytes", Code: "invalid", Message: "too small"})
	}
	if l.PidsMax < 0 || l.PidsMax > maxPidsMax {
		return errx.Validation("invalid_pids_max", "Task limit is out of range.",
			errx.Field{Field: "pids_max", Code: "invalid", Message: "out of range"})
	}
	return nil
}

// GetLimits returns a site's resource limits.
func (s *Service) GetLimits(ctx context.Context, uid string) (*Limits, error) {
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	return s.repo.GetLimits(ctx, rec.ID)
}

// SetLimits records a site's resource limits and applies them to its slice.
//
// The slice is written before the row is updated only in the sense that both
// must agree: the broker call comes first, and the row is written only once
// systemd has accepted the limits. A stored limit the kernel is not enforcing is
// worse than no limit, because the panel would report a cap that does not exist.
func (s *Service) SetLimits(ctx context.Context, uid string, l Limits) (*Limits, error) {
	if err := validateLimits(l); err != nil {
		return nil, err
	}
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !rec.LinuxUser.Valid || rec.LinuxUser.String == "" {
		return nil, errx.Validation("site_not_provisioned",
			"This site has no system identity yet; limits cannot be applied.")
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; limits cannot be applied.")
	}
	if err := s.applySlice(ctx, rec.LinuxUser.String, l); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertLimits(ctx, rec.ID, l); err != nil {
		return nil, err
	}
	return &l, nil
}

// applySlice writes the site's cgroup slice with the given limits.
func (s *Service) applySlice(ctx context.Context, linuxUser string, l Limits) error {
	_, err := s.broker.Invoke(ctx, "site.apply_slice", map[string]any{
		"vhost":           linuxUser,
		"cpu_quota_pct":   l.CPUQuotaPct,
		"mem_limit_bytes": l.MemLimitBytes,
		"pids_max":        l.PidsMax,
	})
	return err
}
