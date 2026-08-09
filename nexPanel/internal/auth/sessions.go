package auth

import "context"

// Session management: a signed-in user can see their own active sessions (one
// per device/login) and revoke any of them, or "sign out everywhere else". The
// caller's current session is marked so the UI never lets someone cut the
// branch they are sitting on by accident.
//
// Note on the principal cache: a revoked session whose principal is already
// cached stays usable until the cache entry expires (PrincipalCacheTTL, 30s by
// default), because the plaintext tokens of *other* sessions are not knowable
// here to evict them. The short TTL bounds that window; the caller's own
// session is evicted immediately on revoke.

// SessionView is the API view of an active session (never carries the token).
type SessionView struct {
	UID       string `json:"uid"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	Current   bool   `json:"current"`
}

// ListSessions returns the caller's active sessions, marking the one that
// belongs to currentToken.
func (s *Service) ListSessions(ctx context.Context, userID int64, currentToken string) ([]SessionView, error) {
	rows, err := s.sessions.ListActiveByUser(ctx, userID, s.now())
	if err != nil {
		return nil, err
	}
	curHash := hashToken(currentToken)
	out := make([]SessionView, len(rows))
	for i, r := range rows {
		out[i] = SessionView{
			UID: r.UID, IP: r.IP, UserAgent: r.UserAgent,
			CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
			Current: r.TokenHash == curHash,
		}
	}
	return out, nil
}

// RevokeSession revokes one of the caller's sessions by UID.
func (s *Service) RevokeSession(ctx context.Context, userID int64, uid string) error {
	return s.sessions.RevokeByUIDForUser(ctx, userID, uid, s.now())
}

// RevokeOtherSessions signs the caller out of every session except the current
// one. Returns how many were revoked.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID int64, currentToken string) (int64, error) {
	return s.sessions.RevokeAllForUserExcept(ctx, userID, hashToken(currentToken), s.now())
}
