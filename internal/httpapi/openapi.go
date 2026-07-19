package httpapi

// OpenAPI 3.1 description of hpd's HTTP surface.
//
// The spec is not hand-maintained as a separate file that drifts from the code.
// Instead buildOpenAPI walks the *live* chi routing tree, so every path and
// method in the document is one that is actually mounted, and enriches each
// operation from opMeta below. A route that exists but has no opMeta entry is a
// documentation gap the drift test (openapi_test.go) fails on — that is the
// mechanism that keeps "REST endpoints + OpenAPI annotations" (docs/10 DoD)
// honest as the surface grows.
//
// Path parameters are derived from the chi pattern ({uid}, {did}, …), which is
// already OpenAPI path-templating syntax, so no translation is needed. Every
// operation inherits the two security schemes (session cookie or bearer API
// key) and the standard error responses; per-operation metadata only has to
// state what is specific to it.

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// opMeta is the hand-written half of an operation: everything that cannot be
// read off the routing tree. reqSchema/respSchema are inline JSON Schema
// fragments (usually a $ref) or nil.
type opMeta struct {
	Summary    string
	Tags       []string
	Permission string // the requirePermission scope, surfaced as x-required-permission
	ReqSchema  map[string]any
	ReqDesc    string
	RespSchema map[string]any // the *data* payload; the envelope is added around it
	RespStatus int            // success status (defaults to 200)
	RespDesc   string
	NoAuth     bool // true for infra/bootstrap routes that carry no session
	NoEnvelope bool // true when the response is not wrapped in {data, meta}
}

// ref returns a JSON Schema reference to a named component schema.
func ref(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }

// arrayOf returns an array schema whose items are the given schema.
func arrayOf(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

// prop is a tiny helper for {type: t, description: d} property schemas.
func prop(t, desc string) map[string]any {
	m := map[string]any{"type": t}
	if desc != "" {
		m["description"] = desc
	}
	return m
}

// object builds an object schema from a property map and a required list.
func object(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

var (
	openapiDoc  map[string]any
	openapiJSON []byte
	openapiOnce sync.Once
)

// openapiHandler serves the generated spec. It builds once against the router it
// is mounted on and caches the marshalled bytes. The spec is served without an
// envelope (it is itself the document a client tooling expects) and without
// auth — an API description leaks no secrets and clients need it to authenticate
// in the first place.
func openapiHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		openapiOnce.Do(func() {
			// r.Context()'s router is the one serving this request; walking it
			// yields exactly the mounted surface.
			routes := chi.RouteContext(r.Context()).Routes
			openapiDoc = buildOpenAPI(routes, d.Version)
			openapiJSON, _ = json.MarshalIndent(openapiDoc, "", "  ")
		})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiJSON)
	}
}

// buildOpenAPI walks the routing tree and assembles the OpenAPI 3.1 document.
// It is exported to the package (not the handler) so the drift test can build
// the spec from a freshly constructed router without an HTTP round-trip.
func buildOpenAPI(routes chi.Routes, version string) map[string]any {
	paths := map[string]any{}
	undocumented := []string{}

	_ = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if skipRoute(method, route) {
			return nil
		}
		key := method + " " + route
		meta, ok := apiDocs[key]
		if !ok {
			undocumented = append(undocumented, key)
			return nil
		}
		item, _ := paths[route].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[route] = item
		}
		item[strings.ToLower(method)] = buildOperation(route, meta)
		return nil
	})

	// undocumentedRoutes is stashed for the drift test; it is not part of the
	// served document.
	lastUndocumented = undocumented

	if version == "" {
		version = "dev"
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "HeroPanel API",
			"version":     version,
			"description": "Control-plane API for the HeroPanel hosting panel (hpd). All responses are wrapped in a `{data, meta}` envelope; errors in a `{error}` envelope. Authenticate with a session cookie (browser) or a bearer API key (programmatic).",
			"license":     map[string]any{"name": "See repository"},
		},
		"servers":  []any{map[string]any{"url": "/", "description": "This hpd instance"}},
		"tags":     openapiTags,
		"security": []any{map[string]any{"sessionCookie": []any{}}, map[string]any{"apiKey": []any{}}},
		"paths":    paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"sessionCookie": map[string]any{
					"type": "apiKey", "in": "cookie", "name": sessionCookieName,
					"description": "Browser session established by POST /api/v1/auth/login. Mutations additionally require the double-submit CSRF token (cookie " + csrfCookieName + " echoed in the X-CSRF-Token header) when CSRF is enabled.",
				},
				"apiKey": map[string]any{
					"type": "http", "scheme": "bearer",
					"description": "Programmatic access with a scoped API key minted at POST /api/v1/account/api-keys. Sent as `Authorization: Bearer <key>`.",
				},
			},
			"schemas": openapiSchemas,
		},
	}
}

// buildOperation assembles one operation object from its metadata plus the
// path parameters derived from the route pattern.
func buildOperation(route string, m opMeta) map[string]any {
	op := map[string]any{
		"summary":     m.Summary,
		"operationId": operationID(route, m),
		"tags":        m.Tags,
	}
	if m.Permission != "" {
		op["x-required-permission"] = m.Permission
		op["description"] = "Requires the `" + m.Permission + "` permission."
	}
	if params := pathParams(route); len(params) > 0 {
		op["parameters"] = params
	}
	if m.NoAuth {
		op["security"] = []any{} // opt out of the global requirement
	}
	if m.ReqSchema != nil {
		desc := m.ReqDesc
		if desc == "" {
			desc = "Request body."
		}
		op["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": m.ReqSchema}},
		}
		_ = desc
	}
	op["responses"] = buildResponses(m)
	return op
}

// buildResponses wraps the success payload in the standard envelope and appends
// the shared error responses that every authenticated route can return.
func buildResponses(m opMeta) map[string]any {
	status := m.RespStatus
	if status == 0 {
		status = http.StatusOK
	}
	desc := m.RespDesc
	if desc == "" {
		desc = "Success."
	}

	var schema map[string]any
	switch {
	case m.RespSchema == nil:
		schema = nil
	case m.NoEnvelope:
		schema = m.RespSchema
	default:
		schema = object(map[string]any{"data": m.RespSchema, "meta": ref("Meta")}, "data", "meta")
	}

	resp := map[string]any{}
	success := map[string]any{"description": desc}
	if schema != nil {
		success["content"] = map[string]any{"application/json": map[string]any{"schema": schema}}
	}
	resp[itoa(status)] = success

	// Shared error responses. Public routes cannot 401/403 on auth, but they can
	// still 400 (bad input) and 500, so those are always present.
	resp["400"] = errRef("Validation or request error")
	resp["500"] = errRef("Internal error")
	if !m.NoAuth {
		resp["401"] = errRef("Not authenticated")
		if m.Permission != "" {
			resp["403"] = errRef("Authenticated but missing the required permission")
		}
	}
	return resp
}

func errRef(desc string) map[string]any {
	return map[string]any{
		"description": desc,
		"content":     map[string]any{"application/json": map[string]any{"schema": ref("ErrorEnvelope")}},
	}
}

// pathParams turns each {name} segment of a chi pattern into a required string
// path parameter.
func pathParams(route string) []any {
	var params []any
	for _, seg := range strings.Split(route, "/") {
		if len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			name := seg[1 : len(seg)-1]
			params = append(params, map[string]any{
				"name": name, "in": "path", "required": true,
				"description": paramDesc(name),
				"schema":      map[string]any{"type": "string"},
			})
		}
	}
	return params
}

func paramDesc(name string) string {
	switch name {
	case "uid":
		return "Resource UID."
	case "did":
		return "Domain UID."
	case "dep":
		return "Deployment UID."
	case "id":
		return "Job ID."
	}
	return name + " path parameter."
}

// operationID builds a stable, unique operationId from method + path.
func operationID(route string, m opMeta) string {
	method := "get"
	// The method is not on opMeta; derive a readable id from the route + summary
	// slug instead. Uniqueness is guaranteed by including the full path.
	_ = method
	slug := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(strings.TrimPrefix(route, "/"))
	return slug + "_" + slugify(m.Summary)
}

func slugify(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// skipRoute excludes non-API surface: the SPA catch-all, HEAD duplicates, the
// websocket upgrade (not describable as a REST operation), and the /api/docs
// viewer assets (a UI, not an API operation).
func skipRoute(method, route string) bool {
	if method == "HEAD" {
		return true
	}
	if route == "/*" || strings.HasPrefix(route, "/api/v1/ws") || strings.HasPrefix(route, "/api/docs") {
		return true
	}
	return false
}

func itoa(n int) string {
	// small, allocation-light itoa for status codes (100–599)
	return string(rune('0'+n/100)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}

// lastUndocumented records routes walked without an apiDocs entry, for the test.
var lastUndocumented []string

// UndocumentedRoutes returns the routes seen by the most recent buildOpenAPI
// call that had no documentation metadata. Used only by the drift test.
func UndocumentedRoutes() []string {
	out := append([]string(nil), lastUndocumented...)
	sort.Strings(out)
	return out
}
