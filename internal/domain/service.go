package domain

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Service manages a site's domains. Every mutation re-renders the site's
// web-server config through the supplied hook, so a new alias/redirect/force-
// HTTPS setting takes effect immediately.
type Service struct {
	repo    Repo
	sites   Sites
	reapply func(context.Context) error
}

// NewService constructs the domain Service.
func NewService(repo Repo, sites Sites) *Service { return &Service{repo: repo, sites: sites} }

// WithReapply sets the hook that re-renders the web server after a change (the
// site service's ReapplyWebserver). Returns s for chaining.
func (s *Service) WithReapply(fn func(context.Context) error) *Service {
	s.reapply = fn
	return s
}

// List returns a site's domains.
func (s *Service) List(ctx context.Context, siteUID string) ([]Domain, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, len(rows))
	for i := range rows {
		out[i] = *toView(&rows[i])
	}
	return out, nil
}

// ListForSiteID returns a site's domains by internal id (used by the site
// service's render adapter).
func (s *Service) ListForSiteID(ctx context.Context, siteID int64) ([]Domain, error) {
	rows, err := s.repo.ListBySiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, len(rows))
	for i := range rows {
		out[i] = *toView(&rows[i])
	}
	return out, nil
}

// Add attaches an alias or redirect domain to a site and re-renders the vhost.
func (s *Service) Add(ctx context.Context, siteUID string, in AddInput) (*Domain, error) {
	if err := validateAdd(&in); err != nil {
		return nil, err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	row := &Row{
		SiteID: ref.ID, FQDN: in.FQDN, Kind: in.Kind,
		RedirectTo: in.RedirectTo, RedirectCode: in.RedirectCode,
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		return nil, err
	}
	if err := s.doReapply(ctx); err != nil {
		// The vhost could not be applied — drop the row again so state matches.
		_ = s.repo.Delete(ctx, row.UID)
		return nil, err
	}
	return toView(row), nil
}

// Delete removes a domain (never the primary) and re-renders the vhost.
func (s *Service) Delete(ctx context.Context, domainUID string) error {
	row, err := s.repo.GetByUID(ctx, domainUID)
	if err != nil {
		return err
	}
	if row.Kind == KindPrimary {
		return errx.Validation("cannot_delete_primary",
			"The primary domain cannot be removed; delete the site instead.")
	}
	if err := s.repo.Delete(ctx, domainUID); err != nil {
		return err
	}
	return s.doReapply(ctx)
}

// SetForceHTTPS turns force-HTTPS on/off for all of a site's domains and
// re-renders the vhost.
func (s *Service) SetForceHTTPS(ctx context.Context, siteUID string, on bool) error {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return err
	}
	if err := s.repo.SetForceHTTPSForSite(ctx, ref.ID, on); err != nil {
		return err
	}
	return s.doReapply(ctx)
}

func (s *Service) doReapply(ctx context.Context) error {
	if s.reapply == nil {
		return nil
	}
	return s.reapply(ctx)
}

func toView(r *Row) *Domain {
	return &Domain{
		UID: r.UID, FQDN: r.FQDN, Kind: r.Kind, ForceHTTPS: r.ForceHTTPS,
		RedirectTo: r.RedirectTo, RedirectCode: r.RedirectCode, CreatedAt: r.CreatedAt,
	}
}
