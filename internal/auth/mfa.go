package auth

import (
	"context"
	"time"

	pcache "github.com/thisisnkp/heropanel/pkg/cache"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/totp"
)

// mfaChallengeTTL bounds how long a pending MFA login stays valid.
const mfaChallengeTTL = 5 * time.Minute

func mfaKey(tokenHash string) string { return "auth:mfa:" + tokenHash }

// issueMFAChallenge stores a short-lived pending-login token in the cache and
// returns its plaintext.
func (s *Service) issueMFAChallenge(ctx context.Context, userID int64) (string, error) {
	if s.cache == nil {
		return "", errx.New(errx.KindUnavailable, "mfa_unavailable", "MFA is unavailable (no cache).")
	}
	token, err := newSessionToken()
	if err != nil {
		return "", errx.Internal(err)
	}
	if err := pcache.SetJSON(ctx, s.cache, mfaKey(hashToken(token)), userID, mfaChallengeTTL); err != nil {
		return "", errx.Internal(err)
	}
	return token, nil
}

// CompleteMFA verifies a TOTP code against a pending login and, on success,
// issues the session.
func (s *Service) CompleteMFA(ctx context.Context, mfaToken, code, ip, userAgent string) (LoginResult, error) {
	if s.cache == nil {
		return LoginResult{}, errx.New(errx.KindUnavailable, "mfa_unavailable", "MFA is unavailable.")
	}
	key := mfaKey(hashToken(mfaToken))
	userID, ok, _ := pcache.GetJSON[int64](ctx, s.cache, key)
	if !ok {
		return LoginResult{}, errx.Unauthorized("mfa_expired", "The MFA challenge expired. Please log in again.")
	}

	secret, enabled, err := s.users.GetTOTP(ctx, userID)
	if err != nil || !enabled || secret == "" {
		return LoginResult{}, errx.Unauthorized("mfa_not_enabled", "MFA is not enabled for this account.")
	}
	if !totp.Validate(secret, code) {
		return LoginResult{}, errx.Unauthorized("invalid_code", "Invalid authentication code.")
	}
	_ = s.cache.Delete(ctx, key)

	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return LoginResult{}, errx.Unauthorized("mfa_expired", "The MFA challenge is no longer valid.")
	}
	return s.issueSession(ctx, identity{u.ID, u.UID, u.Email, u.Username, u.DisplayName}, ip, userAgent)
}

// SetupMFA generates a new secret (not yet enabled) and returns the secret plus
// the otpauth:// provisioning URI for QR display.
func (s *Service) SetupMFA(ctx context.Context, userID int64) (secret, uri string, err error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	secret, err = totp.GenerateSecret()
	if err != nil {
		return "", "", errx.Internal(err)
	}
	if err := s.users.SetTOTP(ctx, userID, secret, false); err != nil {
		return "", "", err
	}
	return secret, totp.ProvisioningURI(secret, u.Email, "HeroPanel"), nil
}

// EnableMFA turns on MFA after verifying a code against the pending secret.
func (s *Service) EnableMFA(ctx context.Context, userID int64, code string) error {
	secret, _, err := s.users.GetTOTP(ctx, userID)
	if err != nil {
		return err
	}
	if secret == "" {
		return errx.Validation("mfa_not_setup", "Set up MFA before enabling it.")
	}
	if !totp.Validate(secret, code) {
		return errx.Validation("invalid_code", "Invalid authentication code.")
	}
	return s.users.SetTOTPEnabled(ctx, userID, true)
}

// DisableMFA turns off MFA and clears the secret, after verifying a code.
func (s *Service) DisableMFA(ctx context.Context, userID int64, code string) error {
	secret, enabled, err := s.users.GetTOTP(ctx, userID)
	if err != nil {
		return err
	}
	if !enabled {
		return nil // already off
	}
	if !totp.Validate(secret, code) {
		return errx.Validation("invalid_code", "Invalid authentication code.")
	}
	return s.users.SetTOTP(ctx, userID, "", false)
}
