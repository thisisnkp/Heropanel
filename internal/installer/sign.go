package installer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Release signing closes the gap the bare checksum manifest leaves open: an
// attacker who can swap a downloaded binary can swap SHA256SUMS alongside it, so
// verifying the files against that manifest proves only that the two agree, not
// that either is genuine. An **ed25519 signature over the manifest**, checked
// against a public key the operator pins out-of-band (a flag, an env var, or a
// key file), makes the manifest itself trusted — and the manifest already
// makes every binary trusted. Signature present and valid ⇒ the whole chain is
// anchored to the release key; that key never touches the host.
//
// The signature file is `SHA256SUMS.sig`: base64 of the raw 64-byte ed25519
// signature over the exact bytes of `SHA256SUMS`. Keys are base64 of their raw
// ed25519 bytes (32-byte public, 64-byte private seed+public per the stdlib),
// which is what GenerateReleaseKey emits and what the signer/verifier accept —
// a hex form and a `@path` form are accepted too, so an operator can point at a
// key file without shelling it into an argument.

const manifestSigName = "SHA256SUMS.sig"

// GenerateReleaseKey mints a fresh ed25519 release keypair, returned as base64
// strings. The private key is held offline by whoever cuts releases; only the
// public key is ever distributed (pinned into install.sh or passed to
// hp-installer). This is the one-time bootstrap of the trust root.
func GenerateReleaseKey() (pub string, priv string, err error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pk), base64.StdEncoding.EncodeToString(sk), nil
}

// SignManifest signs the manifest bytes with a base64/hex/@path private key and
// returns the base64 signature to write into SHA256SUMS.sig.
func SignManifest(manifest []byte, privKey string) (string, error) {
	raw, err := decodeKeyMaterial(privKey)
	if err != nil {
		return "", fmt.Errorf("private key: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(raw))
	}
	sig := ed25519.Sign(ed25519.PrivateKey(raw), manifest)
	return base64.StdEncoding.EncodeToString(sig), nil
}

// parsePublicKey decodes a base64/hex/@path ed25519 public key.
func parsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := decodeKeyMaterial(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// decodeKeyMaterial accepts a "@/path/to/key" reference, a base64 string, or a
// hex string, and returns the raw key bytes. The @path form lets an operator
// keep a key in a file rather than on a command line (where it would land in the
// process table and shell history).
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

// verifyManifestSignature checks SHA256SUMS.sig against the manifest and the
// pinned public key. A configured public key with a missing or bad signature is
// a hard failure — the whole point is that an unsigned or wrongly-signed
// manifest must not be trusted.
func verifyManifestSignature(sourceDir string, pub ed25519.PublicKey) error {
	manifest, err := os.ReadFile(filepath.Join(sourceDir, "SHA256SUMS"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("a release public key is configured but no SHA256SUMS manifest was found to verify")
		}
		return err
	}
	sigB64, err := os.ReadFile(filepath.Join(sourceDir, manifestSigName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("a release public key is configured but SHA256SUMS.sig is missing — refusing to install an unsigned manifest")
		}
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil {
		return fmt.Errorf("SHA256SUMS.sig is not valid base64: %w", err)
	}
	if !ed25519.Verify(pub, manifest, sig) {
		return errors.New("manifest signature does not verify against the release public key")
	}
	return nil
}
