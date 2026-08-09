package installer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thisisnkp/nexpanel/pkg/edkey"
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
// key file without shelling it into an argument. Key parsing itself lives in
// pkg/edkey, shared with the marketplace's publisher keys.
//
// Self-update (docs/26) verifies the *same* chain from npd, which is why
// ParsePublicKey, VerifyDetachedSig and VerifyChecksum are exported: the
// updater must check a downloaded release with this code rather than a second
// implementation of it.

const manifestSigName = "SHA256SUMS.sig"

// GenerateReleaseKey mints a fresh ed25519 release keypair, returned as base64
// strings. The private key is held offline by whoever cuts releases; only the
// public key is ever distributed (pinned into install.sh or passed to
// np-installer). This is the one-time bootstrap of the trust root.
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
	key, err := edkey.PrivateKey(privKey)
	if err != nil {
		return "", fmt.Errorf("private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest)), nil
}

// ParsePublicKey decodes a base64/hex/@path ed25519 public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) { return edkey.PublicKey(s) }

// VerifyDetachedSig checks a base64 detached ed25519 signature over msg. It is
// the single primitive behind every signed release artifact — the SHA256SUMS
// manifest here, and the update channel manifest in internal/update — so there
// is one place where "is this signed by the release key" is decided.
func VerifyDetachedSig(msg []byte, sigB64 string, pub ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return fmt.Errorf("signature is not valid base64: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature must be %d bytes, got %d", ed25519.SignatureSize, len(sig))
	}
	if !ed25519.Verify(pub, msg, sig) {
		return errors.New("signature does not verify against the release public key")
	}
	return nil
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
	if err := VerifyDetachedSig(manifest, string(sigB64), pub); err != nil {
		return fmt.Errorf("SHA256SUMS: %w", err)
	}
	return nil
}
