package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// newSessionToken returns a fresh, high-entropy opaque session token (URL-safe).
// The raw token is given to the client; only its hash is stored server-side.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 of a token. Session/API tokens are
// high-entropy, so a fast hash (not a password hash) is appropriate and lets us
// look them up by hash without ever storing the raw token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
