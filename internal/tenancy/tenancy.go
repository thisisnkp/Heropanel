// Package tenancy answers one question for the rest of the panel: given the
// principal behind a request, which owners' resources may they see and act on?
//
// The model is an ownership tree over users (repository.parent_user_id): a
// reseller owns their clients, who own their sites/databases/zones/etc. Every
// resource carries an owner_id, so isolation reduces to a membership test —
// "is this resource's owner inside my subtree?" — with one exception the caller
// makes, not this package: a superuser (the "*" permission) sees everything and
// is never asked.
//
// Keeping this in one place means every resource enforces tenancy the same way,
// and the rule can be reasoned about (and audited) in isolation from the dozens
// of handlers that depend on it.
package tenancy

import (
	"context"

	"github.com/thisisnkp/heropanel/internal/repository"
)

// Resolver decides tenant visibility from the ownership tree.
type Resolver struct {
	users  *repository.UserRepository
	owners *repository.ResourceOwnerStore
}

// NewResolver constructs a Resolver over the user repository and the resource
// owner store. owners may be nil in wiring-only contexts (Enabled reports false
// then, and no resource is scoped).
func NewResolver(users *repository.UserRepository, owners *repository.ResourceOwnerStore) *Resolver {
	return &Resolver{users: users, owners: owners}
}

// OwnerOf resolves an owned resource's uid to its owner id (see
// repository.ResourceOwnerStore). Used by the edge to find whose tenant a
// uid-addressed resource belongs to before deciding access.
func (r *Resolver) OwnerOf(ctx context.Context, kind repository.ResourceKind, uid string) (int64, error) {
	return r.owners.OwnerOf(ctx, kind, uid)
}

// Enabled reports whether the resolver can actually resolve tenancy (it has a
// user repository). A resolver without one exists only to satisfy wiring checks
// and imposes no scoping — callers use this to skip enforcement entirely.
func (r *Resolver) Enabled() bool {
	return r != nil && r.users != nil
}

// CanAccessOwner reports whether the principal may act on a resource owned by
// ownerID: true when the principal owns it directly or ownerID is somewhere in
// the principal's subtree. An unowned resource (ownerID 0) is visible to no
// tenant — only a superuser, who is never routed here.
//
// A nil Resolver allows everything: tenancy is an added constraint, and a panel
// wired without it must behave exactly as it did before (admins-only), not fail
// closed on every request.
func (r *Resolver) CanAccessOwner(ctx context.Context, principalUserID, ownerID int64) (bool, error) {
	if r == nil || r.users == nil {
		return true, nil
	}
	return r.users.IsDescendantOrSelf(ctx, principalUserID, ownerID)
}

// VisibleOwnerIDs returns the owner ids the principal may see: themselves plus
// their whole subtree. Used to scope list queries. A nil Resolver returns just
// the principal, which is the safe fallback for a list.
func (r *Resolver) VisibleOwnerIDs(ctx context.Context, principalUserID int64) ([]int64, error) {
	if r == nil || r.users == nil {
		return []int64{principalUserID}, nil
	}
	return r.users.VisibleOwnerIDs(ctx, principalUserID)
}
