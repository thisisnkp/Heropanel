package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/edkey"
)

// A licence token is a compact JWS:
//
//	base64url(header) "." base64url(payload) "." base64url(signature)
//
// with {"alg":"EdDSA","typ":"JWT","kid":"lk1"} as the header. The signature
// covers the two encoded segments **exactly as transmitted** — never a
// re-serialised object. That distinction is the whole security of the scheme:
// a verifier that parsed the JSON and re-encoded it before checking would make
// key order and number formatting part of the trust decision, and two
// implementations disagreeing about either would reject good tokens or, far
// worse, accept altered ones.

// Alg is the only signature algorithm this panel accepts.
const Alg = "EdDSA"

var (
	// ErrMalformed is a token that is not three segments of valid base64url
	// wrapping valid JSON.
	ErrMalformed = errors.New("licence token is malformed")
	// ErrUnknownKey is a token signed under a key id this binary was not
	// shipped with. During a rotation this is what an old binary says about a
	// new token, which is why the panel ships a *set* of keys, not one.
	ErrUnknownKey = errors.New("licence token names an unknown signing key")
	// ErrBadSignature is a token whose signature does not verify. It is not
	// distinguished from tampering anywhere a prober can see — see store.go.
	ErrBadSignature = errors.New("licence token signature does not verify")
)

// Limits are the counts a plan entitles. Zero means "none", not "unlimited":
// a licence that failed to say how many sites it covers must not be read as
// covering every number.
type Limits struct {
	Sites int `json:"sites"`
	DBs   int `json:"dbs"`
	Users int `json:"users"`
}

// Claims is the payload, as the licence server signs it.
type Claims struct {
	LID  string   `json:"lid"`
	Acct string   `json:"acct"`
	Plan string   `json:"plan"`
	Feat []string `json:"feat"`
	Lim  Limits   `json:"lim"`
	FP   string   `json:"fp"`
	IP   string   `json:"ip"`
	IAT  int64    `json:"iat"`
	Exp  int64    `json:"exp"`
	// SExp is when the subscription ends, as distinct from when this token
	// does. Null for a perpetual or free licence — a pointer rather than an
	// int64, because zero is a real unix time and "no expiry" must not read as
	// 1 January 1970.
	SExp  *int64 `json:"sexp"`
	Grace int64  `json:"grace"` // seconds after Exp before the panel degrades
	State string `json:"state"`
}

// ExpiresAt and IssuedAt in the form the rest of the package works in.
func (c Claims) ExpiresAt() time.Time { return time.Unix(c.Exp, 0).UTC() }
func (c Claims) IssuedAt() time.Time  { return time.Unix(c.IAT, 0).UTC() }

// GraceFor is how long after expiry the panel keeps working normally.
//
// Read from the token so the policy can be changed for one customer, or for
// everyone, without shipping a new binary to every server in the fleet. A
// token that omits it gets the compiled default rather than zero, because zero
// would mean an expired token degrades the same second — which is exactly the
// cliff the grace period exists to remove.
func (c Claims) GraceFor() time.Duration {
	if c.Grace <= 0 {
		return DefaultGrace
	}
	return time.Duration(c.Grace) * time.Second
}

// Has reports whether the licence includes a named feature.
func (c Claims) Has(feature string) bool {
	for _, f := range c.Feat {
		if strings.EqualFold(f, feature) {
			return true
		}
	}
	return false
}

// Keyring is the set of licence-server public keys this panel trusts, by key id.
type Keyring map[string]ed25519.PublicKey

// pinnedPubKey is the licence server's public key, compiled in.
//
// Set at build time:
//
//	go build -ldflags "-X github.com/thisisnkp/nexpanel/internal/license.pinnedPubKey=<base64> \
//	                   -X github.com/thisisnkp/nexpanel/internal/license.pinnedKeyID=lk1"
//
// It is a variable rather than a constant only because ldflags cannot write a
// constant; nothing at runtime assigns to it. Compiled in rather than
// configured because a verification key read from the same place the tokens
// come from verifies nothing, and one read from a config file can be replaced
// by anyone who can write that file — which on a customer's own VPS is the
// customer.
var (
	pinnedPubKey = ""
	pinnedKeyID  = "lk1"

	// pinnedPrevPubKey holds the key being rotated *out*, so a binary built
	// during a rotation accepts tokens signed by either. Without a second slot
	// a rotation means every install must upgrade before the server may switch,
	// which in practice means the key is never rotated.
	pinnedPrevPubKey = ""
	pinnedPrevKeyID  = ""
)

// LoadKeyring assembles the trusted keys.
//
// The compiled-in keys are the trust anchor. `extra` — from configuration —
// is accepted **only when this binary has none**, which is the development and
// self-hosted-licence-server case. A build that pins a key ignores
// configuration entirely and says so: if config could add a key, then anyone
// who can write the panel's config file could mint themselves a licence, and on
// a customer's own VPS that is the customer.
func LoadKeyring(extra map[string]string) (Keyring, bool, error) {
	ring := Keyring{}
	for id, material := range map[string]string{pinnedKeyID: pinnedPubKey, pinnedPrevKeyID: pinnedPrevPubKey} {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(material) == "" {
			continue
		}
		pub, err := edkey.PublicKey(material)
		if err != nil {
			return nil, false, fmt.Errorf("compiled-in licence key %q: %w", id, err)
		}
		ring[id] = pub
	}
	if len(ring) > 0 {
		return ring, true, nil
	}

	for id, material := range extra {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(material) == "" {
			continue
		}
		pub, err := edkey.PublicKey(material)
		if err != nil {
			return nil, false, fmt.Errorf("configured licence key %q: %w", id, err)
		}
		ring[id] = pub
	}
	return ring, false, nil
}

// Verify checks a token against the keyring and returns its claims.
//
// It does **not** check expiry. That is a policy decision for the caller — the
// panel walks a degradation ladder from it rather than treating expired as
// invalid — and a verifier that folded the two together would leave the caller
// unable to tell an expired token from a forged one, which are opposite
// situations: one is a customer to invoice, the other is a machine to look at.
func (k Keyring) Verify(token string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrMalformed
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrMalformed
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, ErrMalformed
	}

	// The algorithm is read from the token and then checked against the one
	// algorithm this panel uses — never used to *choose* a verifier. Letting a
	// token name its own algorithm is the alg-confusion family of JWT bugs,
	// whose best-known member is alg:"none".
	if header.Alg != Alg {
		return Claims{}, fmt.Errorf("%w: algorithm %q", ErrBadSignature, header.Alg)
	}

	pub, ok := k[header.Kid]
	if !ok {
		return Claims{}, fmt.Errorf("%w: %q", ErrUnknownKey, header.Kid)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Claims{}, ErrMalformed
	}
	// Over the transmitted strings, joined — not over anything re-encoded.
	if !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		return Claims{}, ErrBadSignature
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrMalformed
	}
	var claims Claims
	// DisallowUnknownFields is deliberately *not* used: a server that adds a
	// claim must not break every panel in the field, and a claim this binary
	// does not know about is one it does not enforce on.
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrMalformed
	}
	if claims.LID == "" || claims.Exp == 0 {
		return Claims{}, ErrMalformed
	}
	return claims, nil
}
