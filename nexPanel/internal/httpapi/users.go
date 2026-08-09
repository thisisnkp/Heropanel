package httpapi

import (
	"context"
	"net/http"
)

// UserSummary is the API view of a user. It intentionally excludes sensitive
// fields (password hash, MFA secrets).
type UserSummary struct {
	UID         string `json:"uid"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

// UserDirectory lists users for admin views. Implemented by an adapter in the
// composition root so httpapi does not depend on the repository package.
type UserDirectory interface {
	List(ctx context.Context, limit, offset int) ([]UserSummary, error)
}

// listUsersHandler returns a page of users. Gated by the "user.read" permission.
func listUsersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := d.Users.List(r.Context(), 50, 0)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if users == nil {
			users = []UserSummary{}
		}
		writeJSON(w, r, http.StatusOK, users)
	}
}
