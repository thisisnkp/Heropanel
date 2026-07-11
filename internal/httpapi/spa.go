package httpapi

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded single-page app: real files (JS/CSS/assets)
// when they exist, and index.html as the fallback for client-side routes. API
// and health paths are never handled here (they are matched by more specific
// routes first; this is a defensive guard). When no build is embedded it serves
// the placeholder page.
func spaHandler(distFS fs.FS, hasSPA bool) http.HandlerFunc {
	if !hasSPA || distFS == nil {
		return func(w http.ResponseWriter, _ *http.Request) { writePlaceholder(w) }
	}
	fileServer := http.FileServer(http.FS(distFS))
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || p == "/healthz" || p == "/readyz" || p == "/metrics" {
			notFoundHandler(w, r)
			return
		}

		clean := strings.TrimPrefix(path.Clean(p), "/")
		if clean != "" {
			if f, err := distFS.Open(clean); err == nil {
				_ = f.Close()
				// Long-cache immutable, content-hashed assets.
				if strings.HasPrefix(clean, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, distFS)
	}
}

func serveIndex(w http.ResponseWriter, distFS fs.FS) {
	f, err := distFS.Open("index.html")
	if err != nil {
		writePlaceholder(w)
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
