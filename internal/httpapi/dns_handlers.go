package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/dns"
)

// listZonesHandler returns DNS zones. Gated by "dns.read".
func listZonesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zones, err := d.DNS.ListZones(r.Context(), 0, 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if zones == nil {
			zones = []dns.Zone{}
		}
		writeJSON(w, r, http.StatusOK, zones)
	}
}

// createZoneHandler creates an authoritative zone. Gated by "dns.write".
func createZoneHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		var req struct {
			Name       string `json:"name"`
			PrimaryNS  string `json:"primary_ns"`
			AdminEmail string `json:"admin_email"`
			NSIP       string `json:"ns_ip"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		zone, err := d.DNS.CreateZone(r.Context(), dns.CreateZoneInput{
			OwnerID: p.UserID, Name: req.Name, PrimaryNS: req.PrimaryNS, AdminEmail: req.AdminEmail, NSIP: req.NSIP,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, zone)
	}
}

// getZoneHandler returns one zone. Gated by "dns.read".
func getZoneHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zone, err := d.DNS.GetZone(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, zone)
	}
}

// deleteZoneHandler removes a zone. Gated by "dns.write".
func deleteZoneHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.DNS.DeleteZone(r.Context(), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// listRecordsHandler returns a zone's records. Gated by "dns.read".
func listRecordsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recs, err := d.DNS.ListRecords(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		if recs == nil {
			recs = []dns.Record{}
		}
		writeJSON(w, r, http.StatusOK, recs)
	}
}

// createRecordHandler adds a record to a zone (reloads BIND). Gated by "dns.write".
func createRecordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Content  string `json:"content"`
			TTL      int    `json:"ttl"`
			Priority int    `json:"priority"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		rec, err := d.DNS.AddRecord(r.Context(), chi.URLParam(r, "uid"), dns.AddRecordInput{
			Name: req.Name, Type: req.Type, Content: req.Content, TTL: req.TTL, Priority: req.Priority,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, rec)
	}
}

// deleteRecordHandler removes a record (reloads BIND). Gated by "dns.write".
func deleteRecordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.DNS.DeleteRecord(r.Context(), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
