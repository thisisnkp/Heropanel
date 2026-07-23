package bootstrap

import (
	"context"
	"strings"

	"github.com/thisisnkp/heropanel/internal/auth"
	backuppkg "github.com/thisisnkp/heropanel/internal/backup"
	"github.com/thisisnkp/heropanel/internal/cron"
	"github.com/thisisnkp/heropanel/internal/database"
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

// cronSiteAdapter adapts the site repository to the scheduler's resolver.
type cronSiteAdapter struct {
	repo *repository.SiteStore
}

func (a cronSiteAdapter) Resolve(ctx context.Context, siteUID string) (*cron.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &cron.SiteRef{
		ID:        rec.ID,
		UID:       rec.UID,
		LinuxUser: rec.LinuxUser.String,
		HomeDir:   rec.HomeDir.String,
	}, nil
}

// backupSiteAdapter adapts the site repository to the backup module's resolver.
type backupSiteAdapter struct {
	repo *repository.SiteStore
}

func (a backupSiteAdapter) Resolve(ctx context.Context, siteUID string) (*backuppkg.SiteRef, error) {
	rec, err := a.repo.GetByUID(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	return &backuppkg.SiteRef{
		ID:        rec.ID,
		UID:       rec.UID,
		LinuxUser: rec.LinuxUser.String,
		HomeDir:   rec.HomeDir.String,
	}, nil
}

// backupDBAdapter adapts the database module to the backup module's DBs
// contract: resolve a UID to a name, dump a database, name a staging path.
type backupDBAdapter struct {
	svc  *database.Service
	repo *repository.DatabaseStore
}

func (a backupDBAdapter) Resolve(ctx context.Context, uid string) (string, error) {
	rec, err := a.repo.GetDatabaseByUID(ctx, uid)
	if err != nil {
		return "", err
	}
	return rec.Name, nil
}

func (a backupDBAdapter) Export(ctx context.Context, uid string) (path, name string, err error) {
	exp, err := a.svc.Export(ctx, uid)
	if err != nil {
		return "", "", err
	}
	return exp.Path, exp.Name, nil
}

func (a backupDBAdapter) ImportStagePath(gzipped bool) (path, file string) {
	return a.svc.ImportStagePath(gzipped)
}

// mailDNSAdapter adapts the DNS module to the mail module's record-wiring
// contract (fixed 1h TTL for mail records).
type mailDNSAdapter struct {
	svc *dns.Service
}

func (a mailDNSAdapter) EnsureRecord(ctx context.Context, fqdn, typ, value string, priority int, replace bool) (bool, error) {
	return a.svc.EnsureRecord(ctx, fqdn, typ, value, priority, 3600, replace)
}

// channelAuthorizer authorizes WebSocket channel subscriptions by family:
//   - "job:<uid>"   — the owner of that job, or an admin.
//   - "monitor:*"   — anyone with monitor.read (metrics are host-wide, not scoped
//     to one resource, so the permission is the gate).
//
// jobs may be nil (no async queue); job channels then simply deny, which is the
// correct answer when no job could exist. An unknown family is denied.
func channelAuthorizer(jobs *job.Dispatcher) ws.Authorizer {
	return ws.AuthorizerFunc(func(ctx context.Context, p *auth.Principal, channel string) bool {
		if p == nil {
			return false
		}
		if p.Can("*") {
			return true
		}
		if strings.HasPrefix(channel, "monitor:") {
			return p.Can("monitor.read")
		}
		if uid, ok := strings.CutPrefix(channel, "job:"); ok {
			if jobs == nil {
				return false
			}
			j, err := jobs.Get(ctx, uid)
			if err != nil {
				return false
			}
			return j.OwnerUserID.Valid && j.OwnerUserID.Int64 == p.UserID
		}
		return false // unknown channel family -> deny
	})
}
