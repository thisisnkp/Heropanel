package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/security"
)

// The security HTTP edge. The firewall is host-wide and high-stakes, so it
// carries its own security.read / security.write pair. Applying a ruleset is
// deliberately a two-step act: apply arms a change that reverts itself unless
// confirmed, so a rule that locks the operator out undoes itself.

// listFirewallHandler returns the ordered ruleset and the pending state.
// Gated by "security.read".
func listFirewallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rules, state, err := d.Firewall.ListRules(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		pending := state != nil && state.PendingToken != ""
		deadline := ""
		if state != nil {
			deadline = state.Deadline
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"rules": rules, "pending": pending, "deadline": deadline,
			"available": d.Firewall.Available(),
		})
	}
}

// addFirewallRuleHandler stores a rule (unapplied). Gated by "security.write".
func addFirewallRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in security.RuleInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "action", in.Action)
		audit.AddDetail(r.Context(), "protocol", in.Protocol)
		rule, err := d.Firewall.AddRule(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, rule)
	}
}

// deleteFirewallRuleHandler removes a rule (unapplied). Gated by "security.write".
func deleteFirewallRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "rule", uid)
		if err := d.Firewall.DeleteRule(r.Context(), uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// applyFirewallHandler applies the ruleset and arms the rollback timer. Gated
// by "security.write". The response's token must be sent back to Confirm
// before the deadline or the change reverts.
func applyFirewallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		audit.AddDetail(r.Context(), "firewall", "apply")
		res, err := d.Firewall.Apply(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, res)
	}
}

// confirmFirewallHandler makes the pending change permanent. Gated by
// "security.write".
func confirmFirewallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "firewall", "confirm")
		if err := d.Firewall.Confirm(r.Context(), in.Token); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "confirmed": true})
	}
}

// rollbackFirewallHandler reverts the pending change now. Gated by
// "security.write".
func rollbackFirewallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		audit.AddDetail(r.Context(), "firewall", "rollback")
		if err := d.Firewall.Rollback(r.Context()); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "rolled_back": true})
	}
}

// firewallStatusHandler reports the live ruleset (from nft). Gated by
// "security.read".
func firewallStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Firewall.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, st)
	}
}

// ── malware ──────────────────────────────────────────────────────────────────

// scanSiteHandler runs a malware scan over a site's tree and returns the
// detections. Gated by "security.write" (a scan is an action, not a view).
func scanSiteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "site", siteUID)
		scan, findings, err := d.Malware.ScanSite(r.Context(), siteUID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "infected", strconv.Itoa(scan.Infected))
		writeJSON(w, r, http.StatusOK, map[string]any{"scan": scan, "findings": findings})
	}
}

// listQuarantineHandler returns quarantined items (newest first). Gated by
// "security.read".
func listQuarantineHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := d.Malware.ListQuarantine(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"items": items, "available": d.Malware.Available(),
		})
	}
}

// quarantineHandler moves a detected file out of its site tree. Gated by
// "security.write".
func quarantineHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			SiteUID   string `json:"site_uid"`
			Path      string `json:"path"`
			Signature string `json:"signature"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "path", in.Path)
		item, err := d.Malware.Quarantine(r.Context(), in.SiteUID, in.Path, in.Signature)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, item)
	}
}

// restoreQuarantineHandler returns a quarantined item to its original place.
// Gated by "security.write".
func restoreQuarantineHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "quarantine", uid)
		if err := d.Malware.Restore(r.Context(), uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "restored": true})
	}
}

// deleteQuarantineHandler permanently removes a quarantined item. Gated by
// "security.write".
func deleteQuarantineHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "quarantine", uid)
		if err := d.Malware.Delete(r.Context(), uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// ── fail2ban ─────────────────────────────────────────────────────────────────

// fail2banHandler returns the jails and their banned IPs. Gated by
// "security.read".
func fail2banHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jails, running, err := d.Fail2Ban.Overview(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"jails": jails, "running": running, "available": d.Fail2Ban.Available(),
		})
	}
}

// listFirewallIPEntriesHandler returns the geo/IP allow-block entries. Gated by
// "security.read".
func listFirewallIPEntriesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := d.Firewall.ListIPEntries(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		if entries == nil {
			entries = []security.IPListEntry{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"entries": entries})
	}
}

// addFirewallIPEntryHandler adds an allow/block CIDR (applied at the next
// firewall apply). Gated by "security.write".
func addFirewallIPEntryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in security.IPListInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "cidr", in.CIDR)
		audit.AddDetail(r.Context(), "mode", in.Mode)
		e, err := d.Firewall.AddIPEntry(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, e)
	}
}

// deleteFirewallIPEntryHandler removes an allow/block entry. Gated by
// "security.write".
func deleteFirewallIPEntryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Firewall.DeleteIPEntry(r.Context(), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// listFirewallCountriesHandler lists the imported geo countries with their
// entry counts. Gated by "security.read".
func listFirewallCountriesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		countries, err := d.Firewall.ListCountries(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		if countries == nil {
			countries = []security.CountrySummary{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"countries": countries,
			"available": d.Firewall.GeoAvailable(),
		})
	}
}

// importFirewallCountryHandler bulk-imports a country's CIDR ranges as allow or
// block entries (applied at the next firewall apply). Gated by "security.write".
func importFirewallCountryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in security.CountryImportInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "country", in.Country)
		audit.AddDetail(r.Context(), "mode", in.Mode)
		res, err := d.Firewall.ImportCountry(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "count", res.Count)
		writeJSON(w, r, http.StatusCreated, res)
	}
}

// removeFirewallCountryHandler drops a previously-imported country's entries.
// Gated by "security.write".
func removeFirewallCountryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cc := chi.URLParam(r, "cc")
		audit.AddDetail(r.Context(), "country", cc)
		if err := d.Firewall.RemoveCountry(r.Context(), cc); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// sshStatusHandler returns the effective sshd configuration for the keys the
// panel manages. Gated by "security.read".
func sshStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eff, err := d.SSH.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"effective": eff, "available": d.SSH.Available()})
	}
}

// hardenSSHHandler applies the sshd hardening (key-only, port, root login,
// auth-try budget). Gated by "security.write".
func hardenSSHHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Start from the hardened defaults so an omitted field keeps a secure
		// value (e.g. leaving out pubkey_authentication does not silently disable
		// key auth) rather than JSON's false-for-absent.
		in := security.DefaultSSHOptions()
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "ssh_port", strconv.Itoa(in.Port))
		audit.AddDetail(r.Context(), "permit_root_login", in.PermitRootLogin)
		eff, err := d.SSH.Harden(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "effective": eff})
	}
}

// updatesStatusHandler reports the effective automatic-update state. Gated by
// "security.read".
func updatesStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.Updates.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"status": st, "available": d.Updates.Available()})
	}
}

// configureUpdatesHandler applies the automatic-update policy. Gated by
// "security.write".
func configureUpdatesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in := security.DefaultUpdatesOptions()
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "auto_updates_enabled", strconv.FormatBool(in.Enabled))
		st, err := d.Updates.Configure(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "status": st})
	}
}

// auditScanHandler runs a host audit scanner (rkhunter | lynis, from the path).
// Gated by "security.read" (a read of the host's audit state).
func auditScanHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tool := chi.URLParam(r, "tool")
		audit.AddDetail(r.Context(), "audit_tool", tool)
		res, err := d.AuditScan.Scan(r.Context(), tool)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, res)
	}
}

// fimStatusHandler reports whether a FIM baseline exists. Gated by
// "security.read".
func fimStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := d.FIM.Status(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"status": st, "available": d.FIM.Available()})
	}
}

// fimInitHandler builds (or rebuilds) the FIM baseline. Gated by
// "security.write".
func fimInitHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The scope is optional; a bare init (no body) keeps the panel default.
		var in struct {
			Scope string `json:"scope"`
		}
		if r.ContentLength != 0 {
			if !decodeJSON(w, r, &in) {
				return
			}
		}
		audit.AddDetail(r.Context(), "scope", in.Scope)
		applied, err := d.FIM.Init(r.Context(), in.Scope)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "initialised": true, "scope": applied})
	}
}

// fimCheckHandler compares the filesystem against the FIM baseline. Gated by
// "security.read" (a read of the current integrity state).
func fimCheckHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := d.FIM.Check(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, res)
	}
}

// fail2banActionHandler bans or unbans an IP (action from the path). Gated by
// "security.write".
func fail2banActionHandler(d Deps, unban bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Jail string `json:"jail"`
			IP   string `json:"ip"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "jail", in.Jail)
		audit.AddDetail(r.Context(), "ip", in.IP)
		var err error
		if unban {
			err = d.Fail2Ban.Unban(r.Context(), in.Jail, in.IP)
		} else {
			err = d.Fail2Ban.Ban(r.Context(), in.Jail, in.IP)
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
