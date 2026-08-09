package auth

import (
	"context"
	"time"

	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// ImpersonationTTL bounds an impersonation session. It is deliberately short: an
// impersonation session is a standing grant to act as someone else, and should
// not outlive the task it was opened for. When it expires the admin is dropped
// back to the login screen, not silently returned to their own identity.
const ImpersonationTTL = 30 * time.Minute

// Impersonation is the result of starting an impersonation session.
type Impersonation struct {
	SessionToken string
	Target       *Principal
}

// StartImpersonation mints a session that acts as the target user (by UID) while
// stamping actorUserID as the accountable admin behind it. The caller owns the
// permission gate (user.impersonate) and the no-nesting check (a principal that
// is already impersonating must not start another); this method enforces the
// invariants that keep impersonation from becoming a privilege escalation:
//
//   - you cannot impersonate yourself;
//   - the target must be active;
//   - a superuser can never be impersonated — doing so would hand the actor the
//     "*" permission, turning a limited operator into a full admin.
//
// The minted session carries the target's own permission set, so the admin
// acts with exactly the target's rights while impersonating, never more.
func (s *Service) StartImpersonation(ctx context.Context, actorUserID int64, targetUID, ip, userAgent string) (Impersonation, error) {
	target, err := s.users.GetByUID(ctx, targetUID)
	if err != nil {
		return Impersonation{}, err
	}
	if target.ID == actorUserID {
		return Impersonation{}, errx.Validation("cannot_impersonate_self", "You cannot impersonate your own account.")
	}
	if target.Status != "active" {
		return Impersonation{}, errx.Validation("target_inactive", "You can only impersonate an active user.")
	}
	targetSuper, err := s.rbac.UserHoldsWildcard(ctx, target.ID)
	if err != nil {
		return Impersonation{}, err
	}
	if targetSuper {
		return Impersonation{}, errx.Forbidden("cannot_impersonate_superuser", "Administrators cannot be impersonated.")
	}

	token, err := newSessionToken()
	if err != nil {
		return Impersonation{}, errx.Internal(err)
	}
	if err := s.sessions.Create(ctx, &repository.Session{
		UserID:             target.ID,
		ImpersonatorUserID: actorUserID,
		TokenHash:          hashToken(token),
		IP:                 ip,
		UserAgent:          userAgent,
		ExpiresAt:          s.now().Add(ImpersonationTTL),
	}); err != nil {
		return Impersonation{}, err
	}
	perms, err := s.rbac.PermissionsForUser(ctx, target.ID)
	if err != nil {
		return Impersonation{}, err
	}
	return Impersonation{
		SessionToken: token,
		Target: &Principal{
			UserID:      target.ID,
			UserUID:     target.UID,
			Email:       target.Email,
			Username:    target.Username,
			DisplayName: target.DisplayName,
			Kind:        KindUser,
			Permissions: perms,
		},
	}, nil
}

// StopImpersonation ends the impersonation session behind currentToken and
// issues a fresh session for the impersonator, so the admin lands back in their
// own identity in one step. It errors if the current session is not an
// impersonation. If the impersonator's own account is no longer active, the
// impersonation session is still ended, but no new session is issued — they must
// sign in again.
func (s *Service) StopImpersonation(ctx context.Context, currentToken, ip, userAgent string) (LoginResult, error) {
	hash := hashToken(currentToken)
	as, err := s.sessions.ActiveSessionForToken(ctx, hash, s.now())
	if err != nil {
		return LoginResult{}, err
	}
	if !as.ImpersonatorUserID.Valid || as.ImpersonatorUserID.Int64 == 0 {
		return LoginResult{}, errx.Validation("not_impersonating", "This session is not impersonating anyone.")
	}
	admin, err := s.users.GetByID(ctx, as.ImpersonatorUserID.Int64)
	if err != nil {
		return LoginResult{}, errx.Unauthorized("invalid_session", "The original administrator no longer exists.")
	}

	// End the impersonation session first (and drop its cached principal), so the
	// admin is never left holding two live identities.
	if s.cache != nil {
		_ = s.cache.Delete(ctx, principalCacheKey(hash))
	}
	if err := s.sessions.Revoke(ctx, hash, s.now()); err != nil {
		return LoginResult{}, err
	}
	if admin.Status != "active" {
		return LoginResult{}, errx.Forbidden("account_inactive",
			"Your administrator account is no longer active. Please sign in again.")
	}
	return s.issueSession(ctx, identity{admin.ID, admin.UID, admin.Email, admin.Username, admin.DisplayName}, ip, userAgent)
}
