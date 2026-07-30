package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/dns"
)

// listZonesHandler returns DNS zones. Gated by "dns.read".
func listZonesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		zones, err := listForTenant(d, r, func(ownerID int64) ([]dns.Zone, error) {
			return d.DNS.ListZones(r.Context(), ownerID, 50, 0)
		})
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
		audit.AddDetail(r.Context(), "zone", req.Name)
		zone, err := d.DNS.CreateZone(r.Context(), dns.CreateZoneInput{
			OwnerID: p.UserID, Name: req.Name, PrimaryNS: req.PrimaryNS, AdminEmail: req.AdminEmail, NSIP: req.NSIP,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.SetResource(r.Context(), "dns", zone.UID)
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

// getDNSSECHandler returns a zone's DNSSEC state and, when signed, the DS/DNSKEY
// the operator hands the registrar. Gated by "dns.read".
func getDNSSECHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.DNS.DNSSECStatus(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, st)
	}
}

// setDNSSECHandler turns inline signing on or off for a zone. Gated by
// "dns.write".
func setDNSSECHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.SetResource(r.Context(), "dns", chi.URLParam(r, "uid"))
		audit.AddDetail(r.Context(), "dnssec_enabled", req.Enabled)
		zone, err := d.DNS.SetDNSSEC(r.Context(), chi.URLParam(r, "uid"), req.Enabled)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, zone)
	}
}

// exportZoneHandler returns a zone as a standard RFC 1035 master file
// (text/plain). Gated by "dns.read".
func exportZoneHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		text, err := d.DNS.ExportZone(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(text))
	}
}

// importZoneHandler adds the records in a pasted master file to a zone. Gated by
// "dns.write". Additive: it does not delete existing records.
func importZoneHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ZoneFile string `json:"zone_file"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		audit.SetResource(r.Context(), "dns", chi.URLParam(r, "uid"))
		res, err := d.DNS.ImportZone(r.Context(), chi.URLParam(r, "uid"), req.ZoneFile)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "imported", res.Imported)
		writeJSON(w, r, http.StatusOK, res)
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
		audit.AddDetail(r.Context(), "record", req.Name)
		audit.AddDetail(r.Context(), "type", req.Type)
		audit.AddDetail(r.Context(), "content", req.Content)
		rec, err := d.DNS.AddRecord(r.Context(), chi.URLParam(r, "uid"), dns.AddRecordInput{
			Name: req.Name, Type: req.Type, Content: req.Content, TTL: req.TTL, Priority: req.Priority,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "record_uid", rec.UID)
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
