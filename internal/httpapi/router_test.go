package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/httpapi"
)

func testRouter(t *testing.T, mutate func(*config.Config)) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.Security.RateLimit.Enabled = false // default: avoid the janitor goroutine
	if mutate != nil {
		mutate(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return httpapi.NewRouter(httpapi.Deps{
		Ctx:       ctx,
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
		StartedAt: time.Now(),
	})
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	rec := do(t, testRouter(t, nil), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestReadyz(t *testing.T) {
	rec := do(t, testRouter(t, nil), http.MethodGet, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env struct {
		Data struct {
			Status     string            `json:"status"`
			Components map[string]string `json:"components"`
		} `json:"data"`
		Meta struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if env.Data.Status != "ready" || len(env.Data.Components) == 0 {
		t.Fatalf("unexpected readyz payload: %+v", env.Data)
	}
	if env.Meta.RequestID == "" {
		t.Fatal("meta.request_id should be populated")
	}
}

func TestSystemInfo(t *testing.T) {
	rec := do(t, testRouter(t, nil), http.MethodGet, "/api/v1/system/info")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if env.Data["version"] != "test" {
		t.Fatalf("version = %v, want test", env.Data["version"])
	}
	if env.Data["arch"] != runtime.GOARCH {
		t.Fatalf("arch = %v, want %s", env.Data["arch"], runtime.GOARCH)
	}
}

func TestApiNotFoundEnvelope(t *testing.T) {
	// Unknown API paths return the JSON error envelope (not the SPA).
	rec := do(t, testRouter(t, nil), http.MethodGet, "/api/v1/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Fatalf("error code = %q, want not_found", env.Error.Code)
	}
}

func TestSPAFallbackServesIndex(t *testing.T) {
	// Unknown non-API GET routes serve the SPA (or placeholder) as HTML so
	// client-side routing can take over.
	rec := do(t, testRouter(t, nil), http.MethodGet, "/some/client/route")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec := do(t, testRouter(t, nil), http.MethodPost, "/healthz")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := do(t, testRouter(t, nil), http.MethodGet, "/")
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing X-Content-Type-Options")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing X-Frame-Options")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
}

func TestRequestIDHeader(t *testing.T) {
	rec := do(t, testRouter(t, nil), http.MethodGet, "/healthz")
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected X-Request-Id response header")
	}
}

func TestRateLimiterBlocksBurst(t *testing.T) {
	h := testRouter(t, func(c *config.Config) {
		c.Security.RateLimit = config.RateLimit{Enabled: true, RPS: 0.001, Burst: 2}
	})
	// Same client (httptest default RemoteAddr). Burst of 2 => 3rd is limited.
	if rec := do(t, h, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("req 1 status = %d, want 200", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("req 2 status = %d, want 200", rec.Code)
	}
	rec := do(t, h, http.MethodGet, "/healthz")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3 status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}
