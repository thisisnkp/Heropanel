package marketplace_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/heropanel/internal/marketplace"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/proto"
)

// sha256Hex returns the lowercase hex SHA-256 of a file's contents.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sampleManifest returns a valid, unsigned module manifest.
func sampleManifest() proto.Manifest {
	return proto.Manifest{
		APIVersion: proto.APIVersion,
		Kind:       "Module",
		Metadata:   proto.Metadata{Slug: "backups-pro", Name: "Backups Pro", Version: "2.1.0", Category: "backup"},
		Spec: proto.Spec{
			Binary:       "hp-mod-backups-pro",
			Capabilities: []string{"backup.offsite", "backup.encrypt"},
			Arch:         []string{"amd64", "arm64"},
			Signing:      proto.Signing{Checksum: "abc123"},
		},
	}
}

// signWith signs a manifest and returns it with the signature filled in.
func signWith(t *testing.T, m proto.Manifest, priv ed25519.PrivateKey) proto.Manifest {
	t.Helper()
	sig, err := marketplace.SignManifest(m, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	m.Spec.Signing.Signature = sig
	return m
}

func mustKeyring(t *testing.T, pub string) *marketplace.Keyring {
	t.Helper()
	kr, err := marketplace.NewKeyring(pub)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return kr
}

func TestVerifyManifestRoundTrip(t *testing.T) {
	pub, priv, err := marketplace.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sk, _ := base64.StdEncoding.DecodeString(priv)
	m := signWith(t, sampleManifest(), ed25519.PrivateKey(sk))

	kr := mustKeyring(t, pub)
	id, err := kr.VerifyManifest(m)
	if err != nil {
		t.Fatalf("verify failed for a manifest signed by a trusted key: %v", err)
	}
	pk, _ := base64.StdEncoding.DecodeString(pub)
	if want := marketplace.KeyID(pk); id != want {
		t.Errorf("key id = %q, want %q", id, want)
	}
}

// The signature covers the whole manifest including the artifact checksum: an
// attacker who swaps the checksum (to point the binary check at their payload)
// must break the signature to do it.
func TestTamperingWithChecksumBreaksSignature(t *testing.T) {
	pub, priv, _ := marketplace.GenerateKey()
	sk, _ := base64.StdEncoding.DecodeString(priv)
	m := signWith(t, sampleManifest(), ed25519.PrivateKey(sk))

	m.Spec.Signing.Checksum = "deadbeef" // swap the pinned hash, keep the signature
	kr := mustKeyring(t, pub)
	if _, err := kr.VerifyManifest(m); !errors.Is(err, marketplace.ErrBadSignature) && !errors.Is(err, marketplace.ErrUntrustedKey) {
		t.Fatalf("tampered checksum verified (or wrong error): %v", err)
	}
}

func TestVerifyManifestRejectsUntrustedKey(t *testing.T) {
	_, priv, _ := marketplace.GenerateKey() // signer
	otherPub, _, _ := marketplace.GenerateKey()
	sk, _ := base64.StdEncoding.DecodeString(priv)
	m := signWith(t, sampleManifest(), ed25519.PrivateKey(sk))

	kr := mustKeyring(t, otherPub) // trust a different key
	if _, err := kr.VerifyManifest(m); !errors.Is(err, marketplace.ErrUntrustedKey) {
		t.Fatalf("want ErrUntrustedKey, got %v", err)
	}
}

func TestVerifyManifestNoTrustAnchor(t *testing.T) {
	kr, _ := marketplace.NewKeyring() // empty
	if !kr.Empty() {
		t.Fatal("empty keyring not reported empty")
	}
	if _, err := kr.VerifyManifest(sampleManifest()); !errors.Is(err, marketplace.ErrNoTrustAnchor) {
		t.Fatalf("want ErrNoTrustAnchor, got %v", err)
	}
}

func TestVerifyManifestBadSignature(t *testing.T) {
	pub, _, _ := marketplace.GenerateKey()
	m := sampleManifest()
	m.Spec.Signing.Signature = "not-base64-!!"
	kr := mustKeyring(t, pub)
	if _, err := kr.VerifyManifest(m); !errors.Is(err, marketplace.ErrBadSignature) {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestVerifyArtifact(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hp-mod-backups-pro")
	if err := os.WriteFile(bin, []byte("the binary bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha256("the binary bytes")
	m := sampleManifest()
	m.Spec.Signing.Checksum = "b4c9dfa4e2b8f3d1e6c0aa1e2c8b7a9d5f4e3c2b1a0908070605040302010f0e" // wrong on purpose
	if err := marketplace.VerifyArtifact(bin, m); err == nil {
		t.Fatal("VerifyArtifact accepted a wrong checksum")
	}
	// Now with the right checksum.
	m.Spec.Signing.Checksum = sha256Hex(t, bin)
	if err := marketplace.VerifyArtifact(bin, m); err != nil {
		t.Fatalf("VerifyArtifact rejected a matching binary: %v", err)
	}
	// Empty checksum is refused.
	m.Spec.Signing.Checksum = ""
	if err := marketplace.VerifyArtifact(bin, m); err == nil {
		t.Fatal("VerifyArtifact accepted an empty checksum")
	}
}

func TestParseCatalog(t *testing.T) {
	c := marketplace.Catalog{Modules: []proto.Manifest{sampleManifest()}}
	b, _ := json.Marshal(c)
	got, err := marketplace.ParseCatalog(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Modules) != 1 || got.Modules[0].Metadata.Slug != "backups-pro" {
		t.Fatalf("round-trip lost the module: %+v", got)
	}
}

// ── Service, against an in-memory store ──────────────────────────────────────

type memStore struct {
	rows map[string]marketplace.InstalledModule
}

func newMemStore() *memStore { return &memStore{rows: map[string]marketplace.InstalledModule{}} }

func (s *memStore) Upsert(_ context.Context, m marketplace.InstalledModule) error {
	m.InstalledAt, m.UpdatedAt = "now", "now"
	s.rows[m.Slug] = m
	return nil
}
func (s *memStore) SetState(_ context.Context, slug, state string) error {
	m, ok := s.rows[slug]
	if !ok {
		return errx.NotFound("module_not_found", "no such module")
	}
	m.State = state
	s.rows[slug] = m
	return nil
}
func (s *memStore) Get(_ context.Context, slug string) (*marketplace.InstalledModule, error) {
	m, ok := s.rows[slug]
	if !ok {
		return nil, errx.NotFound("module_not_found", "no such module")
	}
	return &m, nil
}
func (s *memStore) List(_ context.Context) ([]marketplace.InstalledModule, error) {
	out := make([]marketplace.InstalledModule, 0, len(s.rows))
	for _, m := range s.rows {
		out = append(out, m)
	}
	return out, nil
}
func (s *memStore) Delete(_ context.Context, slug string) error {
	delete(s.rows, slug)
	return nil
}

// newSignedEnv returns a service whose catalog holds one trusted, signed module
// ("backups-pro") and one untrusted, unsigned module ("rogue").
func newSignedEnv(t *testing.T) (*marketplace.Service, *memStore) {
	t.Helper()
	pub, priv, _ := marketplace.GenerateKey()
	sk, _ := base64.StdEncoding.DecodeString(priv)
	signed := signWith(t, sampleManifest(), ed25519.PrivateKey(sk))

	rogue := sampleManifest()
	rogue.Metadata.Slug = "rogue"
	rogue.Metadata.Name = "Rogue"
	// no signature

	cat := &marketplace.Catalog{Modules: []proto.Manifest{signed, rogue}}
	kr := mustKeyring(t, pub)
	store := newMemStore()
	return marketplace.NewService(kr, cat, store, nil), store
}

func TestBrowseReportsTrustVerdicts(t *testing.T) {
	svc, _ := newSignedEnv(t)
	entries, err := svc.Browse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byslug := map[string]marketplace.CatalogEntry{}
	for _, e := range entries {
		byslug[e.Slug] = e
	}
	if !byslug["backups-pro"].Verified {
		t.Error("signed module not reported verified")
	}
	if byslug["rogue"].Verified {
		t.Error("unsigned module reported verified")
	}
	if byslug["rogue"].VerifyError == "" {
		t.Error("unsigned module carries no reason")
	}
}

func TestInstallRefusesUnverified(t *testing.T) {
	svc, _ := newSignedEnv(t)
	if _, err := svc.Install(context.Background(), "rogue"); err == nil {
		t.Fatal("installed an unsigned module")
	} else if errx.KindOf(err) != errx.KindForbidden {
		t.Errorf("want forbidden, got %v (%v)", errx.KindOf(err), err)
	}
	if _, err := svc.Install(context.Background(), "does-not-exist"); errx.KindOf(err) != errx.KindNotFound {
		t.Errorf("unknown slug: want not_found, got %v", errx.KindOf(err))
	}
}

func TestInstallEnableDisableUninstall(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSignedEnv(t)

	rec, err := svc.Install(ctx, "backups-pro")
	if err != nil {
		t.Fatalf("install verified module: %v", err)
	}
	if rec.State != marketplace.StateInstalled {
		t.Errorf("fresh install state = %q", rec.State)
	}

	// Browse now shows it installed.
	entries, _ := svc.Browse(ctx)
	for _, e := range entries {
		if e.Slug == "backups-pro" && !e.Installed {
			t.Error("installed module not marked installed in browse")
		}
	}

	rec, err = svc.SetEnabled(ctx, "backups-pro", true)
	if err != nil || rec.State != marketplace.StateEnabled {
		t.Fatalf("enable: state=%q err=%v", rec.State, err)
	}
	rec, err = svc.SetEnabled(ctx, "backups-pro", false)
	if err != nil || rec.State != marketplace.StateDisabled {
		t.Fatalf("disable: state=%q err=%v", rec.State, err)
	}

	// Enabling an uninstalled module is refused.
	if _, err := svc.SetEnabled(ctx, "not-installed", true); errx.KindOf(err) != errx.KindNotFound {
		t.Errorf("enable uninstalled: want not_found, got %v", errx.KindOf(err))
	}

	if err := svc.Uninstall(ctx, "backups-pro"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if err := svc.Uninstall(ctx, "backups-pro"); errx.KindOf(err) != errx.KindNotFound {
		t.Errorf("double uninstall: want not_found, got %v", errx.KindOf(err))
	}
}

func TestDisabledWithoutStore(t *testing.T) {
	svc := marketplace.NewService(nil, nil, nil, nil)
	if svc.Enabled() {
		t.Error("service with no store reported enabled")
	}
	if svc.TrustAnchored() {
		t.Error("service with no keyring reported trust-anchored")
	}
}
