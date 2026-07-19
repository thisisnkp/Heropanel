package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/dns"
	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/internal/git"
	"github.com/thisisnkp/heropanel/internal/job"
	"github.com/thisisnkp/heropanel/internal/php"
	"github.com/thisisnkp/heropanel/internal/registry"
	"github.com/thisisnkp/heropanel/internal/runtime"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/internal/ssl"
)

// stubUsers satisfies UserDirectory so the /users route mounts. It is never
// invoked — the spec builder only walks the routing tree, it does not serve.
type stubUsers struct{}

func (stubUsers) List(context.Context, int, int) ([]UserSummary, error) { return nil, nil }

// fullRouter mounts every optional route by making each service non-nil. The
// services are zero-value pointers: router construction captures them in handler
// closures but never dereferences them, so this is safe for a walk. Auth is the
// gate for the whole authenticated group, so it must be present too.
func fullRouter(t *testing.T) chi.Routes {
	t.Helper()
	cfg := config.Default()
	cfg.Security.RateLimit.Enabled = false
	h := NewRouter(Deps{
		Ctx:       context.Background(),
		Config:    cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test",
		StartedAt: time.Now(),
		Auth:      &auth.Service{},
		Audit:     &audit.Service{},
		Users:     stubUsers{},
		Sites:     &site.Service{},
		PHP:       &php.Service{},
		Databases: &database.Service{},
		SSL:       &ssl.Service{},
		DNS:       &dns.Service{},
		Domains:   &domain.Service{},
		Git:       &git.Service{},
		Runtime:   &runtime.Service{},
		Jobs:      &job.Dispatcher{},
		Registry:  registry.New(),
	})
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("router is not chi.Routes (%T)", h)
	}
	return routes
}

// TestOpenAPINoUndocumentedRoutes is the drift guard: every route actually
// mounted must have an apiDocs entry. When this fails, the fix is to document
// the new route, not to loosen the test — that is the whole point.
func TestOpenAPINoUndocumentedRoutes(t *testing.T) {
	buildOpenAPI(fullRouter(t), "test")
	if undoc := UndocumentedRoutes(); len(undoc) > 0 {
		t.Fatalf("routes are mounted but undocumented (add them to apiDocs):\n  %s",
			strings.Join(undoc, "\n  "))
	}
}

// TestOpenAPIRefsResolve ensures no operation or schema references a component
// schema that does not exist — a dangling $ref makes the document unusable to
// generators.
func TestOpenAPIRefsResolve(t *testing.T) {
	doc := buildOpenAPI(fullRouter(t), "test")
	var refs []string
	collectRefs(doc, &refs)
	if len(refs) == 0 {
		t.Fatal("expected the document to contain $refs")
	}
	for _, r := range refs {
		name := strings.TrimPrefix(r, "#/components/schemas/")
		if name == r {
			t.Errorf("unexpected $ref form: %q", r)
			continue
		}
		if _, ok := openapiSchemas[name]; !ok {
			t.Errorf("$ref %q points at an undefined schema", r)
		}
	}
}

// TestOpenAPIShape checks the top-level document is a well-formed OpenAPI 3.1
// object with the pieces tooling requires.
func TestOpenAPIShape(t *testing.T) {
	doc := buildOpenAPI(fullRouter(t), "test")
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", doc["openapi"])
	}
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) < 50 {
		t.Errorf("expected the full surface (50+ paths), got %d", len(paths))
	}
	// Spot-check that a permission-gated operation surfaces its scope and that
	// path params are derived.
	sites, _ := paths["/api/v1/sites/{uid}"].(map[string]any)
	get, _ := sites["get"].(map[string]any)
	if get["x-required-permission"] != "site.read" {
		t.Errorf("GET /sites/{uid} permission = %v, want site.read", get["x-required-permission"])
	}
	params, _ := get["parameters"].([]any)
	if len(params) != 1 {
		t.Fatalf("expected 1 path param on /sites/{uid}, got %d", len(params))
	}
	comps, _ := doc["components"].(map[string]any)
	schemes, _ := comps["securitySchemes"].(map[string]any)
	if _, ok := schemes["sessionCookie"]; !ok {
		t.Error("missing sessionCookie security scheme")
	}
	if _, ok := schemes["apiKey"]; !ok {
		t.Error("missing apiKey security scheme")
	}
}

// TestOpenAPIGolden keeps docs/openapi.json in step with the code. Regenerate
// with:  HP_UPDATE_OPENAPI=1 go test ./internal/httpapi -run Golden
func TestOpenAPIGolden(t *testing.T) {
	doc := buildOpenAPI(fullRouter(t), openapiGoldenVersion)
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	path := filepath.Join("..", "..", "docs", "openapi.json")
	if os.Getenv("HP_UPDATE_OPENAPI") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with HP_UPDATE_OPENAPI=1): %v", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != string(got) {
		t.Fatalf("docs/openapi.json is stale — regenerate with HP_UPDATE_OPENAPI=1 go test ./internal/httpapi -run Golden")
	}
}

// openapiGoldenVersion pins the version string in the committed spec so the
// golden does not churn on every build's version stamp.
const openapiGoldenVersion = "v1"

func collectRefs(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "$ref" {
				if s, ok := val.(string); ok {
					*out = append(*out, s)
				}
				continue
			}
			collectRefs(val, out)
		}
	case []any:
		for _, e := range t {
			collectRefs(e, out)
		}
	}
}
