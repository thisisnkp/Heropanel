package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signManifest signs the staged SHA256SUMS with priv, writing SHA256SUMS.sig.
func signManifest(t *testing.T, dir, priv string) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := SignManifest(manifest, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestSigName), []byte(sig), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSignedManifestInstalls(t *testing.T) {
	pub, priv, err := GenerateReleaseKey()
	if err != nil {
		t.Fatal(err)
	}
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443, ReleasePubKey: pub}, r)
	writeManifest(t, ex.Layout.SourceDir, false)
	signManifest(t, ex.Layout.SourceDir, priv)

	if err := ex.Execute(context.Background()); err != nil {
		t.Fatalf("a correctly signed manifest must install: %v", err)
	}
	mustExist(t, filepath.Join(ex.Layout.BinDir, "hpd"))
}

func TestUnsignedManifestRefusedWhenKeyPinned(t *testing.T) {
	pub, _, _ := GenerateReleaseKey()
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443, ReleasePubKey: pub}, r)
	writeManifest(t, ex.Layout.SourceDir, false) // manifest but no .sig

	err := ex.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SHA256SUMS.sig is missing") {
		t.Fatalf("a pinned key with no signature must abort, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(ex.Layout.BinDir, "hpd")); !os.IsNotExist(statErr) {
		t.Error("nothing may install when the manifest is unsigned")
	}
}

func TestTamperedManifestFailsSignature(t *testing.T) {
	pub, priv, _ := GenerateReleaseKey()
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443, ReleasePubKey: pub}, r)
	writeManifest(t, ex.Layout.SourceDir, false)
	signManifest(t, ex.Layout.SourceDir, priv)
	// Rewrite the manifest after signing — the signature no longer matches.
	writeManifest(t, ex.Layout.SourceDir, true)

	err := ex.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signature does not verify") {
		t.Fatalf("a manifest changed after signing must fail verification, got %v", err)
	}
}

func TestWrongKeyRejected(t *testing.T) {
	_, priv, _ := GenerateReleaseKey()
	otherPub, _, _ := GenerateReleaseKey() // a different keypair's public half
	r := newFakeRunner()
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443, ReleasePubKey: otherPub}, r)
	writeManifest(t, ex.Layout.SourceDir, false)
	signManifest(t, ex.Layout.SourceDir, priv)

	err := ex.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signature does not verify") {
		t.Fatalf("a signature from the wrong key must be rejected, got %v", err)
	}
}

func TestKeyMaterialEncodings(t *testing.T) {
	pub, _, _ := GenerateReleaseKey()
	// base64 (as emitted)
	if _, err := parsePublicKey(pub); err != nil {
		t.Fatalf("base64 key: %v", err)
	}
	// @path form
	dir := t.TempDir()
	kp := filepath.Join(dir, "key.pub")
	if err := os.WriteFile(kp, []byte(pub+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parsePublicKey("@" + kp); err != nil {
		t.Fatalf("@path key: %v", err)
	}
	// garbage
	if _, err := parsePublicKey("not-a-key!!"); err == nil {
		t.Fatal("garbage key must be rejected")
	}
}
