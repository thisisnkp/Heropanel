// Package auth implements authentication (Argon2id passwords, server-side
// sessions) and authorization (RBAC permission checks). It is the first
// consumer of both the repository and cache layers. See docs/05.
package auth

import "context"

// wildcard is the superuser permission: a principal holding it passes any check.
const wildcard = "*"

// Kind identifies how a principal authenticated. The audit log records it, so a
// human session and a long-lived programmatic key are never conflated — "the
// admin deleted the database" and "a CI key deleted the database" are different
// events with different follow-ups.
type Kind string

const (
	KindUser   Kind = "user"
	KindAPIKey Kind = "apikey"
)

// Principal is the authenticated identity attached to a request. It is safe to
// cache (JSON-serializable) for the short lifetime of a session lookup.
type Principal struct {
	UserID      int64    `json:"user_id"`
	UserUID     string   `json:"user_uid"`
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Kind        Kind     `json:"kind"`
	Permissions []string `json:"permissions"`
}

// Can reports whether the principal holds permission (or the "*" superuser
// permission).
func (p *Principal) Can(permission string) bool {
	for _, granted := range p.Permissions {
		if granted == wildcard || granted == permission {
			return true
		}
	}
	return false
}

// ── request-context plumbing ────────────────────────────────────────────────

type ctxKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal on ctx, if any.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok && p != nil
}
