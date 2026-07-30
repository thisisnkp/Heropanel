package repository

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Ownership tree (reseller tenancy). users.parent_user_id links a client to the
// reseller that owns them; NULL is a top-level account. A principal's "visible
// scope" is itself plus its whole subtree, and resource isolation is then a
// membership test: a resource is visible iff its owner_id is in that set.
//
// The subtree is resolved with a recursive CTE, supported by both engines
// HeroPanel targets (SQLite ≥3.8.3, MariaDB ≥10.2). Deleted users are excluded
// so an orphaned/removed account never carries visibility.

// SetParent sets (or clears, when parentID is 0) a user's owning account.
func (r *UserRepository) SetParent(ctx context.Context, childID, parentID int64) error {
	var parent any // NULL clears the parent
	if parentID != 0 {
		parent = parentID
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE users SET parent_user_id = ? WHERE id = ? AND deleted_at IS NULL`, parent, childID); err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ParentID returns a user's parent id, or 0 when they are a top-level account.
func (r *UserRepository) ParentID(ctx context.Context, userID int64) (int64, error) {
	var parent *int64
	err := r.db.GetContext(ctx, &parent,
		`SELECT parent_user_id FROM users WHERE id = ? AND deleted_at IS NULL`, userID)
	if isNoRows(err) {
		return 0, errx.NotFound("user_not_found", "No such user.")
	}
	if err != nil {
		return 0, errx.Internal(err)
	}
	if parent == nil {
		return 0, nil
	}
	return *parent, nil
}

// DescendantIDs returns every user below rootID in the ownership tree (not
// including rootID itself).
func (r *UserRepository) DescendantIDs(ctx context.Context, rootID int64) ([]int64, error) {
	var ids []int64
	err := r.db.SelectContext(ctx, &ids,
		`WITH RECURSIVE subtree(id) AS (
		    SELECT id FROM users WHERE parent_user_id = ? AND deleted_at IS NULL
		    UNION
		    SELECT u.id FROM users u
		      JOIN subtree s ON u.parent_user_id = s.id
		     WHERE u.deleted_at IS NULL
		 )
		 SELECT id FROM subtree`, rootID)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return ids, nil
}

// VisibleOwnerIDs returns rootID plus all of its descendants — the complete set
// of owner ids a principal rooted at rootID may see.
func (r *UserRepository) VisibleOwnerIDs(ctx context.Context, rootID int64) ([]int64, error) {
	desc, err := r.DescendantIDs(ctx, rootID)
	if err != nil {
		return nil, err
	}
	return append([]int64{rootID}, desc...), nil
}

// ListByIDs returns the non-deleted users whose ids are in the set, ordered by
// id — the tenant-scoped user listing. An empty set returns no users.
func (r *UserRepository) ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]User, error) {
	if len(ids) == 0 {
		return []User{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + userColumns + ` FROM users WHERE deleted_at IS NULL AND id IN (?) ORDER BY id LIMIT ? OFFSET ?`
	query, args, err := sqlx.In(q, ids, limit, offset)
	if err != nil {
		return nil, errx.Internal(err)
	}
	query = r.db.Rebind(query)
	var users []User
	if err := r.db.SelectContext(ctx, &users, query, args...); err != nil {
		return nil, errx.Internal(err)
	}
	return users, nil
}

// maxTreeDepth bounds the ancestor walk in IsDescendantOrSelf. A correct tree
// has no cycles, but a bad re-parent could introduce one; the bound turns that
// into a clean "no access" instead of an infinite loop.
const maxTreeDepth = 64

// IsDescendantOrSelf reports whether nodeID is ancestorID itself or lies
// anywhere below it in the ownership tree. It walks up from nodeID, which is
// O(depth) rather than materializing the whole subtree.
func (r *UserRepository) IsDescendantOrSelf(ctx context.Context, ancestorID, nodeID int64) (bool, error) {
	if ancestorID == 0 || nodeID == 0 {
		return false, nil
	}
	cur := nodeID
	for i := 0; cur != 0 && i < maxTreeDepth; i++ {
		if cur == ancestorID {
			return true, nil
		}
		parent, err := r.ParentID(ctx, cur)
		if err != nil {
			return false, err
		}
		cur = parent
	}
	return false, nil
}
