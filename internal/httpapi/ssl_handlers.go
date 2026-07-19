package httpapi

import (
	"net/http"
	"strings"

	"github.com/thisisnkp/heropanel/internal/audit"
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
		audit.AddDetail(r.Context(), "domain", req.Domain)
		audit.AddDetail(r.Context(), "method", "self_signed")
		out, err := d.SSL.IssueSelfSigned(r.Context(), p.UserID, req.Domain)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "ssl", out.UID)
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
		// The certificate is public, but the key is not, and neither belongs in an
		// audit row. Record the identity the upload produced, which the service
		// parses out of the cert itself.
		audit.AddDetail(r.Context(), "method", "upload")
		out, err := d.SSL.UploadCustom(r.Context(), p.UserID, req.CertPEM, req.KeyPEM)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "common_name", out.CommonName)
		audit.SetResource(r.Context(), "ssl", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}

// issueCertHandler obtains a Let's Encrypt certificate. method "http" (default)
// uses HTTP-01 and needs a webroot; method "dns" uses DNS-01 via a zone
// HeroPanel is authoritative for, and is required for a wildcard ("*.example.com").
func issueCertHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Domain  string `json:"domain"`
			Webroot string `json:"webroot"`
			Method  string `json:"method"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		var (
			out *ssl.Cert
			err error
		)
		audit.AddDetail(r.Context(), "domain", req.Domain)
		// A wildcard can only be issued over DNS-01, so route it there regardless.
		if req.Method == "dns" || strings.HasPrefix(strings.TrimSpace(req.Domain), "*.") {
			audit.AddDetail(r.Context(), "method", "dns-01")
			out, err = d.SSL.IssueDNS(r.Context(), p.UserID, req.Domain)
		} else {
			audit.AddDetail(r.Context(), "method", "http-01")
			out, err = d.SSL.Issue(r.Context(), p.UserID, req.Domain, req.Webroot)
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "ssl", out.UID)
		writeJSON(w, r, http.StatusCreated, out)
	}
}
