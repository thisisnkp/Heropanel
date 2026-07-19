package secrets_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/secrets"
)

func newCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	c, err := secrets.FromBase64(key)
	if err != nil {
		t.Fatalf("from base64: %v", err)
	}
	return c
}

func TestSealOpenRoundTrip(t *testing.T) {
	c := newCipher(t)
	aad := secrets.AAD("git_sources", 7, "credential_enc")

	blob, err := c.Seal([]byte("ghp_supersecret"), aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(blob, "ghp_supersecret") {
		t.Fatalf("plaintext leaked into the stored blob: %q", blob)
	}
	if !strings.HasPrefix(blob, "hp1.") {
		t.Fatalf("missing version prefix: %q", blob)
	}

	got, err := c.Open(blob, aad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != "ghp_supersecret" {
		t.Fatalf("round trip = %q", got)
	}
}

func TestNonceIsPerRecord(t *testing.T) {
	c := newCipher(t)
	aad := secrets.AAD("git_sources", 1, "credential_enc")
	a, _ := c.Seal([]byte("same"), aad)
	b, _ := c.Seal([]byte("same"), aad)
	if a == b {
		t.Fatal("identical plaintext sealed to identical ciphertext — the nonce is not per-record")
	}
}

// The point of the AAD binding: a ciphertext stolen from one row must not open
// against another row, even though both were sealed with the same key.
func TestCiphertextCannotBeSwappedBetweenRecords(t *testing.T) {
	c := newCipher(t)
	blob, err := c.Seal([]byte("victim-token"), secrets.AAD("git_sources", 1, "credential_enc"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := c.Open(blob, secrets.AAD("git_sources", 2, "credential_enc")); err == nil {
		t.Fatal("ciphertext opened against a different row id")
	}
	if _, err := c.Open(blob, secrets.AAD("git_sources", 1, "webhook_secret_enc")); err == nil {
		t.Fatal("ciphertext opened against a different column")
	}
	if _, err := c.Open(blob, secrets.AAD("other_table", 1, "credential_enc")); err == nil {
		t.Fatal("ciphertext opened against a different table")
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	c := newCipher(t)
	aad := secrets.AAD("git_sources", 1, "credential_enc")
	blob, _ := c.Seal([]byte("token"), aad)

	// Flip a byte in the body.
	b := []byte(blob)
	b[len(b)-1] ^= 0x01
	if _, err := c.Open(string(b), aad); err == nil {
		t.Fatal("tampered ciphertext opened")
	}
	if _, err := c.Open("hp2."+strings.TrimPrefix(blob, "hp1."), aad); err == nil {
		t.Fatal("unknown version opened")
	}
	if _, err := c.Open("garbage", aad); err == nil {
		t.Fatal("malformed blob opened")
	}
}

func TestWrongKeyCannotOpen(t *testing.T) {
	a, b := newCipher(t), newCipher(t)
	aad := secrets.AAD("git_sources", 1, "credential_enc")
	blob, _ := a.Seal([]byte("token"), aad)
	if _, err := b.Open(blob, aad); err == nil {
		t.Fatal("a different master key opened the ciphertext")
	}
}

// An unconfigured panel must refuse to seal — never fall back to plaintext.
func TestUnconfiguredCipherRefuses(t *testing.T) {
	c, err := secrets.FromBase64("")
	if err != nil {
		t.Fatalf("empty key should not be an error: %v", err)
	}
	if c.Configured() {
		t.Fatal("nil cipher reported itself as configured")
	}
	if _, err := c.Seal([]byte("x"), "a:1:b"); !errors.Is(err, secrets.ErrNoCipher) {
		t.Fatalf("want ErrNoCipher, got %v", err)
	}
	if _, err := c.Open("hp1.zzz", "a:1:b"); !errors.Is(err, secrets.ErrNoCipher) {
		t.Fatalf("want ErrNoCipher, got %v", err)
	}
}

func TestMasterKeyValidation(t *testing.T) {
	if _, err := secrets.New([]byte("too-short")); err == nil {
		t.Fatal("a short master key was accepted")
	}
	if _, err := secrets.FromBase64("not!base64!"); err == nil {
		t.Fatal("a non-base64 master key was accepted")
	}
	// A valid 32-byte key in raw-url encoding must work too (operators paste keys
	// from all sorts of tools).
	raw := make([]byte, secrets.MasterKeyLen)
	for i := range raw {
		raw[i] = byte(i)
	}
	if _, err := secrets.FromBase64(base64.RawURLEncoding.EncodeToString(raw)); err != nil {
		t.Fatalf("raw-url key rejected: %v", err)
	}
}

func TestSealRequiresAAD(t *testing.T) {
	c := newCipher(t)
	if _, err := c.Seal([]byte("x"), ""); err == nil {
		t.Fatal("sealing without an AAD binding was allowed")
	}
}
