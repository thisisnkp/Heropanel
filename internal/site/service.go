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
	Repo   Repo
	Broker broker.Gateway
	Web    webserver.Applier
	PHP    php.Manager
}

// Service creates and manages sites. Privileged effects (Linux user, directory
// tree, web-server config, PHP pool) go through the broker; state lives in the
// Repo.
type Service struct {
	repo   Repo
	broker broker.Gateway
	web    webserver.Applier
	php    php.Manager
}

// NewService constructs the site Service from its dependencies.
func NewService(d Deps) *Service {
	return &Service{repo: d.Repo, broker: d.Broker, web: d.Web, php: d.PHP}
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
		if r.Status != string(StatusActive) && r.ID != includeID {
			continue
		}
		if !r.LinuxUser.Valid {
			continue
		}
		isPHP := r.Type == string(TypePHP)
		fpmSocket, phpBin := "", ""
		if isPHP {
			fpmSocket = php.SocketPath(r.LinuxUser.String)
			phpBin = php.FpmBinary(s.phpVersion(ctx, r.ID))
		}
		sites = append(sites, webserver.Site{
			VhostName:     r.LinuxUser.String,
			PrimaryDomain: r.PrimaryDomain,
			Domains:       []string{r.PrimaryDomain},
			DocumentRoot:  r.DocumentRoot,
			Home:          r.HomeDir.String,
			LogDir:        r.HomeDir.String + "/logs",
			IsPHP:         isPHP,
			FpmSocket:     fpmSocket,
			PhpBin:        phpBin,
		})
	}
	return s.web.Apply(ctx, sites)
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

// PHPView is the API view of a site's PHP configuration.
type PHPView struct {
	Version       string `json:"version"`
	SocketPath    string `json:"socket_path"`
	PM            string `json:"pm"`
	MaxChildren   int    `json:"pm_max_children"`
	MemoryLimitMB int    `json:"memory_limit_mb"`
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

func phpView(p *php.PoolRecord) *PHPView {
	return &PHPView{
		Version:       p.PHPVersion,
		SocketPath:    p.SocketPath,
		PM:            p.PM,
		MaxChildren:   p.MaxChildren,
		MemoryLimitMB: p.MemoryLimitMB,
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
	case TypeStatic, TypePHP:
	case "":
		in.Type = TypeStatic
	default:
		return errx.Validation("invalid_type", "Unsupported site type.",
			errx.Field{Field: "type", Code: "unsupported", Message: "must be static or php"})
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
