package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/pkg/blobcrypt"
)

// chainFor is the heart of restore correctness: the latest full at or before the
// target, then everything up to and including it.
func TestChainFor(t *testing.T) {
	all := []Record{
		{UID: "f1", Level: LevelFull},
		{UID: "i1", Level: LevelIncr},
		{UID: "i2", Level: LevelIncr},
		{UID: "f2", Level: LevelFull},
		{UID: "i3", Level: LevelIncr},
	}

	chain, err := chainFor(all, "i2")
	if err != nil {
		t.Fatalf("chain i2: %v", err)
	}
	if got := uids(chain); got != "f1,i1,i2" {
		t.Errorf("chain for i2 = %s, want f1,i1,i2", got)
	}

	// A later incremental belongs to the SECOND chain — the first full must not
	// leak in.
	chain, err = chainFor(all, "i3")
	if err != nil {
		t.Fatalf("chain i3: %v", err)
	}
	if got := uids(chain); got != "f2,i3" {
		t.Errorf("chain for i3 = %s, want f2,i3", got)
	}

	// A full restores alone.
	chain, _ = chainFor(all, "f2")
	if got := uids(chain); got != "f2" {
		t.Errorf("chain for f2 = %s, want f2", got)
	}

	// An orphan incremental (no full before it) must refuse, not restore garbage.
	if _, err := chainFor([]Record{{UID: "ix", Level: LevelIncr}}, "ix"); err == nil {
		t.Error("an incremental with no full restored")
	}
	if _, err := chainFor(all, "nope"); err == nil {
		t.Error("an unknown backup produced a chain")
	}
}

func uids(recs []Record) string {
	parts := make([]string, len(recs))
	for i, r := range recs {
		parts[i] = r.UID
	}
	return strings.Join(parts, ",")
}

// The S3 target signs with real SigV4 — verified by recomputing the signature
// server-side from the same inputs, not by matching a golden string.
func TestS3SigV4RoundTrip(t *testing.T) {
	const access, secret, region, bucket = "AKIDEXAMPLE", "topsecret", "eu-central-1", "hp-backups"

	var gotAuth, gotDate, gotSHA, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotDate = r.Header.Get("x-amz-date")
		gotSHA = r.Header.Get("x-amz-content-sha256")
		gotPath = r.URL.EscapedPath()
		gotBody, _ = io.ReadAll(r.Body)

		// Recompute the signature exactly as AWS would.
		canonical := strings.Join([]string{
			r.Method, r.URL.EscapedPath(), r.URL.RawQuery,
			"host:" + r.Host + "\nx-amz-content-sha256:" + gotSHA + "\nx-amz-date:" + gotDate + "\n",
			"host;x-amz-content-sha256;x-amz-date", gotSHA,
		}, "\n")
		ch := sha256.Sum256([]byte(canonical))
		date := gotDate[:8]
		scope := date + "/" + region + "/s3/aws4_request"
		sts := strings.Join([]string{"AWS4-HMAC-SHA256", gotDate, scope, hex.EncodeToString(ch[:])}, "\n")
		mac := func(key, data []byte) []byte {
			h := hmac.New(sha256.New, key)
			h.Write(data)
			return h.Sum(nil)
		}
		k := mac([]byte("AWS4"+secret), []byte(date))
		k = mac(k, []byte(region))
		k = mac(k, []byte("s3"))
		k = mac(k, []byte("aws4_request"))
		want := hex.EncodeToString(mac(k, []byte(sts)))

		if !strings.HasSuffix(gotAuth, "Signature="+want) {
			http.Error(w, "signature mismatch", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s3 := NewS3(S3Config{Endpoint: srv.URL, Region: region, Bucket: bucket, AccessKey: access, SecretKey: secret})
	if s3 == nil {
		t.Fatal("NewS3 returned nil for a complete config")
	}
	s3.client = srv.Client()
	s3.now = func() time.Time { return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC) }

	body := []byte("sealed backup bytes")
	if err := s3.Put(t.Context(), "sites/S1/B1.enc", strings.NewReader(string(body)), int64(len(body))); err != nil {
		t.Fatalf("put (signature rejected?): %v", err)
	}
	if gotPath != "/"+bucket+"/sites/S1/B1.enc" {
		t.Errorf("path = %q", gotPath)
	}
	if gotSHA != unsignedPayload {
		t.Errorf("content sha = %q, want UNSIGNED-PAYLOAD", gotSHA)
	}
	if string(gotBody) != string(body) {
		t.Errorf("body did not arrive intact")
	}

	// An incomplete config is "no target", never a half-working one.
	if NewS3(S3Config{Endpoint: srv.URL}) != nil {
		t.Error("NewS3 accepted an incomplete config")
	}
}

// EnsureBucket is a signed PUT on the bucket itself, and idempotent: a fresh
// bucket (200) and an existing one (409) both succeed; anything else fails.
func TestS3EnsureBucket(t *testing.T) {
	var status = http.StatusOK
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.EscapedPath(), r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	defer srv.Close()

	s3 := NewS3(S3Config{Endpoint: srv.URL, Region: "us-east-1", Bucket: "hp", AccessKey: "a", SecretKey: "s"})
	s3.client = srv.Client()

	if err := s3.EnsureBucket(t.Context()); err != nil {
		t.Fatalf("fresh bucket: %v", err)
	}
	if gotPath != "/hp" {
		t.Errorf("path = %q, want /hp (the bucket, not an object)", gotPath)
	}
	if !strings.Contains(gotAuth, "AWS4-HMAC-SHA256") {
		t.Error("the bucket create was not signed")
	}
	status = http.StatusConflict
	if err := s3.EnsureBucket(t.Context()); err != nil {
		t.Errorf("an existing bucket must be success, got %v", err)
	}
	status = http.StatusForbidden
	if err := s3.EnsureBucket(t.Context()); err == nil {
		t.Error("a 403 on bucket create was swallowed")
	}
}

// ── service-level fakes ──────────────────────────────────────────────────────

// fakeGW plays the broker: backup.create "tars" by writing the staging file.
type fakeGW struct {
	dir   string
	calls []string
}

func (g *fakeGW) Invoke(_ context.Context, cap string, input any) (map[string]any, error) {
	g.calls = append(g.calls, cap)
	if cap == "backup.create" {
		m := input.(map[string]any)
		if err := os.WriteFile(filepath.Join(g.dir, m["file"].(string)), []byte("TARBYTES"), 0o600); err != nil {
			return nil, err
		}
	}
	return map[string]any{}, nil
}
func (g *fakeGW) Health(context.Context) error { return nil }

type memRepo struct {
	recs []Record
	cfg  map[int64]*Config
}

func (m *memRepo) Insert(_ context.Context, r *Record) error { m.recs = append(m.recs, *r); return nil }
func (m *memRepo) ListBySiteID(_ context.Context, siteID int64) ([]Record, error) {
	var out []Record
	for _, r := range m.recs {
		if r.SiteID == siteID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memRepo) GetByUID(_ context.Context, uid string) (*Record, error) {
	for i := range m.recs {
		if m.recs[i].UID == uid {
			return &m.recs[i], nil
		}
	}
	return nil, os.ErrNotExist
}
func (m *memRepo) Delete(_ context.Context, uid string) error {
	for i := range m.recs {
		if m.recs[i].UID == uid {
			m.recs = append(m.recs[:i], m.recs[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memRepo) GetConfig(_ context.Context, siteID int64) (*Config, error) {
	if c, ok := m.cfg[siteID]; ok {
		return c, nil
	}
	return &Config{IntervalHours: 24, Target: TargetLocal, KeepChains: 2}, nil
}
func (m *memRepo) UpsertConfig(_ context.Context, siteID int64, c Config) error {
	if m.cfg == nil {
		m.cfg = map[int64]*Config{}
	}
	m.cfg[siteID] = &c
	return nil
}
func (m *memRepo) EnabledConfigs(context.Context) ([]ConfigRow, error) { return nil, nil }

type fakeSites struct{}

func (fakeSites) Resolve(_ context.Context, uid string) (*SiteRef, error) {
	return &SiteRef{ID: 1, UID: uid, LinuxUser: "hps1", HomeDir: "/srv/1"}, nil
}

// fakeDBs plays the database module: Export writes a plaintext dump file.
type fakeDBs struct {
	dir        string
	exportErr  error
	resolveErr error
	exported   string
}

func (f *fakeDBs) Resolve(_ context.Context, _ string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return "shopdb", nil
}
func (f *fakeDBs) Export(context.Context, string) (string, string, error) {
	if f.exportErr != nil {
		return "", "", f.exportErr
	}
	f.exported = filepath.Join(f.dir, "shopdb-dump.sql.gz")
	if err := os.WriteFile(f.exported, []byte("SQLDUMP"), 0o600); err != nil {
		return "", "", err
	}
	return f.exported, "shopdb", nil
}
func (f *fakeDBs) ImportStagePath(bool) (string, string) {
	return filepath.Join(f.dir, "import-1.sql.gz"), "import-1.sql.gz"
}

func newTestService(t *testing.T) (*Service, *memRepo, *fakeGW, *fakeDBs, string) {
	t.Helper()
	dir := t.TempDir()
	repo := &memRepo{}
	gw := &fakeGW{dir: dir}
	dbs := &fakeDBs{dir: t.TempDir()}
	svc := &Service{
		repo: repo, sites: fakeSites{}, broker: gw, key: make([]byte, 32),
		staging: dir, targets: map[string]Target{TargetLocal: localTarget{dir: dir}},
		dbs: dbs, now: time.Now,
	}
	return svc, repo, gw, dbs, dir
}

// A policy naming a database makes every backup carry a second sealed object:
// the dump — sealed before it touches storage, plaintext gone after the call.
func TestCreateWithDatabaseSealsTheDumpAlongside(t *testing.T) {
	svc, repo, _, dbs, dir := newTestService(t)
	repo.cfg = map[int64]*Config{1: {Enabled: true, IntervalHours: 24, Target: TargetLocal, KeepChains: 2, DBUID: "DB1"}}

	b, err := svc.Create(t.Context(), "S1", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.DBName != "shopdb" {
		t.Errorf("DBName = %q, want shopdb", b.DBName)
	}
	rec := repo.recs[0]
	if rec.DBKey != "sites/S1/"+b.UID+".db.enc" {
		t.Errorf("DBKey = %q", rec.DBKey)
	}

	for _, name := range []string{b.UID + ".enc", b.UID + ".db.enc"} {
		blob, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("sealed object %s missing: %v", name, err)
		}
		if !strings.HasPrefix(string(blob), "HPB1") {
			t.Errorf("%s is not blobcrypt ciphertext", name)
		}
	}
	// The sealed dump opens back to the exact dump bytes.
	var out strings.Builder
	f, _ := os.Open(filepath.Join(dir, b.UID+".db.enc"))
	if err := blobcrypt.Open(&out, f, svc.key); err != nil {
		t.Fatalf("open sealed dump: %v", err)
	}
	_ = f.Close()
	if out.String() != "SQLDUMP" {
		t.Errorf("sealed dump round trip = %q", out.String())
	}
	// The plaintext dump and the plaintext tar are both gone.
	if _, err := os.Stat(dbs.exported); !os.IsNotExist(err) {
		t.Error("the plaintext dump outlived Create")
	}
	if _, err := os.Stat(filepath.Join(dir, b.UID+".tar.zst")); !os.IsNotExist(err) {
		t.Error("the plaintext tar outlived Create")
	}
}

// A failed dump fails the WHOLE backup — no record, no orphan objects. A backup
// that silently skipped its database would be discovered at restore time.
func TestCreateFailsWhenTheDumpFails(t *testing.T) {
	svc, repo, _, dbs, dir := newTestService(t)
	repo.cfg = map[int64]*Config{1: {DBUID: "DB1"}}
	dbs.exportErr = io.ErrUnexpectedEOF

	if _, err := svc.Create(t.Context(), "S1", "", ""); err == nil {
		t.Fatal("a backup with a failed dump succeeded")
	}
	if len(repo.recs) != 0 {
		t.Error("a record was written for a failed backup")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.enc")); len(m) != 0 {
		t.Errorf("orphan sealed objects remain: %v", m)
	}
}

// Deleting a backup removes the dump object with the tree object.
func TestDeleteRemovesTheDBObjectToo(t *testing.T) {
	svc, repo, _, _, dir := newTestService(t)
	repo.recs = []Record{{
		UID: "B1", SiteID: 1, Level: LevelFull, Target: TargetLocal,
		RemoteKey: "sites/S1/B1.enc", DBKey: "sites/S1/B1.db.enc",
	}}
	for _, name := range []string{"B1.enc", "B1.db.enc"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Delete(t.Context(), "S1", "B1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, name := range []string{"B1.enc", "B1.db.enc"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived the delete", name)
		}
	}
}

// StageDBDump opens the sealed dump into the import staging path; a backup
// without a dump refuses.
func TestStageDBDump(t *testing.T) {
	svc, repo, _, _, dir := newTestService(t)
	// Seal a dump by hand where the local target will find it.
	var sealed strings.Builder
	if err := blobcrypt.Seal(&sealed, strings.NewReader("SQLDUMP"), svc.key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "B1.db.enc"), []byte(sealed.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	repo.recs = []Record{
		{UID: "B1", SiteID: 1, Target: TargetLocal, DBKey: "sites/S1/B1.db.enc", DBName: "shopdb"},
		{UID: "B2", SiteID: 1, Target: TargetLocal},
	}

	path, file, name, err := svc.StageDBDump(t.Context(), "S1", "B1")
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if file != "import-1.sql.gz" || name != "shopdb" {
		t.Errorf("file=%q name=%q", file, name)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "SQLDUMP" {
		t.Errorf("staged dump = %q, %v", got, err)
	}

	if _, _, _, err := svc.StageDBDump(t.Context(), "S1", "B2"); err == nil {
		t.Error("a backup with no dump staged something")
	}
}

// ── panel self-backup ────────────────────────────────────────────────────────

type memPanelRepo struct{ recs []PanelRecord }

func (m *memPanelRepo) InsertPanel(_ context.Context, r *PanelRecord) error {
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	m.recs = append(m.recs, *r)
	return nil
}
func (m *memPanelRepo) ListPanel(context.Context) ([]PanelRecord, error) {
	return append([]PanelRecord(nil), m.recs...), nil
}
func (m *memPanelRepo) DeletePanel(_ context.Context, uid string) error {
	for i := range m.recs {
		if m.recs[i].UID == uid {
			m.recs = append(m.recs[:i], m.recs[i+1:]...)
			return nil
		}
	}
	return nil
}

// A panel snapshot is sealed before storage, the plaintext is gone after the
// call, and retention prunes oldest-first.
func TestPanelBackupSealsAndPrunes(t *testing.T) {
	svc, _, _, _, dir := newTestService(t)
	repo := &memPanelRepo{}
	var snapped []string
	svc.WithPanel(repo, func(_ context.Context, d string) (string, error) {
		p := filepath.Join(d, "panel-snap.tar.gz")
		snapped = append(snapped, p)
		return p, os.WriteFile(p, []byte("PANELDB"), 0o600)
	}, PanelPolicy{Keep: 2})

	var uids []string
	for i := 0; i < 3; i++ {
		b, err := svc.CreatePanelBackup(t.Context())
		if err != nil {
			t.Fatalf("panel backup %d: %v", i, err)
		}
		uids = append(uids, b.UID)
	}

	// Keep=2: the first snapshot is pruned, row and object both.
	if len(repo.recs) != 2 {
		t.Fatalf("kept %d records, want 2", len(repo.recs))
	}
	if _, err := os.Stat(filepath.Join(dir, uids[0]+".enc")); !os.IsNotExist(err) {
		t.Error("the pruned snapshot's object survived")
	}
	// The kept objects are ciphertext and open back to the snapshot bytes.
	blob, err := os.ReadFile(filepath.Join(dir, uids[2]+".enc"))
	if err != nil {
		t.Fatalf("kept object missing: %v", err)
	}
	if !strings.HasPrefix(string(blob), "HPB1") {
		t.Error("the stored panel snapshot is not blobcrypt ciphertext")
	}
	var out strings.Builder
	if err := blobcrypt.Open(&out, strings.NewReader(string(blob)), svc.key); err != nil || out.String() != "PANELDB" {
		t.Errorf("panel snapshot round trip = %q, %v", out.String(), err)
	}
	// No plaintext snapshot outlives a call.
	for _, p := range snapped {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("plaintext snapshot %s outlived the call", p)
		}
	}

	// Delete removes the object with the row.
	if err := svc.DeletePanelBackup(t.Context(), uids[2]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, uids[2]+".enc")); !os.IsNotExist(err) {
		t.Error("the deleted snapshot's object survived")
	}
	if err := svc.DeletePanelBackup(t.Context(), "nope"); err == nil {
		t.Error("deleting an unknown panel backup succeeded")
	}
}

// SetConfig refuses a policy naming a database that does not exist.
func TestSetConfigValidatesTheDatabase(t *testing.T) {
	svc, _, _, dbs, _ := newTestService(t)
	cfg := Config{Enabled: true, IntervalHours: 24, Target: TargetLocal, KeepChains: 2, DBUID: "NOPE"}

	dbs.resolveErr = os.ErrNotExist
	if err := svc.SetConfig(t.Context(), "S1", cfg); err == nil {
		t.Error("a config naming a missing database was accepted")
	}
	dbs.resolveErr = nil
	if err := svc.SetConfig(t.Context(), "S1", cfg); err != nil {
		t.Errorf("a valid db config was refused: %v", err)
	}
	svc.dbs = nil
	if err := svc.SetConfig(t.Context(), "S1", cfg); err == nil {
		t.Error("a db config was accepted with no database module")
	}
}
