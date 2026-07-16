package runtime

import (
	"context"
	"encoding/json"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Service manages app runtimes: it persists the config, drives the broker to
// write/control the per-site systemd unit, and re-renders the reverse-proxy
// vhost via a supplied hook.
type Service struct {
	repo    Repo
	sites   Sites
	broker  broker.Gateway
	reproxy func(context.Context) error // re-render the webserver (set at wiring)
}

// NewService constructs the runtime Service. broker may be nil (control ops then
// report "unavailable"; reads still work).
func NewService(repo Repo, sites Sites, gw broker.Gateway) *Service {
	return &Service{repo: repo, sites: sites, broker: gw}
}

// WithReproxy sets the hook that re-renders the web server after a runtime
// change (so a proxy site's vhost is (re)pointed at the app). Returns s for
// chaining. The hook is the site service's ReapplyWebserver.
func (s *Service) WithReproxy(fn func(context.Context) error) *Service {
	s.reproxy = fn
	return s
}

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable", "The broker is not available.")
	}
	return nil
}

// SetRuntime configures (or replaces) a site's runtime: it upserts the config,
// asks the broker to write + (re)start the systemd unit, and re-renders the
// vhost as a reverse proxy.
func (s *Service) SetRuntime(ctx context.Context, siteUID string, in SetInput) (*Runtime, error) {
	if err := validateSet(&in); err != nil {
		return nil, err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}

	envJSON, _ := json.Marshal(in.Env)
	rec := &Record{
		SiteID: ref.ID, Runtime: in.Runtime, Command: in.Command,
		Port: in.Port, Env: string(envJSON), Status: StatusRunning,
	}
	if err := s.repo.Upsert(ctx, rec); err != nil {
		return nil, err
	}

	if _, err := s.broker.Invoke(ctx, "app.unit_apply", map[string]any{
		"vhost":    ref.LinuxUser,
		"username": ref.LinuxUser,
		"home":     ref.HomeDir,
		"command":  in.Command,
		"port":     in.Port,
		"env":      in.Env,
		"runtime":  in.Runtime,
	}); err != nil {
		_ = s.repo.SetStatus(ctx, ref.ID, StatusError)
		return nil, err
	}

	// Re-render the web server so the vhost proxies to the app.
	if s.reproxy != nil {
		if err := s.reproxy(ctx); err != nil {
			return nil, err
		}
	}
	return toView(rec), nil
}

// GetRuntime returns a site's runtime config.
func (s *Service) GetRuntime(ctx context.Context, siteUID string) (*Runtime, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rec, err := s.repo.GetBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	return toView(rec), nil
}

// Control performs a start/stop/restart on the site's unit and records the
// resulting status. action is "start", "stop", or "restart".
func (s *Service) Control(ctx context.Context, siteUID, action string) (*Runtime, error) {
	switch action {
	case "start", "stop", "restart":
	default:
		return nil, errx.Validation("invalid_action", "Action must be start, stop, or restart.")
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rec, err := s.repo.GetBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	if _, err := s.broker.Invoke(ctx, "app.unit_control", map[string]any{
		"vhost":  ref.LinuxUser,
		"action": action,
	}); err != nil {
		_ = s.repo.SetStatus(ctx, ref.ID, StatusError)
		return nil, err
	}
	status := StatusRunning
	if action == "stop" {
		status = StatusStopped
	}
	_ = s.repo.SetStatus(ctx, ref.ID, status)
	rec.Status = status
	return toView(rec), nil
}

// RestartForSite restarts a site's app unit if one is configured (used by the
// git service after a deploy so a proxy site serves the new release). A site
// without a runtime is a no-op, not an error.
func (s *Service) RestartForSite(ctx context.Context, siteUID string) error {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetBySiteID(ctx, ref.ID); err != nil {
		return nil // no runtime configured; nothing to restart
	}
	if s.broker == nil {
		return nil
	}
	if _, err := s.broker.Invoke(ctx, "app.unit_control", map[string]any{
		"vhost": ref.LinuxUser, "action": "restart",
	}); err != nil {
		_ = s.repo.SetStatus(ctx, ref.ID, StatusError)
		return err
	}
	_ = s.repo.SetStatus(ctx, ref.ID, StatusRunning)
	return nil
}

// ProxyPort returns the reverse-proxy port for a site if a runtime is
// configured. It implements the site package's proxy lookup.
func (s *Service) ProxyPort(ctx context.Context, siteID int64) (int, bool) {
	rec, err := s.repo.GetBySiteID(ctx, siteID)
	if err != nil || rec.Port <= 0 {
		return 0, false
	}
	return rec.Port, true
}

// RemoveForSite stops and removes a site's unit (used during de-provisioning). A
// missing runtime is not an error.
func (s *Service) RemoveForSite(ctx context.Context, siteUID string) error {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetBySiteID(ctx, ref.ID); err != nil {
		return nil // no runtime configured; nothing to remove
	}
	if s.broker == nil {
		return nil
	}
	_, err = s.broker.Invoke(ctx, "app.unit_remove", map[string]any{"vhost": ref.LinuxUser})
	return err
}

func toView(r *Record) *Runtime {
	env := map[string]string{}
	if r.Env != "" {
		_ = json.Unmarshal([]byte(r.Env), &env)
	}
	return &Runtime{
		UID: r.UID, Runtime: r.Runtime, Command: r.Command, Port: r.Port,
		Env: env, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}
