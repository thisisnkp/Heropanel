package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/users"
)

// The multi-user administration edge: user CRUD, role assignment, and the
// roles/permissions catalog. Reads are gated by "user.read", writes by
// "user.write". Destructive actions carry lock-out and self-target guards in the
// service, so the edge just forwards the acting principal for the self checks.

// actorFrom builds the management actor from the request principal: their id,
// whether they are a superuser (bypasses tenant scoping and the escalation
// guard), and their permission set (the ceiling a non-superuser may grant).
func actorFrom(r *http.Request) users.Actor {
	if p, ok := auth.FromContext(r.Context()); ok && p != nil {
		return users.Actor{UserID: p.UserID, Superuser: p.Can("*"), Permissions: p.Permissions}
	}
	return users.Actor{}
}

// listUsersMgmtHandler lists users with their roles, scoped to the caller's
// tenant (all users for a superuser). Gated by "user.read".
func listUsersMgmtHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.UserMgmt.ListScoped(r.Context(), actorFrom(r), 100, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if list == nil {
			list = []users.UserView{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"users": list})
	}
}

// getUserHandler returns one user in the caller's tenant. Gated by "user.read".
func getUserHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := d.UserMgmt.Get(r.Context(), actorFrom(r), chi.URLParam(r, "uid"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, v)
	}
}

// createUserHandler creates a user with roles. Gated by "user.write".
func createUserHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in users.CreateUserInput
		if !decodeJSON(w, r, &in) {
			return
		}
		v, err := d.UserMgmt.Create(r.Context(), actorFrom(r), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "user", v.Email)
		audit.AddDetail(r.Context(), "roles", v.Roles)
		writeJSON(w, r, http.StatusCreated, v)
	}
}

// setUserStatusHandler activates or suspends a user. Gated by "user.write".
func setUserStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Status string `json:"status"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "status", in.Status)
		v, err := d.UserMgmt.SetStatus(r.Context(), actorFrom(r), chi.URLParam(r, "uid"), in.Status)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, v)
	}
}

// setUserRolesHandler replaces a user's roles. Gated by "user.write".
func setUserRolesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Roles []string `json:"roles"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "roles", in.Roles)
		v, err := d.UserMgmt.SetRoles(r.Context(), actorFrom(r), chi.URLParam(r, "uid"), in.Roles)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, v)
	}
}

// setUserPasswordHandler resets a user's password. Gated by "user.write".
func setUserPasswordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		if err := d.UserMgmt.SetPassword(r.Context(), actorFrom(r), chi.URLParam(r, "uid"), in.Password); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// deleteUserHandler soft-deletes a user. Gated by "user.write".
func deleteUserHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.UserMgmt.Delete(r.Context(), actorFrom(r), chi.URLParam(r, "uid")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}

// ── roles & permissions ──────────────────────────────────────────────────────

// listRolesHandler lists roles with their permissions. Gated by "user.read".
func listRolesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roles, err := d.UserMgmt.ListRoles(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		if roles == nil {
			roles = []users.RoleView{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
	}
}

// listPermissionsHandler returns the permission catalog. Gated by "user.read".
func listPermissionsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		perms, err := d.UserMgmt.ListPermissions(r.Context())
		if err != nil {
			writeError(w, r, err)
			return
		}
		if perms == nil {
			perms = []users.Permission{}
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"permissions": perms})
	}
}

// createRoleHandler creates a custom role. Gated by "user.write".
func createRoleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in users.CreateRoleInput
		if !decodeJSON(w, r, &in) {
			return
		}
		v, err := d.UserMgmt.CreateRole(r.Context(), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		audit.AddDetail(r.Context(), "role", v.Slug)
		writeJSON(w, r, http.StatusCreated, v)
	}
}

// updateRoleHandler edits a role. Gated by "user.write".
func updateRoleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in users.UpdateRoleInput
		if !decodeJSON(w, r, &in) {
			return
		}
		audit.AddDetail(r.Context(), "role", chi.URLParam(r, "slug"))
		v, err := d.UserMgmt.UpdateRole(r.Context(), chi.URLParam(r, "slug"), in)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, v)
	}
}

// deleteRoleHandler removes a custom role. Gated by "user.write".
func deleteRoleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.UserMgmt.DeleteRole(r.Context(), chi.URLParam(r, "slug")); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true})
	}
}
