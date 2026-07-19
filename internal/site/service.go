package site

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/webserver"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Provisioning constants. Site users are placed in a dedicated uid range and
// under a single filesystem root (matching the broker's policy).
const (
	sitesRoot   = "/srv/heropanel/sites"
	uidBase     = 20000
	siteUserPfx = "hps"
	nologinPath = "/usr/sbin/nologin"
)

// Deps are the site service's dependencies. Broker, Web and PHP may be nil
// (features degrade: reads still work; creation without a broker is
// "unavailable"; a nil Web means the site is provisioned but not served; a nil
// PHP means PHP sites are provisioned without an FPM pool).
type Deps struct {
	Repo    Repo
	Broker  broker.Gateway
	Web     webserver.Applier
	PHP     php.Manager
	Runtime Runtime
	Domains Domains
}

// DomainInfo is one domain attached to a site, as the renderer needs it.
type DomainInfo struct {
	FQDN         string
	Kind         string // "primary" | "alias" | "redirect"
	ForceHTTPS   bool
	RedirectTo   string
	RedirectCode int
}

// Domains supplies a site's domains for rendering (aliases, redirects,
// force-HTTPS). Implemented by an adapter over internal/domain. Optional: when
// nil, only the site's primary domain is served.
type Domains interface {
	ForSite(ctx context.Context, siteID int64) ([]DomainInfo, error)
}

// Runtime is the app-runtime facet the site service consults: the reverse-proxy
// port for a proxy site, unit removal during de-provisioning, and start/stop for
// suspension. Implemented by internal/runtime (the site service stays off that
// concrete type).
type Runtime interface {
	ProxyPort(ctx context.Context, siteID int64) (int, bool)
	RemoveForSite(ctx context.Context, siteUID string) error
	// Control starts/stops/restarts a site's app process. Suspension uses it:
	// a 503 in the vhost is a curtain, not a stop — behind it the app would keep
	// running, keep holding memory, and keep reaching the network and its
	// database. A suspended site must not be able to do work.
	Control(ctx context.Context, siteUID, action string) error
}

// Service creates and manages sites. Privileged effects (Linux user, directory
// tree, web-server config, PHP pool) go through the broker; state lives in the
// Repo.
type Service struct {
	repo    Repo
	broker  broker.Gateway
	web     webserver.Applier
	php     php.Manager
	runtime Runtime
	domains Domains
}

// NewService constructs the site Service from its dependencies.
func NewService(d Deps) *Service {
	return &Service{repo: d.Repo, broker: d.Broker, web: d.Web, php: d.PHP,
		runtime: d.Runtime, domains: d.Domains}
}

// ValidateInput validates a create request without side effects. It is used by
// the HTTP layer to reject bad input synchronously before enqueueing a job.
func ValidateInput(in *CreateInput) error { return validateCreate(in) }

// Create provisions a site synchronously (used directly and by tests).
func (s *Service) Create(ctx context.Context, in CreateInput) (*Site, error) {
	return s.RunCreate(ctx, in, job.Noop)
}

// RunCreate provisions a new isolated site, reporting progress. It records the
// site, derives a dedicated Linux identity and paths, asks the broker to create
// the system user and directory tree, configures PHP (for PHP sites), and
// applies the web-server config. On any provisioning failure the site is marked
// "error" and the failure is returned. This is the body executed by the async
// "site.create" job handler.
func (s *Service) RunCreate(ctx context.Context, in CreateInput, p job.Progress) (*Site, error) {
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; sites cannot be provisioned.")
	}
	p.Report(5, "validating")
	if err := validateCreate(&in); err != nil {
		return nil, err
	}

	p.Report(15, "allocating site")
	rec := &Record{
		OwnerID:       in.OwnerID,
		Name:          in.Name,
		PrimaryDomain: in.PrimaryDomain,
		Type:          string(in.Type),
		DeployMode:    string(in.DeployMode),
		Status:        string(StatusProvisioning),
		Webserver:     "openlitespeed",
	}
	if err := s.repo.Insert(ctx, rec); err != nil {
		return nil, err
	}

	// Derive the site's dedicated identity and paths from its id.
	id := rec.ID
	linuxUser := siteUserPfx + strconv.FormatInt(id, 10)
	linuxUID := uidBase + int(id)
	home := sitesRoot + "/" + strconv.FormatInt(id, 10)
	docRoot := home + "/public"

	p.Report(30, "provisioning identity")
	if err := s.repo.Provision(ctx, ProvisionData{
		SiteID:        id,
		DocumentRoot:  docRoot,
		LinuxUser:     linuxUser,
		LinuxUID:      linuxUID,
		HomeDir:       home,
		Shell:         nologinPath,
		PrimaryDomain: in.PrimaryDomain,
	}); err != nil {
		_ = s.repo.UpdateStatus(ctx, id, string(StatusError))
		return nil, err
	}

	p.Report(50, "creating system user and directories")
	if err := s.provisionSystem(ctx, linuxUser, home); err != nil {
		_ = s.repo.UpdateStatus(ctx, id, string(StatusError))
		return nil, err
	}

	// Every site gets its cgroup slice up front, with accounting on and no caps.
	// The slice has to exist before anything is placed in it — the app unit names
	// it in `Slice=` — and the accounting is what lets an operator see a site's
	// real CPU/memory use before deciding what to limit.
	p.Report(60, "creating resource slice")
	if err := s.applySlice(ctx, linuxUser, Limits{}); err != nil {
		_ = s.repo.UpdateStatus(ctx, id, string(StatusError))
		return nil, err
	}

	// PHP sites get a dedicated FPM pool (default version) before serving.
	if in.Type == TypePHP && s.php != nil {
		p.Report(70, "configuring PHP")
		if _, err := s.php.EnsurePool(ctx, php.PoolRequest{
			SiteID: id, User: linuxUser, Home: home, DocumentRoot: docRoot,
		}); err != nil {
			_ = s.repo.UpdateStatus(ctx, id, string(StatusError))
			return nil, err
		}
	}

	// Configure the web server so the site actually serves.
	p.Report(90, "configuring web server")
	if err := s.applyWebserver(ctx, id); err != nil {
		_ = s.repo.UpdateStatus(ctx, id, string(StatusError))
		return nil, err
	}

	if err := s.repo.UpdateStatus(ctx, id, string(StatusActive)); err != nil {
		return nil, err
	}
	p.Report(100, "active")

	out, err := s.repo.GetByUID(ctx, rec.UID)
	if err != nil {
		return nil, err
	}
	return toView(out), nil
}

// provisionSystem performs the two privileged broker operations: create the
// dedicated Linux user, then its isolated directory tree.
func (s *Service) provisionSystem(ctx context.Context, linuxUser, home string) error {
	if _, err := s.broker.Invoke(ctx, "system_user.create", map[string]any{
		"username": linuxUser,
		"home":     home,
		"shell":    nologinPath,
	}); err != nil {
		return fmt.Errorf("create system user: %w", err)
	}
	if _, err := s.broker.Invoke(ctx, "site.create_dirs", map[string]any{
		"username": linuxUser,
		"root":     home,
	}); err != nil {
		return fmt.Errorf("create site directories: %w", err)
	}
	return nil
}

// applyWebserver renders the vhost config for all serving sites (active plus the
// one identified by includeID, which is still "provisioning") and applies it via
// the broker. A nil Applier means serving is not configured — the site is
// provisioned but no vhost is written.
func (s *Service) applyWebserver(ctx context.Context, includeID int64) error {
	if s.web == nil {
		return nil
	}
	recs, err := s.repo.List(ctx, 0, 1000, 0)
	if err != nil {
		return err
	}
	var sites []webserver.Site
	for i := range recs {
		r := recs[i]
		// Suspended sites stay in the config. They render as a 503 wall
		// (webserver.Site.Suspended) precisely so their domains remain mapped
		// here instead of falling through to whichever vhost happens to be first.
		serving := r.Status == string(StatusActive) || r.Status == string(StatusSuspended)
		if !serving && r.ID != includeID {
			continue
		}
		if !r.LinuxUser.Valid {
			continue
		}
		suspended := r.Status == string(StatusSuspended)
		isPHP := r.Type == string(TypePHP)
		fpmSocket, phpBin := "", ""
		if isPHP {
			fpmSocket = php.SocketPath(r.LinuxUser.String)
			phpBin = php.FpmBinary(s.phpVersion(ctx, r.ID))
		}
		// A proxy site forwards to its app runtime's port, once one is configured;
		// until then it renders as a plain static vhost so the config stays valid.
		proxyTarget := ""
		if r.Type == string(TypeProxy) && s.runtime != nil {
			if port, ok := s.runtime.ProxyPort(ctx, r.ID); ok {
				proxyTarget = "127.0.0.1:" + strconv.Itoa(port)
			}
		}
		domains, forceHTTPS, redirects := s.domainsFor(ctx, r.ID, r.PrimaryDomain)
		sites = append(sites, webserver.Site{
			VhostName:     r.LinuxUser.String,
			PrimaryDomain: r.PrimaryDomain,
			Domains:       domains,
			DocumentRoot:  r.DocumentRoot,
			Home:          r.HomeDir.String,
			LogDir:        r.HomeDir.String + "/logs",
			IsPHP:         isPHP,
			FpmSocket:     fpmSocket,
			PhpBin:        phpBin,
			ProxyTarget:   proxyTarget,
			ForceHTTPS:    forceHTTPS,
			Redirects:     redirects,
			Suspended:     suspended,
		})
	}
	return s.web.Apply(ctx, sites)
}

// domainsFor returns the hostnames mapped to a site's vhost, whether HTTPS is
// forced, and any redirects. Every domain (including redirect ones) is mapped to
// the vhost so the web server routes it here; a redirect domain is then answered
// by a rewrite rule. Falls back to the primary domain alone when no Domains
// source is wired.
func (s *Service) domainsFor(ctx context.Context, siteID int64, primary string) ([]string, bool, []webserver.Redirect) {
	if s.domains == nil {
		return []string{primary}, false, nil
	}
	infos, err := s.domains.ForSite(ctx, siteID)
	if err != nil || len(infos) == 0 {
		return []string{primary}, false, nil
	}
	var (
		hosts     []string
		forceHTTP bool
		redirects []webserver.Redirect
	)
	for _, d := range infos {
		hosts = append(hosts, d.FQDN)
		if d.Kind == DomainKindPrimary && d.ForceHTTPS {
			forceHTTP = true
		}
		if d.Kind == DomainKindRedirect && d.RedirectTo != "" {
			code := d.RedirectCode
			if code == 0 {
				code = 301
			}
			redirects = append(redirects, webserver.Redirect{From: d.FQDN, To: d.RedirectTo, Code: code})
		}
	}
	return hosts, forceHTTP, redirects
}

// ReapplyWebserver re-renders and applies the web-server config for all active
// sites. It is the hook the runtime service calls after a runtime change so a
// proxy site's vhost is (re)pointed at its app.
func (s *Service) ReapplyWebserver(ctx context.Context) error {
	return s.applyWebserver(ctx, 0)
}

// phpVersion returns the site's configured PHP version, or the default.
func (s *Service) phpVersion(ctx context.Context, siteID int64) string {
	if s.php != nil {
		if pool, err := s.php.GetBySiteID(ctx, siteID); err == nil {
			return pool.PHPVersion
		}
	}
	return php.DefaultVersion
}

// PHPView is the API view of a site's PHP configuration: the version selector,
// the FPM sizing, the allowlisted php.ini overrides, and OPcache — everything
// that renders into the site's one pool file.
type PHPView struct {
	Version       string            `json:"version"`
	SocketPath    string            `json:"socket_path"`
	MemoryLimitMB int               `json:"memory_limit_mb"`
	FPM           php.FPM           `json:"fpm"`
	INI           map[string]string `json:"ini"`
	OPcache       php.OPcache       `json:"opcache"`
	// AllowedINI is the set of directives that may appear in INI. The UI builds
	// its editor from this instead of carrying its own copy, which would drift
	// from the server's allowlist and offer fields that are always rejected.
	AllowedINI []string `json:"allowed_ini"`
}

// GetPHP returns the PHP pool configuration for a PHP site.
func (s *Service) GetPHP(ctx context.Context, uid string) (*PHPView, error) {
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if rec.Type != string(TypePHP) {
		return nil, errx.Validation("not_a_php_site", "This site is not a PHP site.")
	}
	if s.php == nil {
		return nil, errx.New(errx.KindUnavailable, "php_unavailable", "PHP management is not available.")
	}
	pool, err := s.php.GetBySiteID(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	return phpView(pool), nil
}

// SetPHPVersion selects a PHP version for a PHP site, rewriting its FPM pool.
func (s *Service) SetPHPVersion(ctx context.Context, uid, version string) (*PHPView, error) {
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if rec.Type != string(TypePHP) {
		return nil, errx.Validation("not_a_php_site", "This site is not a PHP site.")
	}
	if s.php == nil {
		return nil, errx.New(errx.KindUnavailable, "php_unavailable", "PHP management is not available.")
	}
	pool, err := s.php.EnsurePool(ctx, php.PoolRequest{
		SiteID: rec.ID, User: rec.LinuxUser.String, Home: rec.HomeDir.String,
		DocumentRoot: rec.DocumentRoot, Version: version,
	})
	if err != nil {
		return nil, err
	}
	return phpView(pool), nil
}

// SetPHPSettings replaces a site's whole PHP configuration.
//
// It is a full replace rather than a patch, because it maps onto a file that is
// rewritten whole: a field the caller omits means "default", not "leave as-is".
// Anything else would make the pool on disk depend on the order of past
// requests. Callers that want to change one thing read the current settings,
// change it, and send the envelope back.
func (s *Service) SetPHPSettings(ctx context.Context, uid string, settings php.Settings) (*PHPView, error) {
	rec, err := s.phpSite(ctx, uid)
	if err != nil {
		return nil, err
	}
	pool, err := s.php.ApplySettings(ctx, php.PoolRequest{
		SiteID: rec.ID, User: rec.LinuxUser.String, Home: rec.HomeDir.String,
		DocumentRoot: rec.DocumentRoot, Version: settings.Version,
	}, settings)
	if err != nil {
		return nil, err
	}
	return phpView(pool), nil
}

// phpSite loads a site and refuses anything that is not a PHP site.
func (s *Service) phpSite(ctx context.Context, uid string) (*Record, error) {
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if rec.Type != string(TypePHP) {
		return nil, errx.Validation("not_a_php_site", "This site is not a PHP site.")
	}
	if s.php == nil {
		return nil, errx.New(errx.KindUnavailable, "php_unavailable", "PHP management is not available.")
	}
	return rec, nil
}

func phpView(p *php.PoolRecord) *PHPView {
	s := php.SettingsOf(p)
	return &PHPView{
		Version:       s.Version,
		SocketPath:    p.SocketPath,
		MemoryLimitMB: s.MemoryLimitMB,
		FPM:           s.FPM,
		INI:           s.INI,
		OPcache:       s.OPcache,
		AllowedINI:    php.AllowedINIKeys(),
	}
}

// Get returns a site by UID.
func (s *Service) Get(ctx context.Context, uid string) (*Site, error) {
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	return toView(rec), nil
}

// List returns sites (all owners when ownerID is 0).
func (s *Service) List(ctx context.Context, ownerID int64, limit, offset int) ([]Site, error) {
	recs, err := s.repo.List(ctx, ownerID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]Site, len(recs))
	for i := range recs {
		out[i] = *toView(&recs[i])
	}
	return out, nil
}

// Suspend takes a site offline without destroying anything.
//
// It is the lever an operator pulls for non-payment or abuse, so it has to be
// both immediate and completely reversible: the files, the database, the Linux
// user and the vhost all survive, and Resume puts it back exactly as it was.
//
// Two things happen, and both are needed. The vhost re-renders as a 503 wall
// (its domains stay mapped here — see webserver.Site.Suspended for why removing
// it would be worse than useless), and a proxy site's app process is stopped. A
// suspended app that keeps running would still burn the CPU and memory the
// suspension was meant to reclaim, and still reach its database and the network.
func (s *Service) Suspend(ctx context.Context, uid string) (*Site, error) {
	return s.setSuspension(ctx, uid, true)
}

// Resume returns a suspended site to service.
func (s *Service) Resume(ctx context.Context, uid string) (*Site, error) {
	return s.setSuspension(ctx, uid, false)
}

func (s *Service) setSuspension(ctx context.Context, uid string, suspend bool) (*Site, error) {
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}

	want, from := StatusSuspended, StatusActive
	if !suspend {
		want, from = StatusActive, StatusSuspended
	}
	if rec.Status == string(want) {
		return toView(rec), nil // idempotent: already where the caller wants it
	}
	// Only a healthy site may be suspended, and only a suspended one resumed.
	// Suspending a half-provisioned or errored site would write a status that
	// hides why it is actually broken.
	if rec.Status != string(from) {
		return nil, errx.New(errx.KindConflict, "site_status_conflict",
			"This site is "+rec.Status+"; only an "+string(from)+" site can be "+
				map[bool]string{true: "suspended", false: "resumed"}[suspend]+".")
	}

	if err := s.repo.UpdateStatus(ctx, rec.ID, string(want)); err != nil {
		return nil, err
	}
	// The web server is told after the row, not before: applyWebserver reads the
	// status back out of the repo to decide what to render.
	if err := s.applyWebserver(ctx, 0); err != nil {
		// Put the status back. A site recorded as suspended while still serving
		// is the one outcome worse than a failed suspension, because the panel
		// would report it as handled.
		_ = s.repo.UpdateStatus(ctx, rec.ID, rec.Status)
		return nil, err
	}
	s.controlApp(ctx, rec, suspend)

	rec.Status = string(want)
	return toView(rec), nil
}

// controlApp stops or starts a proxy site's app process alongside a suspension.
//
// A failure here is deliberately not fatal. The site is already walled off at
// the web server, which is what the operator asked for; refusing the whole
// operation because a unit would not stop would leave the site serving.
func (s *Service) controlApp(ctx context.Context, rec *Record, suspend bool) {
	if s.runtime == nil || rec.Type != string(TypeProxy) {
		return
	}
	action := "start"
	if suspend {
		action = "stop"
	}
	_ = s.runtime.Control(ctx, rec.UID, action)
}

// Delete soft-deletes and de-provisions a site synchronously.
func (s *Service) Delete(ctx context.Context, uid string) error {
	return s.RunDelete(ctx, uid, job.Noop)
}

// RunDelete soft-deletes a site and de-provisions its OS resources, reporting
// progress: it removes the site from the web-server config (reload), deletes the
// dedicated Linux user, and removes the directory tree. This is the body
// executed by the async "site.delete" job handler.
func (s *Service) RunDelete(ctx context.Context, uid string, p job.Progress) error {
	p.Report(10, "loading site")
	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, uid); err != nil {
		return err
	}
	err = s.deprovision(ctx, rec, p)
	if err == nil {
		p.Report(100, "removed")
	}
	return err
}

// deprovision removes the site's runtime footprint. The DB row is already
// soft-deleted, so re-applying the web-server config drops this site's vhost.
func (s *Service) deprovision(ctx context.Context, rec *Record, p job.Progress) error {
	// Drop this site's vhost by re-rendering the remaining serving sites.
	p.Report(40, "reconfiguring web server")
	if err := s.applyWebserver(ctx, 0); err != nil {
		return err
	}
	if s.broker == nil {
		return nil
	}
	// Stop and remove the app unit (if any) before deleting its user.
	if s.runtime != nil {
		if err := s.runtime.RemoveForSite(ctx, rec.UID); err != nil {
			return fmt.Errorf("remove app runtime: %w", err)
		}
	}
	// Tear the slice down after the unit inside it is gone, so nothing is left
	// pointing at a cgroup that no longer exists.
	if rec.LinuxUser.Valid {
		p.Report(60, "removing resource slice")
		if _, err := s.broker.Invoke(ctx, "site.remove_slice", map[string]any{
			"vhost": rec.LinuxUser.String,
		}); err != nil {
			return fmt.Errorf("remove site slice: %w", err)
		}
	}
	p.Report(70, "removing system user")
	if rec.LinuxUser.Valid {
		if _, err := s.broker.Invoke(ctx, "system_user.delete", map[string]any{
			"username": rec.LinuxUser.String,
		}); err != nil {
			return fmt.Errorf("delete system user: %w", err)
		}
	}
	p.Report(90, "removing files")
	if rec.HomeDir.Valid {
		if _, err := s.broker.Invoke(ctx, "site.remove_dirs", map[string]any{
			"root": rec.HomeDir.String,
		}); err != nil {
			return fmt.Errorf("remove site directories: %w", err)
		}
	}
	return nil
}

func toView(r *Record) *Site {
	return &Site{
		UID:           r.UID,
		Name:          r.Name,
		PrimaryDomain: r.PrimaryDomain,
		Type:          Type(r.Type),
		DeployMode:    DeployMode(r.DeployMode),
		Status:        Status(r.Status),
		Webserver:     r.Webserver,
		DocumentRoot:  r.DocumentRoot,
		SystemUser:    r.LinuxUser.String,
		CreatedAt:     r.CreatedAt,
	}
}

func validateCreate(in *CreateInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.PrimaryDomain = strings.ToLower(strings.TrimSpace(in.PrimaryDomain))
	if in.Name == "" {
		return errx.Validation("invalid_name", "A site name is required.",
			errx.Field{Field: "name", Code: "required", Message: "required"})
	}
	if err := validateFQDN(in.PrimaryDomain); err != nil {
		return err
	}
	switch in.Type {
	case TypeStatic, TypePHP, TypeProxy:
	case "":
		in.Type = TypeStatic
	default:
		return errx.Validation("invalid_type", "Unsupported site type.",
			errx.Field{Field: "type", Code: "unsupported", Message: "must be static, php, or proxy"})
	}
	switch in.DeployMode {
	case DeployBaremetal, DeployGit:
	case "":
		in.DeployMode = DeployBaremetal
	case DeployDocker:
		return errx.Validation("unsupported_deploy_mode",
			"Docker deployment is not supported at this stage.")
	default:
		return errx.Validation("invalid_deploy_mode", "Unknown deploy mode.")
	}
	return nil
}

// validateFQDN is a light domain check (the broker re-validates authoritatively).
func validateFQDN(d string) error {
	invalid := errx.Validation("invalid_domain", "A valid primary domain is required.",
		errx.Field{Field: "primary_domain", Code: "invalid", Message: "invalid domain"})
	if len(d) < 3 || len(d) > 253 || !strings.Contains(d, ".") || strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return invalid
	}
	for _, r := range d {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return invalid
		}
	}
	return nil
}
