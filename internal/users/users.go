// Package users is the multi-user administration module: creating and managing
// user accounts and their role assignments, and managing the roles/permissions
// catalog itself (custom roles on top of the seeded system roles). It sits above
// the auth module (which owns authentication and session issuance) and reuses
// the same user/RBAC/session repositories.
//
// Two invariants run through every mutation:
//   - the panel can never be locked out — an action that would remove the last
//     active superuser is refused;
//   - system roles are structural — their permission sets are fixed, so a custom
//     policy can never accidentally neuter the admin role.
package users

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/pwhash"
)

var (
	reEmail    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	reUsername = regexp.MustCompile(`^[a-zA-Z0-9._-]{2,64}$`)
	reRoleSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)
)

// Service manages users and the RBAC catalog.
type Service struct {
	users    *repository.UserRepository
	rbac     *repository.RBACRepository
	sessions *repository.SessionRepository
	hash     func(string) (string, error)
	now      func() time.Time
}

// NewService constructs the management service.
func NewService(users *repository.UserRepository, rbac *repository.RBACRepository, sessions *repository.SessionRepository) *Service {
	return &Service{users: users, rbac: rbac, sessions: sessions, hash: pwhash.Hash, now: time.Now}
}

// Available reports whether the module can operate.
func (s *Service) Available() bool {
	return s != nil && s.users != nil && s.rbac != nil
}

// ── views ────────────────────────────────────────────────────────────────────

// UserView is the API view of a user with their roles and whether they are a
// superuser (the UI badges it and guards destructive actions).
type UserView struct {
	UID         string   `json:"uid"`
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Superuser   bool     `json:"superuser"`
}

// RoleView is a role with its permission set.
type RoleView struct {
	UID         string   `json:"uid"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	System      bool     `json:"system"`
	Permissions []string `json:"permissions"`
}

// Permission is one catalog entry.
type Permission struct {
	Slug        string `json:"slug"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

// CreateUserInput creates a user.
type CreateUserInput struct {
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Password    string   `json:"password"`
	Roles       []string `json:"roles"`
}

// Actor is the principal performing a management action, reduced to what the
// service needs: their id (for self and tenant checks), whether they are a
// superuser (who bypasses tenant scoping and the role-escalation guard), and
// their own permission set (the ceiling a non-superuser may grant).
type Actor struct {
	UserID      int64
	Superuser   bool
	Permissions []string
}

// assertUserInScope refuses an action on a target outside the actor's tenant
// subtree. A superuser is in scope for everyone. A miss is reported as
// not-found so a reseller cannot probe for accounts in other tenants.
func (s *Service) assertUserInScope(ctx context.Context, actor Actor, targetUserID int64) error {
	if actor.Superuser {
		return nil
	}
	ok, err := s.users.IsDescendantOrSelf(ctx, actor.UserID, targetUserID)
	if err != nil {
		return err
	}
	if !ok {
		return errx.NotFound("user_not_found", "No such user.")
	}
	return nil
}

// assertRolesWithinActor refuses to assign a role that grants any permission the
// actor does not itself hold — the escalation guard that stops a reseller from
// minting an administrator (or otherwise exceeding their own grant). Superusers
// are exempt; the wildcard is never grantable by a non-superuser.
func (s *Service) assertRolesWithinActor(ctx context.Context, actor Actor, roles []string) error {
	if actor.Superuser {
		return nil
	}
	held := make(map[string]bool, len(actor.Permissions))
	for _, p := range actor.Permissions {
		held[p] = true
	}
	for _, slug := range roles {
		perms, err := s.rbac.PermissionsForRole(ctx, slug)
		if err != nil {
			return err
		}
		for _, perm := range perms {
			if perm == "*" || !held[perm] {
				return errx.Forbidden("role_exceeds_grant",
					"You cannot assign a role with permissions you do not hold: "+slug)
			}
		}
	}
	return nil
}

// ── users ────────────────────────────────────────────────────────────────────

// ListScoped returns a page of users visible to the actor: every user for a
// superuser, or the actor's own tenant subtree (themselves plus their clients)
// otherwise.
func (s *Service) ListScoped(ctx context.Context, actor Actor, limit, offset int) ([]UserView, error) {
	var recs []repository.User
	var err error
	if actor.Superuser {
		recs, err = s.users.List(ctx, limit, offset)
	} else {
		var owners []int64
		owners, err = s.users.VisibleOwnerIDs(ctx, actor.UserID)
		if err != nil {
			return nil, err
		}
		recs, err = s.users.ListByIDs(ctx, owners, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(recs))
	for i := range recs {
		v, err := s.viewFor(ctx, &recs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// Get returns one user by UID, scoped to the actor's tenant.
func (s *Service) Get(ctx context.Context, actor Actor, uid string) (*UserView, error) {
	u, err := s.users.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := s.assertUserInScope(ctx, actor, u.ID); err != nil {
		return nil, err
	}
	return s.viewFor(ctx, u)
}

func (s *Service) viewFor(ctx context.Context, u *repository.User) (*UserView, error) {
	roles, err := s.rbac.RolesForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	super, err := s.rbac.UserHoldsWildcard(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []string{}
	}
	return &UserView{
		UID: u.UID, Email: u.Email, Username: u.Username, DisplayName: u.DisplayName,
		Status: u.Status, Roles: roles, Superuser: super,
	}, nil
}

// Create validates and creates a user, then assigns the given roles. A
// non-superuser actor may only assign roles within their own grant (escalation
// guard), and the new account is parented into the actor's tenant subtree.
func (s *Service) Create(ctx context.Context, actor Actor, in CreateUserInput) (*UserView, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !reEmail.MatchString(in.Email) {
		return nil, errx.Validation("invalid_email", "Enter a valid email address.")
	}
	if !reUsername.MatchString(in.Username) {
		return nil, errx.Validation("invalid_username", "Username is 2–64 letters, digits, dot, dash or underscore.")
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	if len(in.Password) < 8 || len(in.Password) > 200 {
		return nil, errx.Validation("invalid_password", "Password must be 8–200 characters.")
	}
	if err := s.validateRoles(ctx, in.Roles); err != nil {
		return nil, err
	}
	if err := s.assertRolesWithinActor(ctx, actor, in.Roles); err != nil {
		return nil, err
	}
	hash, err := s.hash(in.Password)
	if err != nil {
		return nil, errx.Internal(err)
	}
	u := &repository.User{Email: in.Email, Username: in.Username, DisplayName: in.DisplayName, Status: "active"}
	u.PasswordHash.String, u.PasswordHash.Valid = hash, true
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	// A reseller's new accounts belong to their tenant; an admin creates
	// top-level accounts (no parent).
	if !actor.Superuser && actor.UserID != 0 {
		if err := s.users.SetParent(ctx, u.ID, actor.UserID); err != nil {
			return nil, err
		}
	}
	if len(in.Roles) > 0 {
		if err := s.rbac.SetUserRoles(ctx, u.ID, in.Roles); err != nil {
			return nil, err
		}
	}
	return s.viewFor(ctx, u)
}

// SetStatus activates or suspends a user. Suspending revokes their sessions and
// is refused if it would remove the last active superuser or target yourself.
func (s *Service) SetStatus(ctx context.Context, actor Actor, uid, status string) (*UserView, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "active" && status != "suspended" {
		return nil, errx.Validation("invalid_status", "Status must be active or suspended.")
	}
	u, err := s.users.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := s.assertUserInScope(ctx, actor, u.ID); err != nil {
		return nil, err
	}
	if status == "suspended" {
		if u.ID == actor.UserID {
			return nil, errx.Validation("cannot_suspend_self", "You cannot suspend your own account.")
		}
		if err := s.guardLastSuperuser(ctx, u.ID); err != nil {
			return nil, err
		}
	}
	if err := s.users.SetStatus(ctx, uid, status); err != nil {
		return nil, err
	}
	if status == "suspended" {
		// End every session so the suspension takes effect at once.
		_, _ = s.sessions.RevokeAllForUserExcept(ctx, u.ID, "", s.now())
	}
	return s.Get(ctx, actor, uid)
}

// SetRoles replaces a user's roles. Refused if it would strip the last active
// superuser of their superuser role, if the target is outside the actor's
// tenant, or if the new roles exceed the actor's own grant.
func (s *Service) SetRoles(ctx context.Context, actor Actor, uid string, roles []string) (*UserView, error) {
	u, err := s.users.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := s.assertUserInScope(ctx, actor, u.ID); err != nil {
		return nil, err
	}
	if err := s.validateRoles(ctx, roles); err != nil {
		return nil, err
	}
	if err := s.assertRolesWithinActor(ctx, actor, roles); err != nil {
		return nil, err
	}
	// If the user is currently a superuser and the new set would remove that, make
	// sure another active superuser remains.
	wasSuper, err := s.rbac.UserHoldsWildcard(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if wasSuper {
		willBeSuper, err := s.rolesGrantSuperuser(ctx, roles)
		if err != nil {
			return nil, err
		}
		if !willBeSuper {
			if err := s.guardLastSuperuser(ctx, u.ID); err != nil {
				return nil, err
			}
		}
	}
	if err := s.rbac.SetUserRoles(ctx, u.ID, roles); err != nil {
		return nil, err
	}
	return s.Get(ctx, actor, uid)
}

// SetPassword resets a user's password (admin action) and ends their sessions,
// forcing a re-login with the new credential.
func (s *Service) SetPassword(ctx context.Context, actor Actor, uid, password string) error {
	if len(password) < 8 || len(password) > 200 {
		return errx.Validation("invalid_password", "Password must be 8–200 characters.")
	}
	u, err := s.users.GetByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.assertUserInScope(ctx, actor, u.ID); err != nil {
		return err
	}
	hash, err := s.hash(password)
	if err != nil {
		return errx.Internal(err)
	}
	if err := s.users.SetPasswordByUID(ctx, uid, hash); err != nil {
		return err
	}
	_, _ = s.sessions.RevokeAllForUserExcept(ctx, u.ID, "", s.now())
	return nil
}

// Delete soft-deletes a user. Refused for yourself, the last superuser, or a
// target outside the actor's tenant.
func (s *Service) Delete(ctx context.Context, actor Actor, uid string) error {
	u, err := s.users.GetByUID(ctx, uid)
	if err != nil {
		return err
	}
	if err := s.assertUserInScope(ctx, actor, u.ID); err != nil {
		return err
	}
	if u.ID == actor.UserID {
		return errx.Validation("cannot_delete_self", "You cannot delete your own account.")
	}
	if err := s.guardLastSuperuser(ctx, u.ID); err != nil {
		return err
	}
	// Free the email/username so a new account can reuse them (dialect-neutral:
	// the suffix is computed here, not in SQL).
	suffix := "#del-" + u.UID
	if err := s.users.SoftDelete(ctx, uid, u.Email+suffix, u.Username+suffix, s.now()); err != nil {
		return err
	}
	_, _ = s.sessions.RevokeAllForUserExcept(ctx, u.ID, "", s.now())
	return nil
}

// guardLastSuperuser refuses an action on a superuser when they are the only
// active one left.
func (s *Service) guardLastSuperuser(ctx context.Context, userID int64) error {
	isSuper, err := s.rbac.UserHoldsWildcard(ctx, userID)
	if err != nil {
		return err
	}
	if !isSuper {
		return nil
	}
	n, err := s.rbac.CountActiveSuperusers(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return errx.Validation("last_superuser",
			"This is the only active administrator — grant another user admin first.")
	}
	return nil
}

func (s *Service) validateRoles(ctx context.Context, roles []string) error {
	if len(roles) == 0 {
		return nil
	}
	known, err := s.rbac.ListRoles(ctx)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, r := range known {
		set[r.Slug] = true
	}
	for _, r := range roles {
		if !set[r] {
			return errx.Validation("role_not_found", "No such role: "+r)
		}
	}
	return nil
}

// rolesGrantSuperuser reports whether any of the roles holds the wildcard
// permission — checked against the actual grants, not a hard-coded slug, so a
// custom superuser role counts too.
func (s *Service) rolesGrantSuperuser(ctx context.Context, roles []string) (bool, error) {
	for _, slug := range roles {
		perms, err := s.rbac.PermissionsForRole(ctx, slug)
		if err != nil {
			return false, err
		}
		for _, p := range perms {
			if p == "*" {
				return true, nil
			}
		}
	}
	return false, nil
}
