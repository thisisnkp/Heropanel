package git

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// sqlTime is the timestamp format accepted by both SQLite (TEXT) and MariaDB
// (DATETIME) columns, so the service can write times without dialect branching.
const sqlTime = "2006-01-02 15:04:05"

// Service orchestrates Git sources and deployments. Privileged work (clone,
// build, atomic swap) runs as the site's unprivileged user via the broker; state
// lives in the Repo.
type Service struct {
	repo      Repo
	sites     Sites
	broker    broker.Gateway
	restarter Restarter
}

// Restarter restarts a site's app runtime after a successful deploy so a new
// release is actually served (proxy sites). It is a no-op for sites without a
// runtime. Optional — nil when the runtime module is not wired.
type Restarter interface {
	RestartForSite(ctx context.Context, siteUID string) error
}

// NewService constructs the git Service. broker may be nil (deploys then report
// "unavailable"; reads still work).
func NewService(repo Repo, sites Sites, gw broker.Gateway) *Service {
	return &Service{repo: repo, sites: sites, broker: gw}
}

// WithRestarter wires the app-runtime restarter used to reload a proxy site's
// process after a deploy. Returns s for chaining.
func (s *Service) WithRestarter(r Restarter) *Service {
	s.restarter = r
	return s
}

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; deployments cannot run.")
	}
	return nil
}

// SetSource configures (or replaces) a site's Git source. The webhook secret is
// generated once and preserved across updates so an already-configured webhook
// keeps working. The returned Source includes the full webhook URL (with secret)
// — the only time the secret is exposed.
func (s *Service) SetSource(ctx context.Context, siteUID string, in SetSourceInput) (*Source, error) {
	if err := validateSetSource(&in); err != nil {
		return nil, err
	}
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	if ref.DeployMode != "git" {
		return nil, errx.Validation("not_a_git_site",
			"This site is not a Git-deploy site. Create it with deploy mode \"git\".")
	}

	secret := ""
	if existing, err := s.repo.GetSourceBySiteID(ctx, ref.ID); err == nil {
		secret = existing.WebhookSecret
	}
	if secret == "" {
		secret, err = randomSecret()
		if err != nil {
			return nil, err
		}
	}

	rec := &SourceRecord{
		SiteID:        ref.ID,
		RepoURL:       in.RepoURL,
		Branch:        in.Branch,
		BuildCommand:  in.BuildCommand,
		WebRoot:       in.WebRoot,
		WebhookSecret: secret,
	}
	if err := s.repo.UpsertSource(ctx, rec); err != nil {
		return nil, err
	}
	v := toSourceView(rec)
	v.WebhookURL = webhookURL(siteUID, secret)
	return v, nil
}

// GetSource returns a site's Git source (without the webhook secret).
func (s *Service) GetSource(ctx context.Context, siteUID string) (*Source, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	rec, err := s.repo.GetSourceBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	return toSourceView(rec), nil
}

// ListDeployments returns a site's deploy history, newest first.
func (s *Service) ListDeployments(ctx context.Context, siteUID string, limit int) ([]Deployment, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	recs, err := s.repo.ListDeployments(ctx, ref.ID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Deployment, len(recs))
	for i := range recs {
		out[i] = *toDeploymentView(&recs[i])
	}
	return out, nil
}

// VerifyWebhook resolves a site by UID and constant-time-compares the presented
// secret against the source's webhook secret. It returns the site on success so
// the caller can enqueue a deploy.
func (s *Service) VerifyWebhook(ctx context.Context, siteUID, secret string) (*SiteRef, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	src, err := s.repo.GetSourceBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(src.WebhookSecret)) != 1 {
		return nil, errx.Forbidden("invalid_webhook_secret", "Invalid webhook secret.")
	}
	return ref, nil
}

// RunDeploy performs a deployment for a site, reporting progress. It records a
// deployment row, asks the broker to clone+build as the site user and atomically
// swap the live release, then finalizes the row. This is the body run by the
// async "git.deploy" job handler. Trigger is one of the Trigger* constants.
func (s *Service) RunDeploy(ctx context.Context, siteUID, trigger string, p job.Progress) (*Deployment, error) {
	if trigger == "" {
		trigger = TriggerManual
	}
	p.Report(5, "resolving site")
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	if ref.DeployMode != "git" {
		return nil, errx.Validation("not_a_git_site", "This site is not a Git-deploy site.")
	}
	src, err := s.repo.GetSourceBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}

	// Record the deployment as running before doing privileged work, so a crash
	// mid-deploy leaves an observable "running" row rather than a silent gap.
	p.Report(15, "recording deployment")
	releaseID := idgen.NewULID()
	releaseDir := ref.HomeDir + "/releases/" + releaseID
	dep := &DeploymentRecord{
		SiteID: ref.ID, SourceID: src.ID, Status: StatusRunning,
		TriggerKind: trigger, ReleaseDir: releaseDir,
	}
	if err := s.repo.InsertDeployment(ctx, dep); err != nil {
		return nil, err
	}

	p.Report(30, "cloning and building")
	res, derr := s.broker.Invoke(ctx, "git.deploy", map[string]any{
		"username":      ref.LinuxUser,
		"home":          ref.HomeDir,
		"repo_url":      src.RepoURL,
		"branch":        src.Branch,
		"build_command": src.BuildCommand,
		"web_root":      src.WebRoot,
		"release_id":    releaseID,
		"keep":          keepReleases,
	})
	if derr != nil {
		dep.Status = StatusFailed
		dep.Log = derr.Error()
		s.finalize(ctx, dep)
		return nil, derr
	}

	p.Report(90, "activating release")
	dep.Status = StatusSuccess
	dep.CommitSHA, _ = res["commit"].(string)
	if logTail, ok := res["log"].(string); ok {
		dep.Log = logTail
	}

	// Reload the app so a proxy site serves the new release. Best-effort: the
	// release is already live, so a restart failure is recorded but does not fail
	// the deploy (a no-op for sites without a runtime).
	if s.restarter != nil {
		p.Report(95, "restarting app")
		if rerr := s.restarter.RestartForSite(ctx, siteUID); rerr != nil {
			dep.Log = strings.TrimSpace(dep.Log + "\n[warn] app restart failed: " + rerr.Error())
		} else {
			dep.Log = strings.TrimSpace(dep.Log + "\n[app restarted]")
		}
	}
	s.finalize(ctx, dep)

	p.Report(100, "deployed")
	return toDeploymentView(dep), nil
}

// RunRollback repoints the live release at a previous successful deployment's
// release directory (no rebuild), recording a new rollback deployment row.
func (s *Service) RunRollback(ctx context.Context, siteUID, deploymentUID string, p job.Progress) (*Deployment, error) {
	p.Report(10, "resolving site")
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	target, err := s.repo.GetDeploymentByUID(ctx, deploymentUID)
	if err != nil {
		return nil, err
	}
	if target.SiteID != ref.ID {
		return nil, errx.NotFound("deployment_not_found", "No such deployment for this site.")
	}
	if target.Status != StatusSuccess || target.ReleaseDir == "" {
		return nil, errx.Validation("not_rollbackable", "Only a successful deployment can be rolled back to.")
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}

	p.Report(50, "activating previous release")
	if _, err := s.broker.Invoke(ctx, "git.rollback", map[string]any{
		"username":    ref.LinuxUser,
		"home":        ref.HomeDir,
		"release_dir": target.ReleaseDir,
	}); err != nil {
		return nil, err
	}

	src, err := s.repo.GetSourceBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	dep := &DeploymentRecord{
		SiteID: ref.ID, SourceID: src.ID, Status: StatusSuccess,
		TriggerKind: TriggerRollback, ReleaseDir: target.ReleaseDir,
		CommitSHA: target.CommitSHA, Log: "rolled back to deployment " + deploymentUID,
	}
	if err := s.repo.InsertDeployment(ctx, dep); err != nil {
		return nil, err
	}
	s.finalize(ctx, dep)
	p.Report(100, "rolled back")
	return toDeploymentView(dep), nil
}

// finalize stamps the finish time and persists a deployment's terminal state.
func (s *Service) finalize(ctx context.Context, dep *DeploymentRecord) {
	dep.FinishedAt = sql.NullString{String: time.Now().UTC().Format(sqlTime), Valid: true}
	_ = s.repo.UpdateDeployment(ctx, dep)
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "secret_gen_failed", "Could not generate a webhook secret.")
	}
	return hex.EncodeToString(b), nil
}

func webhookURL(siteUID, secret string) string {
	return "/hooks/git/" + siteUID + "?secret=" + secret
}

func toSourceView(r *SourceRecord) *Source {
	return &Source{
		UID: r.UID, RepoURL: r.RepoURL, Branch: r.Branch,
		BuildCommand: r.BuildCommand, WebRoot: r.WebRoot,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func toDeploymentView(r *DeploymentRecord) *Deployment {
	return &Deployment{
		UID: r.UID, CommitSHA: r.CommitSHA, Status: r.Status,
		Trigger: r.TriggerKind, Log: r.Log,
		CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt.String,
	}
}
