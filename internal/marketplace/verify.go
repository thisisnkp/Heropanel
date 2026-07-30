// Package marketplace is the trust layer under HeroPanel's module catalog: it
// decides whether a third-party module may be installed at all.
//
// The manifest (pkg/proto) already describes what a module is and Validate
// checks it is well-formed. But well-formed is not the same as trusted — a
// manifest arrives with a downloaded package and an attacker who can swap the
// package can swap the manifest beside it. What makes a module trustworthy is a
// publisher signature: an ed25519 signature over the canonical manifest bytes,
// checked against a keyring the operator pins out-of-band. Signature valid ⇒ the
// manifest (and therefore the artifact checksum it names) was authored by a key
// hpd was told to trust; the private key never touches the host.
//
// This is the same discipline internal/installer applies to the panel's own
// SHA256SUMS, carried to modules: no code derived from a manifest touches the
// system until a trusted key has vouched for that exact manifest.
package marketplace

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/proto"
)

// Trust outcomes. These are named errors so the surface can tell an operator
// exactly why a module was refused — an unsigned module and one signed by a key
// nobody pinned are different problems with different fixes.
var (
	// ErrNoTrustAnchor means no publisher key is configured. With nothing to
	// verify against, every module is untrusted — the safe default is to refuse
	// them all rather than fall open.
	ErrNoTrustAnchor = errors.New("no trusted publisher key is configured; refusing to trust any module")
	// ErrBadSignature means the manifest carries no signature, or one that is not
	// a well-formed ed25519 signature.
	ErrBadSignature = errors.New("module manifest signature is missing or malformed")
	// ErrUntrustedKey means the signature is well-formed but was not produced by
	// any key in the keyring.
	ErrUntrustedKey = errors.New("module manifest is not signed by any trusted publisher key")
)

// TrustedKey is a publisher's ed25519 public key with a stable id — the short
// fingerprint used to record which key vouched for an installed module.
type TrustedKey struct {
	ID  string
	Key ed25519.PublicKey
}

// Keyring is the set of publisher keys hpd trusts to sign modules. It is built
// once at boot from pinned key material and never mutated, so it is safe to read
// concurrently.
type Keyring struct {
	keys []TrustedKey
}

// NewKeyring parses zero or more encoded ed25519 public keys (base64, hex, or a
// "@/path/to/key" reference) into a keyring. An empty keyring is valid — it
// simply trusts nothing, and VerifyManifest reports ErrNoTrustAnchor.
func NewKeyring(encoded ...string) (*Keyring, error) {
	kr := &Keyring{}
	for _, e := range encoded {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		raw, err := decodeKeyMaterial(e)
		if err != nil {
			return nil, fmt.Errorf("marketplace: publisher key: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("marketplace: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
		}
		kr.keys = append(kr.keys, TrustedKey{ID: KeyID(raw), Key: ed25519.PublicKey(raw)})
	}
	return kr, nil
}

// Empty reports whether the keyring trusts no keys — the condition under which
// every module is refused.
func (k *Keyring) Empty() bool { return k == nil || len(k.keys) == 0 }

// IDs returns the fingerprints of the trusted keys, for an operator to confirm
// the panel pinned the keys they intended.
func (k *Keyring) IDs() []string {
	if k == nil {
		return nil
	}
	out := make([]string, len(k.keys))
	for i, tk := range k.keys {
		out[i] = tk.ID
	}
	return out
}

// KeyID is a short, stable fingerprint of a public key: the first eight bytes of
// its SHA-256, hex-encoded. It names a key in logs and install records without
// carrying the whole key around.
func KeyID(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// VerifyManifest checks that a manifest carries a valid publisher signature and
// returns the id of the key that vouched for it. The caller is expected to have
// run Manifest.Validate first — this adds the provenance gate on top of
// well-formedness. Verification is over the canonical manifest with its own
// signature field blanked, so the signature covers everything else including the
// artifact checksum: swap the checksum and the signature no longer verifies.
func (k *Keyring) VerifyManifest(m proto.Manifest) (string, error) {
	if k.Empty() {
		return "", ErrNoTrustAnchor
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m.Spec.Signing.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", ErrBadSignature
	}
	msg, err := canonicalManifest(m)
	if err != nil {
		return "", err
	}
	for _, tk := range k.keys {
		if ed25519.Verify(tk.Key, msg, sig) {
			return tk.ID, nil
		}
	}
	return "", ErrUntrustedKey
}

// VerifyArtifact checks a module's binary on disk against the checksum its
// manifest names. It is the second half of the chain, run at install time when
// the artifact is present: the signature made the checksum trustworthy, and this
// makes the binary match it. A manifest with no checksum is refused — there is
// nothing to pin the binary to.
func VerifyArtifact(path string, m proto.Manifest) error {
	want := strings.ToLower(strings.TrimSpace(m.Spec.Signing.Checksum))
	if want == "" {
		return errors.New("marketplace: manifest lists no artifact checksum")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("marketplace: artifact sha256 mismatch: got %s, want %s", got, want)
	}
	return nil
}

// SignManifest signs the canonical form of a manifest, returning the base64
// signature to write into Spec.Signing.Signature. It is used by the publisher
// tooling (and the tests) and mirrors what VerifyManifest checks.
func SignManifest(m proto.Manifest, priv ed25519.PrivateKey) (string, error) {
	msg, err := canonicalManifest(m)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)), nil
}

// GenerateKey mints a fresh ed25519 publisher keypair as base64 strings — the
// one-time bootstrap of a publisher's identity. The private key is held offline;
// only the public key is ever pinned into a panel.
func GenerateKey() (pub string, priv string, err error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pk), base64.StdEncoding.EncodeToString(sk), nil
}

// canonicalManifest is the exact byte sequence that is signed and verified: the
// manifest with its own signature field cleared, JSON-encoded. Go's json encoder
// emits struct fields in declaration order and the manifest holds no maps, so
// the encoding is deterministic — signer and verifier derive identical bytes.
func canonicalManifest(m proto.Manifest) ([]byte, error) {
	m.Spec.Signing.Signature = ""
	return json.Marshal(m)
}

// decodeKeyMaterial accepts a "@/path/to/key" reference, a base64 string, or a
// hex string and returns the raw key bytes. The @path form lets an operator keep
// a key in a file rather than on a command line or in a process environment.
func decodeKeyMaterial(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty key")
	}
	if strings.HasPrefix(s, "@") {
		b, err := os.ReadFile(s[1:])
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(string(b))
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	if raw, err := hex.DecodeString(s); err == nil {
		return raw, nil
	}
	return nil, errors.New("key is neither valid base64 nor hex")
}
