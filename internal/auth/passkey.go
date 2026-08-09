package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/thisisnkp/nexpanel/internal/auth/webauthn"
	"github.com/thisisnkp/nexpanel/internal/repository"
	pcache "github.com/thisisnkp/nexpanel/pkg/cache"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Passkeys (WebAuthn). Registration happens while signed in (like MFA setup);
// login is passwordless — the operator enters their email, signs the server's
// challenge with their authenticator, and gets a session. The signature is the
// only credential that crosses the wire; there is no shared secret to phish.

const passkeyChallengeTTL = 5 * time.Minute

var b64url = base64.RawURLEncoding

// WithWebAuthn enables passkeys. A nil verifier (no RP-ID configured) leaves
// them unavailable rather than guessing the relying-party identity, which must
// match the panel's domain exactly.
func (s *Service) WithWebAuthn(repo *repository.WebAuthnRepository, v *webauthn.Verifier) *Service {
	s.passkeys = repo
	s.webauthn = v
	return s
}

// PasskeysEnabled reports whether the passkey ceremonies can run.
func (s *Service) PasskeysEnabled() bool {
	return s.passkeys != nil && s.webauthn != nil && s.cache != nil
}

func (s *Service) requirePasskeys() error {
	if s.PasskeysEnabled() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "passkeys_unavailable",
		"Passkeys are not configured on this panel (needs webauthn.rp_id and a cache).")
}

// regChallengeKey / loginChallenge storage.
func regKey(userID int64) string   { return "auth:wa:reg:" + itoa(userID) }
func loginKey(token string) string { return "auth:wa:login:" + token }

// loginChallenge is the pending assertion state stored under a login token.
type loginChallenge struct {
	UserID    int64  `json:"u"`
	Challenge []byte `json:"c"`
}

// Passkey is the API view of a registered credential.
type Passkey struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// BeginPasskeyRegistration issues creation options for the signed-in user.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, userID int64) (*webauthn.CreationOptions, error) {
	if err := s.requirePasskeys(); err != nil {
		return nil, err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	existing, err := s.passkeys.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	exclude := make([][]byte, 0, len(existing))
	for _, c := range existing {
		if raw, err := b64url.DecodeString(c.CredentialID); err == nil {
			exclude = append(exclude, raw)
		}
	}
	challenge, err := randomChallenge()
	if err != nil {
		return nil, err
	}
	if err := pcache.SetJSON(ctx, s.cache, regKey(userID), challenge, passkeyChallengeTTL); err != nil {
		return nil, errx.Internal(err)
	}
	// The WebAuthn user handle is the account UID (stable, non-PII-ish).
	return s.webauthn.RegistrationOptions([]byte(u.UID), u.Email, displayOrEmail(u), challenge, exclude), nil
}

// FinishPasskeyRegistration verifies the attestation and stores the credential.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, userID int64, name string, credentialID, clientDataJSON, attestationObject []byte) (*Passkey, error) {
	if err := s.requirePasskeys(); err != nil {
		return nil, err
	}
	challenge, ok, _ := pcache.GetJSON[[]byte](ctx, s.cache, regKey(userID))
	if !ok {
		return nil, errx.Unauthorized("challenge_expired", "The registration challenge expired. Please try again.")
	}
	_ = s.cache.Delete(ctx, regKey(userID))

	cred, err := s.webauthn.FinishRegistration(challenge, clientDataJSON, attestationObject)
	if err != nil {
		return nil, errx.Validation("passkey_invalid", "The passkey could not be registered.")
	}
	rec := &repository.WebAuthnCredential{
		UserID:       userID,
		CredentialID: b64url.EncodeToString(cred.ID),
		PublicKey:    base64.StdEncoding.EncodeToString(cred.COSEKey),
		SignCount:    int64(cred.SignCount),
		Name:         sanitizeName(name),
	}
	if err := s.passkeys.Insert(ctx, rec); err != nil {
		return nil, err
	}
	return &Passkey{UID: rec.UID, Name: rec.Name, CreatedAt: rec.CreatedAt}, nil
}

// ListPasskeys returns a user's registered passkeys.
func (s *Service) ListPasskeys(ctx context.Context, userID int64) ([]Passkey, error) {
	if s.passkeys == nil {
		return []Passkey{}, nil
	}
	recs, err := s.passkeys.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Passkey, len(recs))
	for i, c := range recs {
		out[i] = Passkey{UID: c.UID, Name: c.Name, CreatedAt: c.CreatedAt}
	}
	return out, nil
}

// DeletePasskey removes one of the user's passkeys.
func (s *Service) DeletePasskey(ctx context.Context, userID int64, uid string) error {
	if err := s.requirePasskeys(); err != nil {
		return err
	}
	return s.passkeys.Delete(ctx, uid, userID)
}

// BeginPasskeyLogin issues assertion options for a user identified by email,
// plus an opaque login token that carries the challenge to the finish step.
// An unknown email still returns options against a random challenge and no
// credentials — the response shape does not reveal whether the account exists.
func (s *Service) BeginPasskeyLogin(ctx context.Context, email string) (*webauthn.RequestOptions, string, error) {
	if err := s.requirePasskeys(); err != nil {
		return nil, "", err
	}
	challenge, err := randomChallenge()
	if err != nil {
		return nil, "", err
	}
	token, err := newSessionToken()
	if err != nil {
		return nil, "", errx.Internal(err)
	}

	var allow [][]byte
	var userID int64
	if au, err := s.users.GetAuthByEmail(ctx, email, s.now()); err == nil && au.Status == "active" {
		userID = au.ID
		if creds, err := s.passkeys.ListByUser(ctx, au.ID); err == nil {
			for _, c := range creds {
				if raw, err := b64url.DecodeString(c.CredentialID); err == nil {
					allow = append(allow, raw)
				}
			}
		}
	}
	// Store the challenge under the token regardless, so timing/shape is uniform.
	if err := pcache.SetJSON(ctx, s.cache, loginKey(token), loginChallenge{UserID: userID, Challenge: challenge}, passkeyChallengeTTL); err != nil {
		return nil, "", errx.Internal(err)
	}
	return s.webauthn.AssertionOptions(challenge, allow), token, nil
}

// FinishPasskeyLogin verifies an assertion and issues a session.
func (s *Service) FinishPasskeyLogin(ctx context.Context, loginToken string, credentialID, clientDataJSON, authenticatorData, signature []byte, ip, userAgent string) (LoginResult, error) {
	if err := s.requirePasskeys(); err != nil {
		return LoginResult{}, err
	}
	pending, ok, _ := pcache.GetJSON[loginChallenge](ctx, s.cache, loginKey(loginToken))
	if !ok || pending.UserID == 0 {
		return LoginResult{}, errx.Unauthorized("passkey_failed", "Passkey login failed.")
	}
	_ = s.cache.Delete(ctx, loginKey(loginToken))

	rec, err := s.passkeys.GetByCredentialID(ctx, b64url.EncodeToString(credentialID))
	if err != nil || rec.UserID != pending.UserID {
		return LoginResult{}, errx.Unauthorized("passkey_failed", "Passkey login failed.")
	}
	coseKey, err := base64.StdEncoding.DecodeString(rec.PublicKey)
	if err != nil {
		return LoginResult{}, errx.Unauthorized("passkey_failed", "Passkey login failed.")
	}
	cred := &webauthn.Credential{ID: credentialID, COSEKey: coseKey, SignCount: uint32(rec.SignCount)}
	newCount, err := s.webauthn.FinishAssertion(cred, pending.Challenge, clientDataJSON, authenticatorData, signature)
	if err != nil {
		return LoginResult{}, errx.Unauthorized("passkey_failed", "Passkey login failed.")
	}
	_ = s.passkeys.UpdateSignCount(ctx, rec.UID, int64(newCount))

	u, err := s.users.GetByID(ctx, rec.UserID)
	if err != nil {
		return LoginResult{}, errx.Unauthorized("passkey_failed", "Passkey login failed.")
	}
	if err := s.users.RegisterSuccessfulLogin(ctx, u.ID, ip, s.now()); err != nil {
		return LoginResult{}, err
	}
	return s.issueSession(ctx, identity{u.ID, u.UID, u.Email, u.Username, u.DisplayName}, ip, userAgent)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func randomChallenge() ([]byte, error) {
	c := make([]byte, 32)
	if _, err := rand.Read(c); err != nil {
		return nil, errx.Internal(err)
	}
	return c, nil
}

func displayOrEmail(u *repository.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

// sanitizeName bounds a passkey label to something printable.
func sanitizeName(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "Passkey"
	}
	return string(out)
}

// itoa avoids importing strconv for one small use in a cache key.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
