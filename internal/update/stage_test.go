package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// release builds a fake release server: three "binaries", a SHA256SUMS over
// them, a channels.json, and detached signatures for both — signed by signKey,
// which the tests vary to prove the panel refuses anything else.
type release struct {
	version string
	files   map[string][]byte
	sums    []byte
	sumsSig string
	chans   []byte
	chanSig string
}

func newRelease(t *testing.T, version string, signKey ed25519.PrivateKey) *release {
	t.Helper()
	r := &release{version: version, files: map[string][]byte{}}
	var lines []string
	for _, name := range updateBinaries {
		body := []byte("#!/bin/sh\n# " + name + " " + version + "\n")
		r.files[name] = body
		sum := sha256.Sum256(body)
		lines = append(lines, fmt.Sprintf("%s  %s", hex.EncodeToString(sum[:]), name))
	}
	r.sums = []byte(strings.Join(lines, "\n") + "\n")
	r.sumsSig = base64.StdEncoding.EncodeToString(ed25519.Sign(signKey, r.sums))

	m := Manifest{Channels: map[string]ChannelRelease{
		ChannelStable: {Version: version, Notes: "test release"},
	}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	r.chans = raw
	r.chanSig = base64.StdEncoding.EncodeToString(ed25519.Sign(signKey, raw))
	return r
}

func (r *release) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/channels.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(r.chans) })
	mux.HandleFunc("/channels.json.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(r.chanSig))
	})
	mux.HandleFunc("/"+r.version+"/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(r.sums) })
	mux.HandleFunc("/"+r.version+"/SHA256SUMS.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(r.sumsSig))
	})
	for name, body := range r.files {
		b := body
		mux.HandleFunc("/"+r.version+"/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// stubRepo satisfies Repo without a database.
type stubRepo struct {
	rows []Row
}

func (s *stubRepo) Insert(_ context.Context, r *Row) error { s.rows = append(s.rows, *r); return nil }
func (s *stubRepo) Latest(_ context.Context) (*Row, error) {
	if len(s.rows) == 0 {
		return nil, nil
	}
	r := s.rows[len(s.rows)-1]
	return &r, nil
}
func (s *stubRepo) List(_ context.Context, _ int) ([]Row, error) { return s.rows, nil }
func (s *stubRepo) SetState(_ context.Context, uid, state, msg string, _ bool) error {
	for i := range s.rows {
		if s.rows[i].UID == uid {
			s.rows[i].State, s.rows[i].Error = state, msg
		}
	}
	return nil
}

func newSvc(t *testing.T, srvURL, pubKey, current string) *Service {
	t.Helper()
	return NewService(&stubRepo{}, nil, Config{
		Channel: ChannelStable, BaseURL: srvURL, PubKey: pubKey,
	}, current, t.TempDir(), nil)
}

func genKey(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pub), priv
}

func TestStageVerifiesAndLandsArtifacts(t *testing.T) {
	pub, priv := genKey(t)
	rel := newRelease(t, "1.2.3", priv)
	srv := rel.server(t)
	s := newSvc(t, srv.URL, pub, "1.0.0")

	dir, err := s.Stage(context.Background(), "1.2.3")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	// Every binary plus the manifest and its signature, so the installer can
	// re-verify the chain itself rather than trusting npd did.
	for _, name := range append(append([]string{}, updateBinaries...), "SHA256SUMS", "SHA256SUMS.sig") {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("staged release is missing %s: %v", name, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "npd"))
	if err != nil || string(got) != string(rel.files["npd"]) {
		t.Errorf("staged npd does not match what the server served")
	}
	// Nothing partial survives a success.
	if _, err := os.Stat(dir + ".partial"); !os.IsNotExist(err) {
		t.Errorf(".partial directory should be gone after a successful stage")
	}
}

// The signature is the whole trust root: a manifest signed by anyone else must
// be refused even though every checksum inside it is internally consistent.
func TestStageRefusesWrongKey(t *testing.T) {
	_, attackerPriv := genKey(t)
	pinnedPub, _ := genKey(t)
	rel := newRelease(t, "1.2.3", attackerPriv)
	srv := rel.server(t)
	s := newSvc(t, srv.URL, pinnedPub, "1.0.0")

	_, err := s.Stage(context.Background(), "1.2.3")
	if err == nil {
		t.Fatal("Stage accepted a release signed by an untrusted key")
	}
	if !strings.Contains(err.Error(), "release_untrusted") {
		t.Errorf("expected release_untrusted, got %v", err)
	}
	assertNothingStaged(t, s, "1.2.3")
}

// A swapped binary with the manifest left alone must fail on the checksum.
func TestStageRefusesTamperedBinary(t *testing.T) {
	pub, priv := genKey(t)
	rel := newRelease(t, "1.2.3", priv)
	rel.files["np-broker"] = []byte("#!/bin/sh\n# backdoored\n")
	srv := rel.server(t)
	s := newSvc(t, srv.URL, pub, "1.0.0")

	_, err := s.Stage(context.Background(), "1.2.3")
	if err == nil {
		t.Fatal("Stage accepted a binary that does not match the signed manifest")
	}
	if !strings.Contains(err.Error(), "release_corrupt") {
		t.Errorf("expected release_corrupt, got %v", err)
	}
	assertNothingStaged(t, s, "1.2.3")
}

// Swapping the checksum *and* re-listing it must still fail, because the
// signature covers the manifest bytes.
func TestStageRefusesTamperedManifest(t *testing.T) {
	pub, priv := genKey(t)
	rel := newRelease(t, "1.2.3", priv)
	evil := []byte("#!/bin/sh\n# backdoored\n")
	rel.files["npd"] = evil
	sum := sha256.Sum256(evil)
	rel.sums = []byte(fmt.Sprintf("%s  npd\n", hex.EncodeToString(sum[:])))
	// rel.sumsSig is left as the signature over the ORIGINAL manifest.
	srv := rel.server(t)
	s := newSvc(t, srv.URL, pub, "1.0.0")

	_, err := s.Stage(context.Background(), "1.2.3")
	if err == nil {
		t.Fatal("Stage accepted a manifest whose signature no longer matches it")
	}
	if !strings.Contains(err.Error(), "release_untrusted") {
		t.Errorf("expected release_untrusted, got %v", err)
	}
	assertNothingStaged(t, s, "1.2.3")
}

func assertNothingStaged(t *testing.T, s *Service, version string) {
	t.Helper()
	dir := s.StageDir(version)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a refused release must leave no staged directory at %s", dir)
	}
	if _, err := os.Stat(dir + ".partial"); !os.IsNotExist(err) {
		t.Errorf("a refused release must leave no .partial directory")
	}
}

// A version is a path segment; it must never be able to escape the stage root.
func TestStageRefusesTraversingVersion(t *testing.T) {
	pub, priv := genKey(t)
	rel := newRelease(t, "1.2.3", priv)
	srv := rel.server(t)
	s := newSvc(t, srv.URL, pub, "1.0.0")

	for _, bad := range []string{"../../etc", "..", "1.2.3/../../x", "a/b", "", strings.Repeat("9", 65)} {
		if _, err := s.Stage(context.Background(), bad); err == nil {
			t.Errorf("Stage accepted version %q", bad)
		}
	}
}

func TestStatusReportsAvailableRelease(t *testing.T) {
	pub, priv := genKey(t)
	rel := newRelease(t, "1.2.3", priv)
	srv := rel.server(t)

	s := newSvc(t, srv.URL, pub, "1.0.0")
	st := s.Status(context.Background())
	if !st.Configured {
		t.Fatalf("expected configured, reason=%q", st.Reason)
	}
	if st.UpToDate || st.Available != "1.2.3" {
		t.Errorf("got available=%q up_to_date=%v, want 1.2.3 / false", st.Available, st.UpToDate)
	}
	if st.Notes != "test release" {
		t.Errorf("notes = %q", st.Notes)
	}

	// Already on the offered version.
	s2 := newSvc(t, srv.URL, pub, "1.2.3")
	if st2 := s2.Status(context.Background()); !st2.UpToDate || st2.Available != "" {
		t.Errorf("running the offered version should be up to date, got %+v", st2)
	}

	// Ahead of it — a downgrade must never be offered as an update.
	s3 := newSvc(t, srv.URL, pub, "2.0.0")
	if st3 := s3.Status(context.Background()); !st3.UpToDate || st3.Available != "" {
		t.Errorf("running ahead of the channel should be up to date, got %+v", st3)
	}
}

// With no key or no source the panel must keep working and simply explain why
// updates are off — an unreachable release server is not an outage.
func TestStatusUnconfigured(t *testing.T) {
	pub, _ := genKey(t)

	noSource := NewService(&stubRepo{}, nil, Config{Channel: ChannelStable, PubKey: pub}, "1.0.0", t.TempDir(), nil)
	if st := noSource.Status(context.Background()); st.Configured || st.Reason == "" {
		t.Errorf("expected unconfigured with a reason, got %+v", st)
	}

	noKey := NewService(&stubRepo{}, nil, Config{Channel: ChannelStable, BaseURL: "https://example.invalid"}, "1.0.0", t.TempDir(), nil)
	st := noKey.Status(context.Background())
	if st.Configured {
		t.Error("a release source with no pinned key must not count as configured")
	}
	if !strings.Contains(st.Reason, "public key") {
		t.Errorf("reason should name the missing key, got %q", st.Reason)
	}
}

func TestParseManifestRejectsBadSignature(t *testing.T) {
	pub, priv := genKey(t)
	pubKey, err := ParseReleaseKey(pub)
	if err != nil {
		t.Fatalf("ParseReleaseKey: %v", err)
	}
	raw := []byte(`{"channels":{"stable":{"version":"1.0.0"}}}`)
	good := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw))

	if _, err := ParseManifest(raw, good, pubKey); err != nil {
		t.Fatalf("a correctly signed manifest was refused: %v", err)
	}
	for name, sig := range map[string]string{
		"empty":      "",
		"not base64": "!!!!",
		"wrong size": base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		if _, err := ParseManifest(raw, sig, pubKey); err == nil {
			t.Errorf("ParseManifest accepted a %s signature", name)
		}
	}
	// Signed, but the bytes changed afterwards.
	if _, err := ParseManifest([]byte(`{"channels":{"stable":{"version":"9.9.9"}}}`), good, pubKey); err == nil {
		t.Error("ParseManifest accepted a manifest whose bytes no longer match its signature")
	}
}

func TestManifestRelease(t *testing.T) {
	m := Manifest{Channels: map[string]ChannelRelease{
		ChannelStable: {Version: "1.0.0"},
		ChannelBeta:   {Version: ""},
	}}
	if _, err := m.Release(ChannelStable); err != nil {
		t.Errorf("stable should resolve: %v", err)
	}
	if _, err := m.Release(ChannelBeta); err == nil {
		t.Error("a channel naming no version must not resolve")
	}
	if _, err := m.Release(ChannelNightly); err == nil {
		t.Error("an absent channel must not resolve")
	}
	if got := m.Names(); len(got) != 2 || got[0] != ChannelBeta {
		t.Errorf("Names() = %v, want sorted [beta stable]", got)
	}
}
