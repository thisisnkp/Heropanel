package runtime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeProber answers probes from a script, recording the URLs it was given.
type fakeProber struct {
	mu   sync.Mutex
	urls []string
	code int
	err  error
	// healthyAfter makes the first N probes fail, mimicking an app that needs a
	// moment to bind its port.
	healthyAfter int
	calls        int
}

func (p *fakeProber) Probe(_ context.Context, url string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.urls = append(p.urls, url)
	p.calls++
	if p.calls <= p.healthyAfter {
		return 0, errors.New("connection refused")
	}
	if p.err != nil {
		return 0, p.err
	}
	return p.code, nil
}

func (p *fakeProber) lastURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.urls) == 0 {
		return ""
	}
	return p.urls[len(p.urls)-1]
}

func healthSvc(t *testing.T, p runtime.Prober) (*runtime.Service, *fakeRepo, *mockGW) {
	t.Helper()
	repo := &fakeRepo{}
	gw := &mockGW{}
	// A short readiness window: these tests assert the logic, not the wall clock.
	svc := runtime.NewService(repo, fakeSites{ref: siteRef()}, gw).
		WithProber(p).
		WithReadyTimeout(400 * time.Millisecond)
	return svc, repo, gw
}

func setWithHealth(t *testing.T, svc *runtime.Service, path string) *runtime.Runtime {
	t.Helper()
	rt, err := svc.SetRuntime(context.Background(), "site-uid", runtime.SetInput{
		Runtime: runtime.RuntimeNode, Command: "node server.js", Port: 3000, HealthPath: path,
	})
	if err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	return rt
}

func TestHealthProbesTheAppOnLoopback(t *testing.T) {
	p := &fakeProber{code: 200}
	svc, _, _ := healthSvc(t, p)
	setWithHealth(t, svc, "/healthz")

	h, err := svc.Health(context.Background(), "site-uid")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if !h.Healthy || !h.Configured || h.StatusCode != 200 {
		t.Fatalf("health = %+v", h)
	}
	// Always loopback: the app binds 127.0.0.1 and the probe must go where the
	// web server goes, not somewhere a caller chose.
	if p.lastURL() != "http://127.0.0.1:3000/healthz" {
		t.Fatalf("probe url = %q", p.lastURL())
	}
}

// systemd reporting "started" only means the process was forked. Without a
// probe the panel would call a crash-on-boot app healthy.
func TestSetRuntimeReportsErrorWhenTheAppNeverAnswers(t *testing.T) {
	p := &fakeProber{err: errors.New("connection refused")}
	svc, repo, _ := healthSvc(t, p)

	rt, err := svc.SetRuntime(context.Background(), "site-uid", runtime.SetInput{
		Runtime: runtime.RuntimeNode, Command: "node broken.js", Port: 3000, HealthPath: "/healthz",
	})
	if err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	if rt.Status != runtime.StatusError {
		t.Fatalf("an app that never answered was reported as %q", rt.Status)
	}
	if repo.rec.Status != runtime.StatusError {
		t.Fatalf("stored status = %q", repo.rec.Status)
	}
}

// An app that takes a moment to bind must not be called broken.
func TestSetRuntimeWaitsForTheAppToComeUp(t *testing.T) {
	p := &fakeProber{code: 200, healthyAfter: 2}
	svc, _, _ := healthSvc(t, p)

	rt := setWithHealth(t, svc, "/healthz")
	if rt.Status != runtime.StatusRunning {
		t.Fatalf("status = %q, want running after the app came up", rt.Status)
	}
	if p.calls < 3 {
		t.Fatalf("expected retries while the app started, got %d probes", p.calls)
	}
}

// With no health path the panel must not invent a verdict either way.
func TestNoHealthPathMeansNoProbe(t *testing.T) {
	p := &fakeProber{code: 500}
	svc, _, _ := healthSvc(t, p)

	rt, err := svc.SetRuntime(context.Background(), "site-uid", runtime.SetInput{
		Runtime: runtime.RuntimeNode, Command: "node server.js", Port: 3000,
	})
	if err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	if rt.Status != runtime.StatusRunning {
		t.Fatalf("status = %q", rt.Status)
	}
	if p.calls != 0 {
		t.Fatal("an app with no health path was probed anyway")
	}

	h, err := svc.Health(context.Background(), "site-uid")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if h.Configured || h.Healthy {
		t.Fatalf("health without a probe should claim nothing: %+v", h)
	}
}

func TestHealthTreats2xxAnd3xxAsServing(t *testing.T) {
	for _, tc := range []struct {
		code int
		want bool
	}{
		{200, true},
		{204, true}, // a health endpoint that answers with no body is still a yes
		{301, true},
		{400, false},
		{404, false},
		{500, false},
		{502, false},
	} {
		p := &fakeProber{code: tc.code}
		svc, _, _ := healthSvc(t, p)
		setWithHealth(t, svc, "/healthz")

		h, _ := svc.Health(context.Background(), "site-uid")
		if h.Healthy != tc.want {
			t.Fatalf("HTTP %d: healthy = %v, want %v", tc.code, h.Healthy, tc.want)
		}
	}
}

// A deploy that builds fine but crashes the app is a very common failure; it
// must surface, not show green.
func TestRestartForSiteFailsWhenTheAppIsUnhealthy(t *testing.T) {
	p := &fakeProber{code: 502}
	svc, repo, _ := healthSvc(t, p)
	setWithHealth(t, svc, "/healthz")

	err := svc.RestartForSite(context.Background(), "site-uid")
	if err == nil {
		t.Fatal("a restart into an unhealthy app should report an error")
	}
	if !strings.Contains(err.Error(), "health check") {
		t.Fatalf("error should say what happened: %v", err)
	}
	if repo.rec.Status != runtime.StatusError {
		t.Fatalf("stored status = %q", repo.rec.Status)
	}
}

func TestRestartForSiteSucceedsWhenHealthy(t *testing.T) {
	p := &fakeProber{code: 200}
	svc, repo, _ := healthSvc(t, p)
	setWithHealth(t, svc, "/healthz")

	if err := svc.RestartForSite(context.Background(), "site-uid"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if repo.rec.Status != runtime.StatusRunning {
		t.Fatalf("stored status = %q", repo.rec.Status)
	}
}

// A stop is not an app failure; it must not be probed into "error".
func TestStopIsNotProbed(t *testing.T) {
	p := &fakeProber{err: errors.New("connection refused")}
	svc, _, _ := healthSvc(t, p)
	setWithHealth(t, svc, "/healthz")
	before := p.calls

	rt, err := svc.Control(context.Background(), "site-uid", "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if rt.Status != runtime.StatusStopped {
		t.Fatalf("status = %q, want stopped", rt.Status)
	}
	if p.calls != before {
		t.Fatal("a stopped app was health-probed")
	}
}

// The probe target is built from the site's own port, so the path must never be
// able to redirect it at another host.
func TestHealthPathValidation(t *testing.T) {
	svc, _, _ := healthSvc(t, &fakeProber{code: 200})
	for _, bad := range []string{
		"http://evil.test/steal",
		"//evil.test/steal",
		"healthz",
		"/health z",
		"/health\nInjected: 1",
		"/health#frag",
		"/" + strings.Repeat("a", 300),
	} {
		_, err := svc.SetRuntime(context.Background(), "site-uid", runtime.SetInput{
			Runtime: runtime.RuntimeNode, Command: "node s.js", Port: 3000, HealthPath: bad,
		})
		if !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%q: want validation, got %v", bad, err)
		}
	}
}

func TestHealthPathIsPersisted(t *testing.T) {
	svc, _, _ := healthSvc(t, &fakeProber{code: 200})
	setWithHealth(t, svc, "/healthz")

	rt, err := svc.GetRuntime(context.Background(), "site-uid")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rt.HealthPath != "/healthz" {
		t.Fatalf("health path = %q", rt.HealthPath)
	}
}
