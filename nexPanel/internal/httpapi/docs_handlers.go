package httpapi

import "net/http"

// docsPageHandler serves the /api/docs viewer — a small, dependency-free page
// that renders the OpenAPI document fetched from /api/v1/openapi.json. It is
// unauthenticated (like the spec it renders) so a developer can read the API
// before holding a session.
func docsPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, "text/html; charset=utf-8", docsHTML)
	}
}

// docsAssetHandler serves the viewer's same-origin CSS/JS (see docs_assets.go).
func docsAssetHandler(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, contentType, body)
	}
}

func serveAsset(w http.ResponseWriter, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
