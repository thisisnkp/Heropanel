package auth

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/internal/repository"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/pwhash"
)

// Config tunes auth behavior.
type Config struct {
	SessionTTL        time.Duration
	LockThreshold     int           // failed logins before lockout
	LockDuration      time.Duration // how long an account stays locked
	PrincipalCacheTTL time.Duration // how long a resolved principal is cached
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		SessionTTL:        24 * time.Hour,
		LockThreshold:     5,
		LockDuration:      15 * time.Minute,
		PrincipalCacheTTL: 30 * time.Second,
	}
}

// Service is the authentication/authorization service.
type Service struct {
	users     *repository.UserRepository
	sessions  *repository.SessionRepository
	rbac      *repository.RBACRepository
	cache     pcache.Cache
	cfg       Config
	now       func() time.Time
	dummyHash string // for constant-ish timing on unknown users
}

// NewService constructs the auth Service. cache may be nil.
func NewService(users *repository.UserRepository, sessions *repository.SessionRepository, rbac *repository.RBACRepository, cache pcache.Cache, cfg Config) *Service {
	dummy, _ := pwhash.Hash("heropanel-timing-decoy")
	return &Service{
		users:     users,
		sessions:  sessions,
		rbac:      rbac,
		cache:     cache,
		cfg:       cfg,
		now:       time.Now,
		dummyHash: dummy,
	}
}

func invalidCredentials() error {
	return errx.Unauthorized("invalid_credentials", "Invalid email or password.")
}

func principalCacheKey(tokenHash string) string { return "auth:principal:" + tokenHash }

// Login verifies credentials and, on success, creates a session and returns the
// raw session token plus the resolved principal.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (string, *Principal, error) {
	now := s.now()

	au, err := s.users.GetAuthByEmail(ctx, email, now)
	if err != nil {
		if errx.IsKind(err, errx.KindNotFound) {
			// Verify against a decoy to blunt user-enumeration timing signals.
			_, _ = pwhash.Verify(password, s.dummyHash)
			return "", nil, invalidCredentials()
		}
		return "", nil, err
	}
	if au.Locked == 1 {
		return "", nil, errx.Forbidden("account_locked", "Account is temporarily locked. Try again later.")
	}
	if au.Status != "active" {
		return "", nil, errx.Forbidden("account_inactive", "This account is not active.")
	}
	if !au.PasswordHash.Valid {
		return "", nil, invalidCredentials()
	}

	ok, err := pwhash.Verify(password, au.PasswordHash.String)
	if err != nil || !ok {
		_ = s.users.RegisterFailedLogin(ctx, au.ID, s.cfg.LockThreshold, now.Add(s.cfg.LockDuration))
		return "", nil, invalidCredentials()
	}
	if err := s.users.RegisterSuccessfulLogin(ctx, au.ID, ip, now); err != nil {
		return "", nil, err
	}

	token, err := newSessionToken()
	if err != nil {
		return "", nil, errx.Internal(err)
	}
	sess := &repository.Session{
		UserID:    au.ID,
		TokenHash: hashToken(token),
		IP:        ip,
		UserAgent: userAgent,
		ExpiresAt: now.Add(s.cfg.SessionTTL),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return "", nil, err
	}

	perms, err := s.rbac.PermissionsForUser(ctx, au.ID)
	if err != nil {
		return "", nil, err
	}
	return token, &Principal{
		UserID:      au.ID,
		UserUID:     au.UID,
		Email:       au.Email,
		Username:    au.Username,
		DisplayName: au.DisplayName,
		Permissions: perms,
	}, nil
}

// Authenticate resolves the principal for a session token, using the cache to
// avoid a DB hit on every request. Returns an unauthorized error for invalid or
// expired sessions.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, error) {
	hash := hashToken(token)

	if s.cache != nil {
		if p, ok, _ := pcache.GetJSON[Principal](ctx, s.cache, principalCacheKey(hash)); ok {
			return &p, nil
		}
	}

	userID, err := s.sessions.UserIDForActiveToken(ctx, hash, s.now())
	if err != nil {
		return nil, err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, errx.Unauthorized("invalid_session", "Session is no longer valid.")
	}
	if u.Status != "active" {
		return nil, errx.Forbidden("account_inactive", "This account is not active.")
	}
	perms, err := s.rbac.PermissionsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	p := &Principal{
		UserID:      u.ID,
		UserUID:     u.UID,
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Permissions: perms,
	}
	if s.cache != nil {
		_ = pcache.SetJSON(ctx, s.cache, principalCacheKey(hash), *p, s.cfg.PrincipalCacheTTL)
	}
	return p, nil
}

// Logout revokes the session for token and drops its cached principal.
func (s *Service) Logout(ctx context.Context, token string) error {
	hash := hashToken(token)
	if s.cache != nil {
		_ = s.cache.Delete(ctx, principalCacheKey(hash))
	}
	return s.sessions.Revoke(ctx, hash, s.now())
}

// Bootstrap creates the first administrator. It only succeeds when no users
// exist yet (first-run flow).
func (s *Service) Bootstrap(ctx context.Context, email, username, password string) (*Principal, error) {
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if strings.TrimSpace(username) == "" {
		return nil, errx.Validation("invalid_username", "Username is required.")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	n, err := s.users.Count(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errx.Conflict("already_bootstrapped", "An administrator already exists.")
	}

	hash, err := pwhash.Hash(password)
	if err != nil {
		return nil, errx.Internal(err)
	}
	u := &repository.User{
		Email:        email,
		Username:     username,
		DisplayName:  username,
		PasswordHash: sql.NullString{String: hash, Valid: true},
		Status:       "active",
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}
	if err := s.rbac.AssignRole(ctx, u.ID, "admin"); err != nil {
		return nil, err
	}
	perms, err := s.rbac.PermissionsForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return &Principal{
		UserID:      u.ID,
		UserUID:     u.UID,
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Permissions: perms,
	}, nil
}

// SessionCookieMaxAge returns the session TTL in seconds (for the cookie).
func (s *Service) SessionCookieMaxAge() int { return int(s.cfg.SessionTTL.Seconds()) }

// NeedsBootstrap reports whether no users exist yet, so the UI can show the
// first-run administrator setup instead of the login screen.
func (s *Service) NeedsBootstrap(ctx context.Context) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

func validateEmail(email string) error {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || !strings.Contains(email[at:], ".") || len(email) > 255 {
		return errx.Validation("invalid_email", "A valid email address is required.")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return errx.Validation("weak_password", "Password must be at least 8 characters.")
	}
	return nil
}
