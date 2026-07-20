package bootstrap

import (
	"context"
	"strings"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/internal/files"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/httpapi"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/terminal"
	"github.com/thisisnkp/heropanel/internal/ws"
)

// userDirectoryAdapter adapts the user repository to httpapi.UserDirectory,
// mapping persistence rows to the API view. Living in the composition root keeps
// httpapi decoupled from the repository package.
type userDirectoryAdapter struct {
	repo *repository.UserRepository
}

func (a *userDirectoryAdapter) List(ctx context.Context, limit, offset int) ([]httpapi.UserSummary, error) {
	rows, err := a.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.UserSummary, len(rows))
	for i, u := range rows {
		out[i] = httpapi.UserSummary{
			UID:         u.UID,
			Email:       u.Email,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Status:      u.Status,
		}
	}
	return out, nil
}

// gitSiteAdapter adapts the site repository to git.Sites, resolving the identity
// and paths a deploy needs (Linux user, home) by site UID. Keeps the git service
// off the concrete site store.
type gitSiteAdapter struct {
	repo site.Repo
}

func (a gitSiteAdapter) Resolve(ctx context.Context, siteUID string) (*git.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &git.SiteRef{
		ID:         rec.ID,
		UID:        rec.UID,
		LinuxUser:  rec.LinuxUser.String,
		HomeDir:    rec.HomeDir.String,
		DeployMode: rec.DeployMode,
	}, nil
}

// filesSiteAdapter adapts the site repository to files.Sites, resolving the
// identity and paths a file operation needs (Linux user, home, deploy mode) by
// site UID. The deploy mode is what the File Manager gates its baremetal-only
// rule on, so it is carried through here.
type filesSiteAdapter struct {
	repo site.Repo
}

func (a filesSiteAdapter) Resolve(ctx context.Context, siteUID string) (*files.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &files.SiteRef{
		ID:         rec.ID,
		UID:        rec.UID,
		LinuxUser:  rec.LinuxUser.String,
		HomeDir:    rec.HomeDir.String,
		DeployMode: rec.DeployMode,
	}, nil
}

// terminalSiteAdapter adapts the site repository to terminal.Sites, resolving
// the Linux user a shell will run as and the home it starts in.
type terminalSiteAdapter struct {
	repo site.Repo
}

func (a terminalSiteAdapter) Resolve(ctx context.Context, siteUID string) (*terminal.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &terminal.SiteRef{
		ID:         rec.ID,
		UID:        rec.UID,
		LinuxUser:  rec.LinuxUser.String,
		HomeDir:    rec.HomeDir.String,
		DeployMode: rec.DeployMode,
	}, nil
}

// domainSiteAdapter adapts the site repository to domain.Sites.
type domainSiteAdapter struct {
	repo site.Repo
}

func (a domainSiteAdapter) Resolve(ctx context.Context, siteUID string) (*domain.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &domain.SiteRef{ID: rec.ID, UID: rec.UID}, nil
}

// siteDomainsAdapter adapts the domain service to site.Domains, so the site
// renderer can map every alias/redirect onto the vhost without depending on the
// domain package's types.
type siteDomainsAdapter struct {
	svc *domain.Service
}

func (a siteDomainsAdapter) ForSite(ctx context.Context, siteID int64) ([]site.DomainInfo, error) {
	ds, err := a.svc.ListForSiteID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	out := make([]site.DomainInfo, len(ds))
	for i, d := range ds {
		out[i] = site.DomainInfo{
			FQDN: d.FQDN, Kind: d.Kind, ForceHTTPS: d.ForceHTTPS,
			RedirectTo: d.RedirectTo, RedirectCode: d.RedirectCode,
		}
	}
	return out, nil
}

// sslDNSAdapter adapts the DNS service to ssl.DNSProvider, letting ACME publish
// DNS-01 challenges into a zone HeroPanel is authoritative for (which is what
// makes wildcard certificates possible).
type sslDNSAdapter struct {
	svc *dns.Service
}

func (a sslDNSAdapter) SetTXT(ctx context.Context, fqdn, value string) error {
	return a.svc.SetChallengeTXT(ctx, fqdn, value)
}

func (a sslDNSAdapter) DeleteTXT(ctx context.Context, fqdn string) error {
	return a.svc.DeleteChallengeTXT(ctx, fqdn)
}

// runtimeSiteAdapter adapts the site repository to runtime.Sites.
type runtimeSiteAdapter struct {
	repo site.Repo
}

func (a runtimeSiteAdapter) Resolve(ctx context.Context, siteUID string) (*runtime.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &runtime.SiteRef{
		ID:        rec.ID,
		UID:       rec.UID,
		LinuxUser: rec.LinuxUser.String,
		HomeDir:   rec.HomeDir.String,
	}, nil
}

// siteRuntimeAdapter adapts the runtime service to site.Runtime.
//
// The site service is only ever asking "is it running or not"; the runtime
// record Control returns is of no use to it, and taking it would drag
// runtime's concrete types across the boundary.
type siteRuntimeAdapter struct {
	svc *runtime.Service
}

func (a siteRuntimeAdapter) ProxyPort(ctx context.Context, siteID int64) (int, bool) {
	return a.svc.ProxyPort(ctx, siteID)
}

func (a siteRuntimeAdapter) RemoveForSite(ctx context.Context, siteUID string) error {
	return a.svc.RemoveForSite(ctx, siteUID)
}

func (a siteRuntimeAdapter) Control(ctx context.Context, siteUID, action string) error {
	_, err := a.svc.Control(ctx, siteUID, action)
	return err
}

// jobChannelAuthorizer authorizes WebSocket channel subscriptions. A principal
// may subscribe to "job:<uid>" only if they own that job (or are an admin).
func jobChannelAuthorizer(jobs *job.Dispatcher) ws.Authorizer {
	return ws.AuthorizerFunc(func(ctx context.Context, p *auth.Principal, channel string) bool {
		if p == nil {
			return false
		}
		if p.Can("*") {
			return true
		}
		if uid, ok := strings.CutPrefix(channel, "job:"); ok {
			j, err := jobs.Get(ctx, uid)
			if err != nil {
				return false
			}
			return j.OwnerUserID.Valid && j.OwnerUserID.Int64 == p.UserID
		}
		return false // unknown channel family -> deny
	})
}
