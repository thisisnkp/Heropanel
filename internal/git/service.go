package git

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
	"github.com/thisisnkp/heropanel/pkg/secrets"
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
	cipher    *secrets.Cipher
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

// WithSecrets wires the cipher that seals Git credentials at rest. Without it
// the service still serves public repositories; private ones report
// "unavailable" rather than storing a token in the clear. Returns s for chaining.
func (s *Service) WithSecrets(c *secrets.Cipher) *Service {
	s.cipher = c
	return s
}

func (s *Service) requireBroker() error {
	if s.broker == nil {
		return errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; deployments cannot run.")
	}
	return nil
}

// requireSecrets guards every path that would persist or read a credential.
func (s *Service) requireSecrets() error {
	if !s.cipher.Configured() {
		return errx.New(errx.KindUnavailable, "secrets_unavailable",
			"Private repositories need an encryption key. Set security.secret_key (HP_SECRET_KEY) and restart the panel.")
	}
	return nil
}

// credentialAAD binds a sealed credential to one site's source row.
//
// It keys on site_id rather than the git_sources surrogate id because the source
// is upserted and is 1:1 with the site: the surrogate can change when a row is
// replaced, the site cannot. Either way a ciphertext moved to another row fails
// to open.
func credentialAAD(siteID int64) string {
	return secrets.AAD("git_sources", siteID, "credential_enc")
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

	existing, _ := s.repo.GetSourceBySiteID(ctx, ref.ID)

	secret := ""
	if existing != nil {
		secret = existing.WebhookSecret
	}
	if secret == "" {
		secret, err = randomSecret()
		if err != nil {
			return nil, err
		}
	}

	// Unspecified keeps whatever is stored, and defaults to on for a new source:
	// a PHP checkout with a composer.json is not runnable until its dependencies
	// are installed, so "on" is the only sensible default.
	autoComposer := true
	if existing != nil {
		autoComposer = existing.AutoComposer
	}
	if in.AutoComposer != nil {
		autoComposer = *in.AutoComposer
	}
	// An empty host key on update keeps the stored pin, so it can be set once and
	// survive later branch/build edits.
	hostKey := in.HostKey
	if hostKey == "" && existing != nil {
		hostKey = existing.HostKey
	}

	rec := &SourceRecord{
		SiteID:        ref.ID,
		RepoURL:       in.RepoURL,
		Branch:        in.Branch,
		BuildCommand:  in.BuildCommand,
		WebRoot:       in.WebRoot,
		WebhookSecret: secret,
		AuthKind:      in.AuthKind,
		AuthUsername:  in.AuthUsername,
		HostKey:       hostKey,
		AutoComposer:  autoComposer,
	}
	if err := s.applyAuth(rec, existing, &in, ref); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertSource(ctx, rec); err != nil {
		return nil, err
	}
	v := toSourceView(rec)
	v.WebhookURL = webhookURL(siteUID, secret)
	return v, nil
}

// applyAuth resolves the credential for the source being saved: seal a new one,
// carry the stored one forward, or generate a deploy key. It never returns the
// secret — only writes the sealed form onto rec.
func (s *Service) applyAuth(rec, existing *SourceRecord, in *SetSourceInput, ref *SiteRef) error {
	switch in.AuthKind {
	case AuthNone:
		rec.CredentialEnc, rec.PublicKey, rec.AuthUsername = "", "", ""
		return nil

	case AuthToken:
		if err := s.requireSecrets(); err != nil {
			return err
		}
		if in.Token == "" {
			// An update that leaves the token blank keeps the stored one, so
			// changing a branch does not force the operator to re-paste a secret
			// they may no longer have.
			if existing == nil || existing.AuthKind != AuthToken || existing.CredentialEnc == "" {
				return errx.Validation("token_required", "An access token is required for token authentication.",
					errx.Field{Field: "token", Code: "required", Message: "token required"})
			}
			rec.CredentialEnc = existing.CredentialEnc
			return nil
		}
		sealed, err := s.cipher.Seal([]byte(in.Token), credentialAAD(ref.ID))
		if err != nil {
			return errx.Wrap(err, errx.KindInternal, "seal_failed", "Could not encrypt the access token.")
		}
		rec.CredentialEnc, rec.PublicKey = sealed, ""
		return nil

	case AuthSSHKey:
		if err := s.requireSecrets(); err != nil {
			return err
		}
		reusable := existing != nil && existing.AuthKind == AuthSSHKey &&
			existing.CredentialEnc != "" && existing.PublicKey != ""
		if reusable && !in.RotateKey {
			// Keep the key the operator already registered on the repository.
			rec.CredentialEnc, rec.PublicKey = existing.CredentialEnc, existing.PublicKey
			return nil
		}
		priv, pub, err := generateDeployKey("heropanel-" + ref.LinuxUser)
		if err != nil {
			return err
		}
		sealed, err := s.cipher.Seal([]byte(priv), credentialAAD(ref.ID))
		if err != nil {
			return errx.Wrap(err, errx.KindInternal, "seal_failed", "Could not encrypt the deploy key.")
		}
		rec.CredentialEnc, rec.PublicKey = sealed, pub
		return nil
	}
	return errx.Validation("invalid_auth_kind", "Unsupported authentication kind.")
}

// brokerAuth unseals a source's credential into the payload git.deploy needs.
// The plaintext exists only for the life of this call and the broker request.
func (s *Service) brokerAuth(src *SourceRecord, siteID int64) (map[string]any, error) {
	if src.AuthKind == "" || src.AuthKind == AuthNone {
		return nil, nil
	}
	if err := s.requireSecrets(); err != nil {
		return nil, err
	}
	plain, err := s.cipher.Open(src.CredentialEnc, credentialAAD(siteID))
	if err != nil {
		return nil, errx.Wrap(err, errx.KindInternal, "credential_unreadable",
			"The stored Git credential could not be decrypted. Re-save the source to replace it.")
	}
	switch src.AuthKind {
	case AuthToken:
		return map[string]any{"kind": AuthToken, "username": src.AuthUsername, "secret": string(plain)}, nil
	case AuthSSHKey:
		return map[string]any{"kind": AuthSSHKey, "secret": string(plain), "host_key": src.HostKey}, nil
	}
	return nil, errx.Validation("invalid_auth_kind", "Unsupported authentication kind.")
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

// WebhookProof carries the ways a provider proves a push webhook is genuine,
// all verified against the source's webhook secret:
//   - GitHubSig: an HMAC-SHA256 of the raw body, "sha256=<hex>" (GitHub's
//     X-Hub-Signature-256). This is the strong proof — it binds the secret to
//     *this* payload, so a captured request cannot be replayed with a different
//     body.
//   - GitLabToken: GitLab's X-Gitlab-Token, a plain shared token.
//   - Secret: the bare secret in the URL (?secret=) or X-HeroPanel-Secret, the
//     fallback for a manual `curl` trigger.
//
// Any one that verifies authorizes the deploy; all comparisons are
// constant-time.
type WebhookProof struct {
	Body        []byte
	GitHubSig   string
	GitLabToken string
	Secret      string
}

// Kind names the proof that was presented, for the audit trail (the value is
// never recorded). "none" means the request carried no credential at all.
func (p WebhookProof) Kind() string {
	switch {
	case p.GitHubSig != "":
		return "github_signature"
	case p.GitLabToken != "":
		return "gitlab_token"
	case p.Secret != "":
		return "shared_secret"
	default:
		return "none"
	}
}

// VerifyWebhook resolves a site by UID and constant-time-compares the presented
// shared secret against the source's webhook secret. Retained for callers (and
// tests) that only have the bare secret; it delegates to VerifyWebhookSigned.
func (s *Service) VerifyWebhook(ctx context.Context, siteUID, secret string) (*SiteRef, error) {
	return s.VerifyWebhookSigned(ctx, siteUID, WebhookProof{Secret: secret})
}

// VerifyWebhookSigned authorizes a push webhook using whichever proof the
// provider supplied (GitHub signature, GitLab token, or the shared secret),
// returning the site on success so the caller can enqueue a deploy.
func (s *Service) VerifyWebhookSigned(ctx context.Context, siteUID string, proof WebhookProof) (*SiteRef, error) {
	ref, err := s.sites.Resolve(ctx, siteUID)
	if err != nil {
		return nil, err
	}
	src, err := s.repo.GetSourceBySiteID(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	if !webhookProofValid(src.WebhookSecret, proof) {
		return nil, errx.Forbidden("invalid_webhook_secret", "Invalid webhook signature or secret.")
	}
	return ref, nil
}

// webhookProofValid reports whether any presented proof verifies against secret.
// A source with no secret configured can never be authorized.
func webhookProofValid(secret string, p WebhookProof) bool {
	if secret == "" {
		return false
	}
	if p.GitHubSig != "" && githubSigValid(p.GitHubSig, secret, p.Body) {
		return true
	}
	if p.GitLabToken != "" && ctEqual(p.GitLabToken, secret) {
		return true
	}
	if p.Secret != "" && ctEqual(p.Secret, secret) {
		return true
	}
	return false
}

// githubSigValid verifies a GitHub "sha256=<hex>" signature over body.
func githubSigValid(sigHeader, secret string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := strings.ToLower(strings.TrimPrefix(sigHeader, prefix))
	got := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// ctEqual is a length-safe constant-time string compare.
func ctEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
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
	payload := map[string]any{
		"username":      ref.LinuxUser,
		"home":          ref.HomeDir,
		"repo_url":      src.RepoURL,
		"branch":        src.Branch,
		"build_command": src.BuildCommand,
		"web_root":      src.WebRoot,
		"release_id":    releaseID,
		"keep":          keepReleases,
		"auto_composer": src.AutoComposer,
	}
	auth, err := s.brokerAuth(src, ref.ID)
	if err != nil {
		dep.Status = StatusFailed
		dep.Log = err.Error()
		s.finalize(ctx, dep)
		return nil, err
	}
	if auth != nil {
		payload["auth"] = auth
	}
	res, derr := s.broker.Invoke(ctx, "git.deploy", payload)
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

	s.restartApp(ctx, siteUID, dep, p)
	s.finalize(ctx, dep)

	p.Report(100, "deployed")
	return toDeploymentView(dep), nil
}

// restartApp reloads a proxy site's process so it runs the release that `current`
// now points at, appending the outcome to the deployment log.
//
// Both a deploy and a rollback need this, and for the same reason: flipping the
// `current` symlink is enough for a static or PHP site (the web server reads the
// files per request), but a long-running app has already loaded its code from the
// old release and will happily keep serving it. A rollback that does not restart
// reports success while the bad release stays live — the worst possible outcome
// for the one operation an operator reaches for when things are already broken.
//
// Best-effort: the release is already active, so a restart failure is recorded
// rather than failing the operation. It is a no-op for sites without a runtime.
func (s *Service) restartApp(ctx context.Context, siteUID string, dep *DeploymentRecord, p job.Progress) {
	if s.restarter == nil {
		return
	}
	p.Report(95, "restarting app")
	if err := s.restarter.RestartForSite(ctx, siteUID); err != nil {
		dep.Log = strings.TrimSpace(dep.Log + "\n[warn] app restart failed: " + err.Error())
		return
	}
	dep.Log = strings.TrimSpace(dep.Log + "\n[app restarted]")
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
	// A proxy site's process has already loaded the bad release's code; without
	// this the rollback reports success while the bad release keeps serving.
	s.restartApp(ctx, siteUID, dep, p)
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
	kind := r.AuthKind
	if kind == "" {
		kind = AuthNone
	}
	return &Source{
		UID: r.UID, RepoURL: r.RepoURL, Branch: r.Branch,
		BuildCommand: r.BuildCommand, WebRoot: r.WebRoot,
		AuthKind: kind, AuthUsername: r.AuthUsername, PublicKey: r.PublicKey,
		HostKey:      r.HostKey,
		AutoComposer: r.AutoComposer,
		CreatedAt:    r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func toDeploymentView(r *DeploymentRecord) *Deployment {
	return &Deployment{
		UID: r.UID, CommitSHA: r.CommitSHA, Status: r.Status,
		Trigger: r.TriggerKind, Log: r.Log,
		CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt.String,
	}
}
