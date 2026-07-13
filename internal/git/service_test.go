package git_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// ── test doubles ─────────────────────────────────────────────────────────────

type gwCall struct {
	capability string
	input      map[string]any
}

type mockGW struct {
	calls        []gwCall
	failOn       string
	deployResult map[string]any
}

func (m *mockGW) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	m.calls = append(m.calls, gwCall{capability: capability, input: in})
	if m.failOn == capability {
		return nil, errx.New(errx.KindUpstream, "boom", "simulated broker failure")
	}
	if capability == "git.deploy" && m.deployResult != nil {
		return m.deployResult, nil
	}
	return map[string]any{"ok": true}, nil
}

func (m *mockGW) Health(context.Context) error { return nil }

type fakeSites struct {
	ref *git.SiteRef
	err error
}

func (f fakeSites) Resolve(_ context.Context, uid string) (*git.SiteRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	r := *f.ref
	r.UID = uid
	return &r, nil
}

// fakeRepo is an in-memory git.Repo. It stores deployment pointers, so the
// service's in-place finalize is reflected on read.
type fakeRepo struct {
	sources map[int64]*git.SourceRecord
	deploys []*git.DeploymentRecord
	nextID  int64
}

func newFakeRepo() *fakeRepo { return &fakeRepo{sources: map[int64]*git.SourceRecord{}} }

func (r *fakeRepo) UpsertSource(_ context.Context, rec *git.SourceRecord) error {
	if existing, ok := r.sources[rec.SiteID]; ok {
		rec.UID = existing.UID
		rec.CreatedAt = existing.CreatedAt
	}
	if rec.UID == "" {
		r.nextID++
		rec.UID = "src-" + itoa(r.nextID)
		rec.CreatedAt = "now"
	}
	rec.UpdatedAt = "now"
	cp := *rec
	r.sources[rec.SiteID] = &cp
	return nil
}

func (r *fakeRepo) GetSourceBySiteID(_ context.Context, siteID int64) (*git.SourceRecord, error) {
	if s, ok := r.sources[siteID]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, errx.NotFound("git_source_not_found", "no source")
}

func (r *fakeRepo) InsertDeployment(_ context.Context, rec *git.DeploymentRecord) error {
	r.nextID++
	rec.ID = r.nextID
	if rec.UID == "" {
		rec.UID = "dep-" + itoa(r.nextID)
	}
	rec.CreatedAt = "now"
	r.deploys = append(r.deploys, rec) // store pointer
	return nil
}

func (r *fakeRepo) UpdateDeployment(_ context.Context, _ *git.DeploymentRecord) error { return nil }

func (r *fakeRepo) ListDeployments(_ context.Context, siteID int64, limit int) ([]git.DeploymentRecord, error) {
	var out []git.DeploymentRecord
	for i := len(r.deploys) - 1; i >= 0 && len(out) < limit; i-- {
		if r.deploys[i].SiteID == siteID {
			out = append(out, *r.deploys[i])
		}
	}
	return out, nil
}

func (r *fakeRepo) GetDeploymentByUID(_ context.Context, uid string) (*git.DeploymentRecord, error) {
	for _, d := range r.deploys {
		if d.UID == uid {
			cp := *d
			return &cp, nil
		}
	}
	return nil, errx.NotFound("deployment_not_found", "no deployment")
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func gitSite() *git.SiteRef {
	return &git.SiteRef{ID: 1, LinuxUser: "hps1", HomeDir: "/srv/heropanel/sites/1", DeployMode: "git"}
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestSetSourceValidatesAndRequiresGitMode(t *testing.T) {
	repo := newFakeRepo()
	svc := git.NewService(repo, fakeSites{ref: gitSite()}, &mockGW{})
	ctx := context.Background()

	// Bad repo URL is rejected before any persistence.
	if _, err := svc.SetSource(ctx, "site-uid", git.SetSourceInput{RepoURL: "http://insecure/x"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for non-https url, got %v", err)
	}

	// A non-git site cannot have a source.
	bare := git.NewService(repo, fakeSites{ref: &git.SiteRef{ID: 1, DeployMode: "baremetal"}}, &mockGW{})
	if _, err := bare.SetSource(ctx, "site-uid", git.SetSourceInput{RepoURL: "https://github.com/acme/app.git"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for non-git site, got %v", err)
	}
}

func TestSetSourceGeneratesAndPreservesWebhookSecret(t *testing.T) {
	repo := newFakeRepo()
	svc := git.NewService(repo, fakeSites{ref: gitSite()}, &mockGW{})
	ctx := context.Background()

	first, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/app.git", Branch: "main", WebRoot: "public",
	})
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	if !strings.HasPrefix(first.WebhookURL, "/hooks/git/acme-uid?secret=") {
		t.Fatalf("webhook url = %q", first.WebhookURL)
	}
	secret1 := strings.TrimPrefix(first.WebhookURL, "/hooks/git/acme-uid?secret=")
	if len(secret1) != 64 {
		t.Fatalf("secret len = %d, want 64 hex chars", len(secret1))
	}

	// A second update keeps the same secret so an existing webhook keeps working.
	second, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/app2.git", Branch: "release",
	})
	if err != nil {
		t.Fatalf("update source: %v", err)
	}
	if got := strings.TrimPrefix(second.WebhookURL, "/hooks/git/acme-uid?secret="); got != secret1 {
		t.Fatalf("secret changed on update: %q -> %q", secret1, got)
	}
}

func TestRunDeployRecordsAndInvokesBroker(t *testing.T) {
	repo := newFakeRepo()
	gw := &mockGW{deployResult: map[string]any{"commit": "abc1234", "log": "built ok"}}
	svc := git.NewService(repo, fakeSites{ref: gitSite()}, gw)
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{
		RepoURL: "https://github.com/acme/app.git", Branch: "main", BuildCommand: "npm ci", WebRoot: "dist",
	}); err != nil {
		t.Fatalf("set source: %v", err)
	}

	dep, err := svc.RunDeploy(ctx, "acme-uid", git.TriggerManual, job.Noop)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if dep.Status != git.StatusSuccess || dep.CommitSHA != "abc1234" {
		t.Fatalf("deployment = %+v", dep)
	}

	// The broker was invoked with the site's identity and the source config.
	var call *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "git.deploy" {
			call = &gw.calls[i]
		}
	}
	if call == nil {
		t.Fatalf("git.deploy was not invoked: %+v", gw.calls)
	}
	if call.input["username"] != "hps1" || call.input["home"] != "/srv/heropanel/sites/1" ||
		call.input["repo_url"] != "https://github.com/acme/app.git" || call.input["branch"] != "main" ||
		call.input["build_command"] != "npm ci" || call.input["web_root"] != "dist" {
		t.Fatalf("git.deploy input = %+v", call.input)
	}
	if rid, _ := call.input["release_id"].(string); rid == "" {
		t.Fatalf("expected a generated release_id, got %+v", call.input)
	}

	// The deployment is visible in history as a success.
	hist, _ := svc.ListDeployments(ctx, "acme-uid", 10)
	if len(hist) != 1 || hist[0].Status != git.StatusSuccess {
		t.Fatalf("history = %+v", hist)
	}
}

func TestRunDeployMarksFailureOnBrokerError(t *testing.T) {
	repo := newFakeRepo()
	gw := &mockGW{failOn: "git.deploy"}
	svc := git.NewService(repo, fakeSites{ref: gitSite()}, gw)
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{RepoURL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if _, err := svc.RunDeploy(ctx, "acme-uid", git.TriggerManual, job.Noop); err == nil {
		t.Fatal("expected deploy error")
	}
	hist, _ := svc.ListDeployments(ctx, "acme-uid", 10)
	if len(hist) != 1 || hist[0].Status != git.StatusFailed {
		t.Fatalf("expected one failed deployment, got %+v", hist)
	}
}

func TestVerifyWebhookConstantTime(t *testing.T) {
	repo := newFakeRepo()
	svc := git.NewService(repo, fakeSites{ref: gitSite()}, &mockGW{})
	ctx := context.Background()

	src, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{RepoURL: "https://github.com/acme/app.git"})
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	secret := strings.TrimPrefix(src.WebhookURL, "/hooks/git/acme-uid?secret=")

	if _, err := svc.VerifyWebhook(ctx, "acme-uid", secret); err != nil {
		t.Fatalf("correct secret should pass: %v", err)
	}
	if _, err := svc.VerifyWebhook(ctx, "acme-uid", "wrong"); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("wrong secret should be forbidden, got %v", err)
	}
	if _, err := svc.VerifyWebhook(ctx, "acme-uid", ""); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("empty secret should be forbidden, got %v", err)
	}
}

func TestRunRollbackActivatesPriorRelease(t *testing.T) {
	repo := newFakeRepo()
	gw := &mockGW{deployResult: map[string]any{"commit": "c1", "log": ""}}
	svc := git.NewService(repo, fakeSites{ref: gitSite()}, gw)
	ctx := context.Background()

	if _, err := svc.SetSource(ctx, "acme-uid", git.SetSourceInput{RepoURL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("set source: %v", err)
	}
	first, err := svc.RunDeploy(ctx, "acme-uid", git.TriggerManual, job.Noop)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	rb, err := svc.RunRollback(ctx, "acme-uid", first.UID, job.Noop)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rb.Trigger != git.TriggerRollback || rb.Status != git.StatusSuccess {
		t.Fatalf("rollback deployment = %+v", rb)
	}

	// git.rollback was invoked pointing at the first release's directory.
	var rbCall *gwCall
	for i := range gw.calls {
		if gw.calls[i].capability == "git.rollback" {
			rbCall = &gw.calls[i]
		}
	}
	if rbCall == nil || rbCall.input["home"] != "/srv/heropanel/sites/1" {
		t.Fatalf("git.rollback input = %+v", rbCall)
	}
	if rd, _ := rbCall.input["release_dir"].(string); !strings.HasPrefix(rd, "/srv/heropanel/sites/1/releases/") {
		t.Fatalf("rollback release_dir = %v", rbCall.input["release_dir"])
	}
}
