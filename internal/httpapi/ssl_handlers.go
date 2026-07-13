package httpapi

import (
	"net/http"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/ssl"
)

func listCertsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := d.SSL.List(r.Context(), 0, 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if out == nil {
			out = []ssl.Cert{}
		}
		writeJSON(w, r, http.StatusOK, out)
	}
}

// issueSelfSignedHandler generates a self-signed cert for immediate HTTPS.
func issueSelfSignedHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Domain string `json:"domain"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		out, err := d.SSL.IssueSelfSigned(r.Context(), p.UserID, req.Domain)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// uploadCertHandler installs a caller-provided certificate + key.
func uploadCertHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			CertPEM string `json:"cert_pem"`
			KeyPEM  string `json:"key_pem"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		out, err := d.SSL.UploadCustom(r.Context(), p.UserID, req.CertPEM, req.KeyPEM)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// issueCertHandler obtains a Let's Encrypt certificate via ACME HTTP-01.
func issueCertHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Domain  string `json:"domain"`
			Webroot string `json:"webroot"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		out, err := d.SSL.Issue(r.Context(), p.UserID, req.Domain, req.Webroot)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, out)
	}
}
