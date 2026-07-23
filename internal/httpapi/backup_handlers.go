package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/backup"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/site"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The backup HTTP edge. Backups are site-scoped and hold everything the site
// holds, so they ride on the site permissions: listing is site.read; creating,
// configuring, restoring and deleting are site.write. Restoring **into a new
// site** provisions that site first — the same site.write act it always is.

// listBackupsHandler returns a site's backups (newest first), its policy, and
// which targets exist. Gated by "site.read".
func listBackupsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backups, cfg, err := d.Backups.List(r.Context(), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{
			"backups": backups, "config": cfg,
			"available": d.Backups.Available(), "s3": d.Backups.HasS3(),
		})
	}
}

// createBackupHandler runs a backup now. Gated by "site.write". Level empty
// picks automatically (full for a fresh chain, incremental otherwise).
func createBackupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID := chi.URLParam(r, "uid")
		var in struct {
			Level  string `json:"level"`
			Target string `json:"target"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "site", siteUID)
		audit.AddDetail(r.Context(), "level", in.Level)
		b, err := d.Backups.Create(r.Context(), siteUID, in.Level, in.Target)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, b)
	}
}

// setBackupConfigHandler stores the site's backup policy. Gated by "site.write".
func setBackupConfigHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID := chi.URLParam(r, "uid")
		var cfg backup.Config
		if !decodeJSON(w, r, &cfg) {
			return
		}
		audit.AddDetail(r.Context(), "site", siteUID)
		if err := d.Backups.SetConfig(r.Context(), siteUID, cfg); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, cfg)
	}
}

// restoreBackupHandler is the restore wizard's endpoint: it provisions a NEW
// site and replays the chosen backup's chain into it. Gated by "site.write".
//
// Restoring into a fresh site rather than over the original is deliberate: the
// original keeps serving untouched while the operator verifies the restored
// copy, and a mistaken restore destroys nothing. Promoting the restored site is
// then an explicit choice (point the domain, or suspend the old one).
func restoreBackupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Sites == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "sites_unavailable",
				"Site management is not available on this host."))
			return
		}
		siteUID, backupUID := chi.URLParam(r, "uid"), chi.URLParam(r, "bid")
		p, _ := auth.FromContext(r.Context())
		var in struct {
			Name          string `json:"name"`
			PrimaryDomain string `json:"primary_domain"`
			// DBName, when set, creates a NEW database with that name and
			// imports the backup's dump into it — the original database is
			// never touched, for the same reason the tree goes into a new site.
			DBName string `json:"db_name"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "backup", backupUID)
		audit.AddDetail(r.Context(), "restore_domain", in.PrimaryDomain)

		// 1. When a database restore is requested, stage its dump FIRST — a
		// backup with no dump, a corrupt dump, or a missing object fails here,
		// before anything has been provisioned.
		var stagedPath, stagedFile string
		if in.DBName != "" {
			if d.Databases == nil {
				writeError(w, r, errx.New(errx.KindUnavailable, "db_unavailable",
					"Database management is not available on this host."))
				return
			}
			var err error
			stagedPath, stagedFile, _, err = d.Backups.StageDBDump(r.Context(), siteUID, backupUID)
			if err != nil {
				writeError(w, r, err)
				return
			}
			audit.AddDetail(r.Context(), "restore_db", in.DBName)
		}
		discardStaged := func() {
			if stagedPath != "" {
				_ = d.Databases.DiscardExport(stagedPath)
			}
		}

		// 2. Provision the destination — an ordinary static site create.
		dest, err := d.Sites.Create(r.Context(), site.CreateInput{
			Name: in.Name, PrimaryDomain: in.PrimaryDomain, Type: site.TypeStatic, OwnerID: p.UserID,
		})
		if err != nil {
			discardStaged()
			writeError(w, r, err)
			return
		}
		// 3. Replay the chain into it.
		if err := d.Backups.Restore(r.Context(), siteUID, backupUID, dest.UID); err != nil {
			discardStaged()
			writeError(w, r, err)
			return
		}
		// 4. The database, into a NEW database. Import consumes the staged file.
		var dbInst *database.Instance
		if stagedFile != "" {
			dbInst, err = d.Databases.CreateDatabase(r.Context(), p.UserID, in.DBName)
			if err != nil {
				discardStaged()
				writeError(w, r, err)
				return
			}
			if err := d.Databases.Import(r.Context(), dbInst.UID, stagedFile); err != nil {
				writeError(w, r, err)
				return
			}
		}
		writeJSON(w, r, http.StatusCreated, struct {
			*site.Site
			Database *database.Instance `json:"database,omitempty"`
		}{dest, dbInst})
	}
}

// listPanelBackupsHandler returns the panel's own snapshots plus the active
// policy. Gated by "system.read".
func listPanelBackupsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		backups, err := d.Backups.ListPanelBackups(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		p := d.Backups.PanelPolicyView()
		writeJSON(w, r, http.StatusOK, map[string]any{
			"backups":   backups,
			"available": d.Backups.PanelAvailable(),
			"policy": map[string]any{
				"target": p.Target, "interval_hours": p.IntervalHours, "keep": p.Keep,
			},
		})
	}
}

// createPanelBackupHandler snapshots the panel now. Gated by "system.write".
func createPanelBackupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := d.Backups.CreatePanelBackup(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusCreated, b)
	}
}

// deletePanelBackupHandler removes one panel snapshot. Gated by "system.write".
func deletePanelBackupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		audit.AddDetail(r.Context(), "panel_backup", uid)
		if err := d.Backups.DeletePanelBackup(r.Context(), uid); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// deleteBackupHandler removes a backup and — because later incrementals depend
// on it — every later backup in the same chain. Gated by "site.write".
func deleteBackupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteUID, backupUID := chi.URLParam(r, "uid"), chi.URLParam(r, "bid")
		audit.AddDetail(r.Context(), "backup", backupUID)
		removed, err := d.Backups.Delete(r.Context(), siteUID, backupUID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "removed": removed})
	}
}
