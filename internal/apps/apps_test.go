package apps_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/apps"
	"github.com/thisisnkp/heropanel/internal/docker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeDeployer captures the compose file it was asked to bring up, so tests can
// assert what the template rendered without a daemon.
type fakeDeployer struct {
	available bool
	upProject string
	upSite    string
	upFile    string
	upErr     error
	published []int
	psResult  []docker.ComposeService
}

func (f *fakeDeployer) ComposeUp(_ context.Context, project, site, file string) (string, error) {
	f.upProject, f.upSite, f.upFile = project, site, file
	return "", f.upErr
}
func (f *fakeDeployer) ComposeDown(context.Context, string) error { return nil }
func (f *fakeDeployer) ComposePs(context.Context, string) ([]docker.ComposeService, error) {
	return f.psResult, nil
}
func (f *fakeDeployer) ComposeLogs(context.Context, string, int) (*docker.Logs, error) {
	return &docker.Logs{}, nil
}
func (f *fakeDeployer) PublishedPorts(context.Context) ([]int, error) { return f.published, nil }
func (f *fakeDeployer) Available(context.Context) bool                { return f.available }

func TestCatalogHasTheExitCriteriaApps(t *testing.T) {
	// The phase's exit criteria name Ghost and Uptime Kuma specifically.
	for _, slug := range []string{"ghost", "uptime-kuma"} {
		if _, ok := apps.Get(slug); !ok {
			t.Errorf("the catalog is missing %q, named in the phase exit criteria", slug)
		}
	}
}

// A secret field is generated, never taken from the operator's input, and never
// the same twice. This is the difference between a one-click deploy and a
// one-click way to stand up a service with a default password.
func TestSecretsAreGeneratedNotAcceptedAndAreUnique(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.New(d)

	// The operator tries to supply the secret directly; it must be ignored.
	res, err := svc.Deploy(context.Background(), apps.DeployInput{
		Slug: "ghost", Name: "blog",
		Values: map[string]string{"url": "https://blog.test", "db_password": "hunter2"},
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if res.Secrets["db_password"] == "" {
		t.Fatal("no secret was generated")
	}
	if res.Secrets["db_password"] == "hunter2" {
		t.Error("the operator-supplied secret was used instead of a generated one")
	}
	if len(res.Secrets["db_password"]) < 20 {
		t.Errorf("generated secret is too short to be strong: %q", res.Secrets["db_password"])
	}
	// The rendered compose carries the generated secret, not the operator's.
	if strings.Contains(d.upFile, "hunter2") {
		t.Error("the operator's password leaked into the compose file")
	}
	if !strings.Contains(d.upFile, res.Secrets["db_password"]) {
		t.Error("the generated secret did not reach the compose file")
	}

	// A second deploy must generate a different secret.
	res2, _ := svc.Deploy(context.Background(), apps.DeployInput{
		Slug: "ghost", Name: "blog2", Values: map[string]string{"url": "https://b2.test"},
	})
	if res.Secrets["db_password"] == res2.Secrets["db_password"] {
		t.Error("two deploys produced the same secret")
	}
}

// A deploy the host cannot run must be refused before anything is pulled, with a
// message naming both numbers — not discovered when the OOM killer arrives.
func TestInsufficientMemoryIsRefusedUpFront(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.NewWithMemory(d, func() int { return 200 }) // 200 MB available

	_, err := svc.Deploy(context.Background(), apps.DeployInput{
		Slug: "ghost", Name: "blog", Values: map[string]string{"url": "https://b.test"},
	})
	if errx.KindOf(err) != errx.KindValidation {
		t.Fatalf("kind = %v, want Validation", errx.KindOf(err))
	}
	if !strings.Contains(err.Error(), "512") || !strings.Contains(err.Error(), "200") {
		t.Errorf("error should name the required and available memory, got: %v", err)
	}
	// Nothing was deployed.
	if d.upFile != "" {
		t.Error("a stack was brought up despite insufficient memory")
	}
}

// Unknown memory (a non-Linux host, /proc absent) allows the deploy rather than
// blocking every one — the check is a safeguard, not a gate that fails closed on
// a platform where it cannot measure.
func TestUnknownMemoryDoesNotBlockDeploy(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.NewWithMemory(d, func() int { return 0 })
	if _, err := svc.Deploy(context.Background(), apps.DeployInput{
		Slug: "uptime-kuma", Name: "status",
	}); err != nil {
		t.Fatalf("deploy refused with unknown memory: %v", err)
	}
}

// A field value is interpolated into YAML. A newline could forge extra compose
// lines, so it is refused rather than escaped.
func TestFieldValuesCannotForgeYAML(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.New(d)
	_, err := svc.Deploy(context.Background(), apps.DeployInput{
		Slug: "ghost", Name: "blog",
		Values: map[string]string{"url": "https://b.test\n    privileged: true"},
	})
	if errx.KindOf(err) != errx.KindValidation {
		t.Fatalf("kind = %v, want Validation for a newline in a value", errx.KindOf(err))
	}
	if d.upFile != "" {
		t.Error("a stack was deployed with a forged value")
	}
}

// A required field with no value blocks the deploy with a clear message.
func TestRequiredFieldsAreEnforced(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.New(d)
	_, err := svc.Deploy(context.Background(), apps.DeployInput{Slug: "ghost", Name: "blog"})
	if errx.KindOf(err) != errx.KindValidation {
		t.Errorf("kind = %v, want Validation for a missing required field", errx.KindOf(err))
	}
}

// The app name becomes a compose project name, an argv element in a privileged
// command. A flag-shaped or path-shaped name must be refused.
func TestAppNameIsValidated(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.New(d)
	for _, bad := range []string{"--privileged", "../escape", "has/slash", "has space"} {
		if _, err := svc.Deploy(context.Background(), apps.DeployInput{
			Slug: "uptime-kuma", Name: bad,
		}); err == nil {
			t.Errorf("accepted %q as an app name", bad)
		}
	}
}

// Every published port in a rendered template is bound to loopback: an app must
// be reachable through a reverse proxy, not directly from the internet.
func TestTemplatesPublishOnlyToLoopback(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.New(d)
	for _, tc := range []struct{ slug, name string }{
		{"ghost", "g"}, {"uptime-kuma", "u"}, {"gitea", "gi"},
		{"postgres", "p"}, {"redis", "r"}, {"nginx-demo", "n"},
	} {
		values := map[string]string{"url": "https://x.test", "domain": "https://x.test", "db_name": "db"}
		if _, err := svc.Deploy(context.Background(), apps.DeployInput{Slug: tc.slug, Name: tc.name, Values: values}); err != nil {
			t.Fatalf("%s: %v", tc.slug, err)
		}
		for _, line := range strings.Split(d.upFile, "\n") {
			if strings.Contains(line, "ports:") || strings.Contains(line, "127.0.0.1") {
				if strings.Contains(line, "0.0.0.0") {
					t.Errorf("%s publishes on all interfaces: %q", tc.slug, line)
				}
			}
		}
		if strings.Contains(d.upFile, "ports:") && !strings.Contains(d.upFile, "127.0.0.1:") {
			t.Errorf("%s publishes a port without binding loopback:\n%s", tc.slug, d.upFile)
		}
	}
}

// A new deploy must not be handed a port a running container already publishes.
// The old in-memory counter reset to 39000 on restart and would collide; reading
// docker's live set makes that impossible.
func TestPortAllocationSkipsPortsAlreadyPublished(t *testing.T) {
	d := &fakeDeployer{available: true, published: []int{39000, 39001}}
	svc := apps.New(d)
	if _, err := svc.Deploy(context.Background(), apps.DeployInput{
		Slug: "uptime-kuma", Name: "status",
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	// 39000 and 39001 are taken, so the stack must publish on 39002.
	if !strings.Contains(d.upFile, "127.0.0.1:39002:") {
		t.Errorf("deploy reused a taken port; compose:\n%s", d.upFile)
	}
}

// Upstream resolves a proxy site's target live from the app's published port, so
// the vhost is always pointed where the app actually is.
func TestUpstreamResolvesTheLivePublishedPort(t *testing.T) {
	d := &fakeDeployer{available: true, psResult: []docker.ComposeService{
		{Name: "blog-ghost-1", Service: "ghost", State: "running", Ports: "127.0.0.1:39007->2368/tcp"},
	}}
	svc := apps.New(d)
	up, ok := svc.Upstream(context.Background(), "blog")
	if !ok || up != "127.0.0.1:39007" {
		t.Fatalf("Upstream = %q ok=%v, want 127.0.0.1:39007", up, ok)
	}

	// An app with nothing published (down, or no ports) does not resolve, so the
	// proxy site falls back to a static vhost rather than proxying to a dead port.
	d2 := &fakeDeployer{available: true, psResult: nil}
	if _, ok := apps.New(d2).Upstream(context.Background(), "gone"); ok {
		t.Error("Upstream resolved for an app with no published port")
	}
}

func TestCatalogFeasibilityReflectsMemory(t *testing.T) {
	d := &fakeDeployer{available: true}
	svc := apps.NewWithMemory(d, func() int { return 300 })
	for _, v := range svc.Catalog() {
		if v.MinMemoryMB > 300 && v.Feasible {
			t.Errorf("%s needs %d MB but is marked feasible with 300 MB", v.Slug, v.MinMemoryMB)
		}
		if v.MinMemoryMB <= 300 && !v.Feasible {
			t.Errorf("%s needs only %d MB but is marked infeasible with 300 MB", v.Slug, v.MinMemoryMB)
		}
	}
}
