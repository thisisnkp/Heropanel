// Package edkey parses operator-supplied ed25519 key material.
//
// The panel anchors two separate trust chains to ed25519 keys pinned by an
// operator — the **release key** that signs `SHA256SUMS` and the update channel
// manifest (internal/installer, internal/update), and the **publisher keys**
// that sign third-party module manifests (internal/marketplace). Both accept a
// key the same three ways, and both had their own byte-identical copy of the
// decoder.
//
// One copy is the point of this package. Two implementations of "how do we read
// a trust anchor" is precisely where a trust chain rots: a fix or a hardening
// applied to one and missed in the other leaves a gap nobody is looking at,
// because both files read as if they were the only one.
package edkey

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Decode accepts a "@/path/to/key" reference, a base64 string, or a hex string,
// and returns the raw key bytes.
//
// The @path form matters: a key passed as a command-line argument lands in the
// process table and the operator's shell history, and one passed as an
// environment variable is readable from /proc for anything running as that user.
// A file the operator controls the mode of is the only form that avoids both.
func Decode(s string) ([]byte, error) {
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

// PublicKey decodes and length-checks an ed25519 public key. The length check
// is not pedantry: ed25519.Verify panics on a wrong-sized key, so a typo'd
// anchor would take the process down rather than refuse the signature.
func PublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := Decode(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// PrivateKey decodes and length-checks an ed25519 private key. Only release and
// publisher *tooling* ever calls this — a panel holds public keys alone.
func PrivateKey(s string) (ed25519.PrivateKey, error) {
	raw, err := Decode(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}

// ID is a short, stable fingerprint of a public key: the first eight bytes of
// its SHA-256, hex-encoded. It names a key in logs and records without carrying
// the whole key around, and lets an operator confirm the panel pinned the key
// they meant to.
func ID(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}
