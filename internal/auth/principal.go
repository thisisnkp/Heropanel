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
//
// During impersonation the principal is that of the *target* user — same
// UserID, same permission set — so the acting admin genuinely operates with the
// target's rights, never more. The Impersonator* fields name the real human
// behind the session; they are what the audit log attributes every mutation to,
// so an impersonated action is never mistaken for the target acting alone.
type Principal struct {
	UserID      int64    `json:"user_id"`
	UserUID     string   `json:"user_uid"`
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Kind        Kind     `json:"kind"`
	Permissions []string `json:"permissions"`

	// Impersonation context (zero/empty when the session is an ordinary self
	// session).
	ImpersonatorUserID int64  `json:"impersonator_user_id,omitempty"`
	ImpersonatorUID    string `json:"impersonator_uid,omitempty"`
	ImpersonatorEmail  string `json:"impersonator_email,omitempty"`
}

// Impersonated reports whether this principal is an admin acting as another user.
func (p *Principal) Impersonated() bool { return p != nil && p.ImpersonatorUserID != 0 }

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
