package users

import (
	"context"
	"strings"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Role and permission catalog management. System roles (admin/reseller/…) are
// structural: their names/descriptions may be edited but their permission sets
// are fixed, so a custom policy can never neuter the admin role. Custom roles
// are fully editable and deletable.

// ListRoles returns every role with its permission set.
func (s *Service) ListRoles(ctx context.Context) ([]RoleView, error) {
	roles, err := s.rbac.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleView, 0, len(roles))
	for _, r := range roles {
		perms, err := s.rbac.PermissionsForRole(ctx, r.Slug)
		if err != nil {
			return nil, err
		}
		if perms == nil {
			perms = []string{}
		}
		out = append(out, RoleView{
			UID: r.UID, Slug: r.Slug, Name: r.Name, Description: r.Description,
			System: r.IsSystem != 0, Permissions: perms,
		})
	}
	return out, nil
}

// ListPermissions returns the permission catalog.
func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := s.rbac.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Permission, 0, len(rows))
	for _, p := range rows {
		out = append(out, Permission{Slug: p.Slug, Resource: p.Resource, Action: p.Action, Description: p.Description})
	}
	return out, nil
}

// CreateRoleInput creates a custom role.
type CreateRoleInput struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// CreateRole creates a custom (non-system) role with the given permissions.
func (s *Service) CreateRole(ctx context.Context, in CreateRoleInput) (*RoleView, error) {
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	in.Name = strings.TrimSpace(in.Name)
	if !reRoleSlug.MatchString(in.Slug) {
		return nil, errx.Validation("invalid_slug", "Role slug is 2–32 lowercase letters, digits, dash or underscore.")
	}
	if in.Name == "" {
		return nil, errx.Validation("invalid_name", "A role name is required.")
	}
	if err := s.validatePermissions(ctx, in.Permissions); err != nil {
		return nil, err
	}
	// A custom role must never mint a second superuser class by holding the
	// wildcard — admin is the one superuser role.
	if containsWildcard(in.Permissions) {
		return nil, errx.Validation("wildcard_forbidden", "A custom role cannot hold the full-access permission.")
	}
	if err := s.rbac.CreateCustomRole(ctx, in.Slug, in.Name, in.Description); err != nil {
		return nil, err
	}
	if len(in.Permissions) > 0 {
		if err := s.rbac.SetRolePermissions(ctx, in.Slug, in.Permissions); err != nil {
			return nil, err
		}
	}
	return s.roleView(ctx, in.Slug)
}

// UpdateRoleInput edits a role.
type UpdateRoleInput struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions *[]string `json:"permissions"` // nil = leave permissions unchanged
}

// UpdateRole edits a role's name/description and (for custom roles only) its
// permission set.
func (s *Service) UpdateRole(ctx context.Context, slug string, in UpdateRoleInput) (*RoleView, error) {
	role, err := s.rbac.GetRole(ctx, slug)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = role.Name
	}
	if err := s.rbac.UpdateRoleMeta(ctx, slug, name, in.Description); err != nil {
		return nil, err
	}
	if in.Permissions != nil {
		if role.IsSystem != 0 {
			return nil, errx.Validation("system_role_locked",
				"A system role's permissions are fixed; create a custom role instead.")
		}
		if containsWildcard(*in.Permissions) {
			return nil, errx.Validation("wildcard_forbidden", "A custom role cannot hold the full-access permission.")
		}
		if err := s.validatePermissions(ctx, *in.Permissions); err != nil {
			return nil, err
		}
		if err := s.rbac.SetRolePermissions(ctx, slug, *in.Permissions); err != nil {
			return nil, err
		}
	}
	return s.roleView(ctx, slug)
}

// DeleteRole removes a custom role (system roles cannot be deleted). Assignments
// and grants fall away via ON DELETE CASCADE.
func (s *Service) DeleteRole(ctx context.Context, slug string) error {
	role, err := s.rbac.GetRole(ctx, slug)
	if err != nil {
		return err
	}
	if role.IsSystem != 0 {
		return errx.Validation("system_role_locked", "A system role cannot be deleted.")
	}
	return s.rbac.DeleteRole(ctx, slug)
}

func (s *Service) roleView(ctx context.Context, slug string) (*RoleView, error) {
	role, err := s.rbac.GetRole(ctx, slug)
	if err != nil {
		return nil, err
	}
	perms, err := s.rbac.PermissionsForRole(ctx, slug)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []string{}
	}
	return &RoleView{
		UID: role.UID, Slug: role.Slug, Name: role.Name, Description: role.Description,
		System: role.IsSystem != 0, Permissions: perms,
	}, nil
}

func (s *Service) validatePermissions(ctx context.Context, perms []string) error {
	if len(perms) == 0 {
		return nil
	}
	known, err := s.rbac.ListPermissions(ctx)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, p := range known {
		set[p.Slug] = true
	}
	for _, p := range perms {
		if !set[p] {
			return errx.Validation("permission_not_found", "No such permission: "+p)
		}
	}
	return nil
}

func containsWildcard(perms []string) bool {
	for _, p := range perms {
		if p == "*" {
			return true
		}
	}
	return false
}
