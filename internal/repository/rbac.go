package repository

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// RBACRepository resolves and seeds roles/permissions. Seeding uses
// check-then-insert (dialect-neutral) so it is idempotent across boots.
type RBACRepository struct {
	db *DB
}

// NewRBACRepository constructs an RBACRepository.
func NewRBACRepository(db *DB) *RBACRepository { return &RBACRepository{db: db} }

// PermissionsForUser returns the distinct permission slugs granted to a user via
// their roles. A user in a role holding the "*" permission is a superuser.
func (r *RBACRepository) PermissionsForUser(ctx context.Context, userID int64) ([]string, error) {
	var slugs []string
	err := r.db.SelectContext(ctx, &slugs,
		`SELECT DISTINCT p.slug
		   FROM permissions p
		   JOIN role_permissions rp ON rp.permission_id = p.id
		   JOIN user_roles ur ON ur.role_id = rp.role_id
		  WHERE ur.user_id = ?`,
		userID)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return slugs, nil
}

// EnsurePermission inserts a permission if its slug does not already exist.
func (r *RBACRepository) EnsurePermission(ctx context.Context, slug, resource, action, description string) error {
	var id int64
	err := r.db.GetContext(ctx, &id, `SELECT id FROM permissions WHERE slug = ?`, slug)
	if err == nil {
		return nil // exists
	}
	if !isNoRows(err) {
		return errx.Internal(err)
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO permissions (slug, resource, action, description) VALUES (?, ?, ?, ?)`,
		slug, resource, action, description); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// EnsureRole inserts a role if its slug does not already exist, returning its id.
func (r *RBACRepository) EnsureRole(ctx context.Context, slug, name string, isSystem bool, description string) (int64, error) {
	id, err := r.roleID(ctx, slug)
	if err == nil {
		return id, nil
	}
	if !errx.IsKind(err, errx.KindNotFound) {
		return 0, err
	}
	sys := 0
	if isSystem {
		sys = 1
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO roles (uid, name, slug, is_system, description) VALUES (?, ?, ?, ?, ?)`,
		idgen.NewULID(), name, slug, sys, description); err != nil {
		return 0, errx.Internal(err)
	}
	return r.roleID(ctx, slug)
}

// GrantPermission grants a permission (by slug) to a role (by slug) if not
// already granted.
func (r *RBACRepository) GrantPermission(ctx context.Context, roleSlug, permSlug string) error {
	roleID, err := r.roleID(ctx, roleSlug)
	if err != nil {
		return err
	}
	permID, err := r.permissionID(ctx, permSlug)
	if err != nil {
		return err
	}
	var exists int
	err = r.db.GetContext(ctx, &exists,
		`SELECT COUNT(*) FROM role_permissions WHERE role_id = ? AND permission_id = ?`, roleID, permID)
	if err != nil {
		return errx.Internal(err)
	}
	if exists > 0 {
		return nil
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)`, roleID, permID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// AssignRole assigns a role (by slug) to a user at global scope, if not already
// assigned.
func (r *RBACRepository) AssignRole(ctx context.Context, userID int64, roleSlug string) error {
	roleID, err := r.roleID(ctx, roleSlug)
	if err != nil {
		return err
	}
	var exists int
	err = r.db.GetContext(ctx, &exists,
		`SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ? AND scope_type = 'global' AND scope_id = 0`,
		userID, roleID)
	if err != nil {
		return errx.Internal(err)
	}
	if exists > 0 {
		return nil
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role_id, scope_type, scope_id) VALUES (?, ?, 'global', 0)`,
		userID, roleID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

func (r *RBACRepository) roleID(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := r.db.GetContext(ctx, &id, `SELECT id FROM roles WHERE slug = ?`, slug)
	if isNoRows(err) {
		return 0, errx.NotFound("role_not_found", "No such role.")
	}
	if err != nil {
		return 0, errx.Internal(err)
	}
	return id, nil
}

func (r *RBACRepository) permissionID(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := r.db.GetContext(ctx, &id, `SELECT id FROM permissions WHERE slug = ?`, slug)
	if isNoRows(err) {
		return 0, errx.NotFound("permission_not_found", "No such permission.")
	}
	if err != nil {
		return 0, errx.Internal(err)
	}
	return id, nil
}
