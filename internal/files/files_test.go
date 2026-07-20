package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeBroker records every Invoke and replies from a per-capability queue of
// canned results, so a test can both assert the payloads sent and drive the
// service's chunk-looping.
type fakeBroker struct {
	calls   []brokerCall
	replies map[string][]map[string]any
	err     error
}

type brokerCall struct {
	capability string
	input      map[string]any
}

func (b *fakeBroker) Invoke(_ context.Context, capability string, input any) (map[string]any, error) {
	m, _ := input.(map[string]any)
	b.calls = append(b.calls, brokerCall{capability: capability, input: m})
	if b.err != nil {
		return nil, b.err
	}
	q := b.replies[capability]
	if len(q) == 0 {
		return map[string]any{}, nil
	}
	res := q[0]
	b.replies[capability] = q[1:]
	return res, nil
}

// fakeSites is a fixed one-site resolver.
type fakeSites struct {
	ref *SiteRef
	err error
}

func (s fakeSites) Resolve(_ context.Context, _ string) (*SiteRef, error) {
	return s.ref, s.err
}

func baremetalRef() *SiteRef {
	return &SiteRef{ID: 1, UID: "site1", LinuxUser: "hps1", HomeDir: "/srv/heropanel/sites/1", DeployMode: "baremetal"}
}

func b64(s string) any { return base64.StdEncoding.EncodeToString([]byte(s)) }

// ── the baremetal gate ────────────────────────────────────────────────────────

func TestGateRejectsNonBaremetal(t *testing.T) {
	for _, mode := range []string{"git", "docker", ""} {
		ref := baremetalRef()
		ref.DeployMode = mode
		br := &fakeBroker{}
		svc := NewService(fakeSites{ref: ref}, br)
		if _, err := svc.List(context.Background(), "site1", ""); !errx.IsKind(err, errx.KindForbidden) {
			t.Errorf("deploy_mode %q: want forbidden, got %v", mode, err)
		}
		if len(br.calls) != 0 {
			t.Errorf("deploy_mode %q: the broker must not be called when the gate refuses; got %v", mode, br.calls)
		}
	}
}

func TestGateRejectsUnprovisionedSite(t *testing.T) {
	ref := baremetalRef()
	ref.HomeDir = "" // provisioning has not populated the home yet
	svc := NewService(fakeSites{ref: ref}, &fakeBroker{})
	if _, err := svc.List(context.Background(), "site1", ""); !errx.IsKind(err, errx.KindUnavailable) {
		t.Errorf("want unavailable for an unprovisioned site, got %v", err)
	}
}

func TestNoBrokerIsUnavailable(t *testing.T) {
	svc := NewService(fakeSites{ref: baremetalRef()}, nil)
	if _, err := svc.List(context.Background(), "site1", ""); !errx.IsKind(err, errx.KindUnavailable) {
		t.Errorf("want unavailable when the broker is not configured, got %v", err)
	}
}

// ── broker payloads ───────────────────────────────────────────────────────────

func TestListSendsRootPathUsername(t *testing.T) {
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.list": {{
			"path": "/srv/heropanel/sites/1/pub",
			"entries": []any{
				map[string]any{"name": "index.php", "kind": "file", "size": float64(42), "mode": "644", "mtime": float64(1700000000)},
				map[string]any{"name": "css", "kind": "dir", "size": float64(0), "mode": "755", "mtime": float64(1700000001)},
			},
		}},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	out, err := svc.List(context.Background(), "site1", "pub")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(br.calls) != 1 || br.calls[0].capability != "file.list" {
		t.Fatalf("calls = %v", br.calls)
	}
	in := br.calls[0].input
	if in["root"] != "/srv/heropanel/sites/1" || in["path"] != "pub" || in["username"] != "hps1" {
		t.Errorf("payload = %v, want root/path/username of the resolved site", in)
	}
	if out.Path != "/srv/heropanel/sites/1/pub" || len(out.Entries) != 2 {
		t.Fatalf("listing = %+v", out)
	}
	// float64 (JSON) sizes/mtimes are converted to int64.
	if out.Entries[0].Name != "index.php" || out.Entries[0].Size != 42 || out.Entries[0].Mode != "644" || out.Entries[0].Mtime != 1700000000 {
		t.Errorf("entry[0] = %+v", out.Entries[0])
	}
	if out.Entries[1].Kind != "dir" {
		t.Errorf("entry[1] kind = %q, want dir", out.Entries[1].Kind)
	}
}

func TestRenamePayload(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if err := svc.Rename(context.Background(), "site1", "a.txt", "b.txt"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	in := br.calls[0].input
	if br.calls[0].capability != "file.rename" || in["from"] != "a.txt" || in["to"] != "b.txt" || in["username"] != "hps1" {
		t.Errorf("rename payload = %v", in)
	}
}

func TestChmodPayload(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if err := svc.Chmod(context.Background(), "site1", "x", "640"); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	in := br.calls[0].input
	if br.calls[0].capability != "file.chmod" || in["path"] != "x" || in["mode"] != "640" {
		t.Errorf("chmod payload = %v", in)
	}
}

func TestExtractPayload(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if err := svc.Extract(context.Background(), "site1", "b.zip", "out"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	in := br.calls[0].input
	if br.calls[0].capability != "file.extract" || in["archive"] != "b.zip" || in["dest"] != "out" {
		t.Errorf("extract payload = %v", in)
	}
}

func TestCompressPayload(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if err := svc.Compress(context.Background(), "site1", []string{"a.txt", "b.txt"}, "out.zip", "zip"); err != nil {
		t.Fatalf("compress: %v", err)
	}
	in := br.calls[0].input
	if br.calls[0].capability != "file.compress" || in["archive"] != "out.zip" || in["format"] != "zip" {
		t.Errorf("compress payload = %v", in)
	}
	srcs, _ := in["sources"].([]string)
	if len(srcs) != 2 || srcs[0] != "a.txt" {
		t.Errorf("sources = %v", in["sources"])
	}
}

// FixOwnership must never let the caller name the target account — the broker
// derives it from the resolved site user.
func TestFixOwnershipSendsOnlyTheSiteUser(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if err := svc.FixOwnership(context.Background(), "site1", "public"); err != nil {
		t.Fatalf("chown: %v", err)
	}
	in := br.calls[0].input
	if br.calls[0].capability != "file.chown" || in["username"] != "hps1" || in["path"] != "public" {
		t.Errorf("chown payload = %v", in)
	}
	if _, hasOwner := in["owner"]; hasOwner {
		t.Error("the payload must not carry a caller-chosen owner")
	}
}

func TestSearchParsesResults(t *testing.T) {
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.search": {{
			"path":      "/srv/heropanel/sites/1",
			"truncated": true,
			"entries": []any{
				map[string]any{"name": "config.php", "path": "app/config.php", "kind": "file", "size": float64(120)},
				map[string]any{"name": "cache", "path": "var/cache", "kind": "dir"},
			},
		}},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	out, err := svc.Search(context.Background(), "site1", "", "config", "name")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	in := br.calls[0].input
	if in["query"] != "config" || in["mode"] != "name" {
		t.Errorf("search payload = %v", in)
	}
	if !out.Truncated || len(out.Entries) != 2 {
		t.Fatalf("results = %+v", out)
	}
	if out.Entries[0].Path != "app/config.php" || out.Entries[0].Size != 120 {
		t.Errorf("entry[0] = %+v", out.Entries[0])
	}
	if out.Entries[1].Kind != "dir" {
		t.Errorf("entry[1] kind = %q, want dir", out.Entries[1].Kind)
	}
}

func TestNewOpsRespectTheBaremetalGate(t *testing.T) {
	ref := baremetalRef()
	ref.DeployMode = "git"
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: ref}, br)

	if err := svc.Compress(context.Background(), "site1", []string{"a"}, "o.zip", "zip"); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("compress on a git site: want forbidden, got %v", err)
	}
	if err := svc.FixOwnership(context.Background(), "site1", "a"); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("chown on a git site: want forbidden, got %v", err)
	}
	if _, err := svc.Search(context.Background(), "site1", "", "q", "name"); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("search on a git site: want forbidden, got %v", err)
	}
	if len(br.calls) != 0 {
		t.Errorf("the broker must not be called when the gate refuses; got %v", br.calls)
	}
}

// ── base64 round-trip + streaming ─────────────────────────────────────────────

func TestDownloadDecodesAndStreamsUntilEOF(t *testing.T) {
	// Two chunks: the broker returns base64 (as Go marshals []byte), and the
	// second chunk is short → eof. Download must decode and concatenate both.
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.read": {
			{"content": b64("first-half-"), "eof": false},
			{"content": b64("second-half"), "eof": true},
		},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	var buf bytes.Buffer
	n, err := svc.Download(context.Background(), "site1", "f.txt", &buf)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if buf.String() != "first-half-second-half" {
		t.Errorf("downloaded %q", buf.String())
	}
	if n != int64(buf.Len()) {
		t.Errorf("byte count = %d, want %d", n, buf.Len())
	}
	// Successive reads advance the offset by the bytes already read.
	if br.calls[0].input["offset"] != int64(0) || br.calls[1].input["offset"] != int64(11) {
		t.Errorf("offsets = %v, %v", br.calls[0].input["offset"], br.calls[1].input["offset"])
	}
}

func TestDownloadRawBytesAreBinarySafe(t *testing.T) {
	raw := []byte{0x00, 0xff, 0x10, 0x00, 0x7f}
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.read": {{"content": base64.StdEncoding.EncodeToString(raw), "eof": true}},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	var buf bytes.Buffer
	if _, err := svc.Download(context.Background(), "site1", "f.bin", &buf); err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), raw) {
		t.Errorf("binary content round-trip failed: %v", buf.Bytes())
	}
}

func TestUploadChunksTruncateThenAppend(t *testing.T) {
	// A body larger than one chunk must be a single truncating write followed by
	// appends, so a re-upload replaces rather than grows the file.
	body := bytes.Repeat([]byte("A"), ChunkSize+100)
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	n, err := svc.Upload(context.Background(), "site1", "up.bin", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if n != int64(len(body)) {
		t.Errorf("byte count = %d, want %d", n, len(body))
	}
	if len(br.calls) != 2 {
		t.Fatalf("want 2 write calls (chunk + remainder), got %d", len(br.calls))
	}
	if br.calls[0].input["append"] != false {
		t.Errorf("first chunk must truncate (append=false); got %v", br.calls[0].input["append"])
	}
	if br.calls[1].input["append"] != true {
		t.Errorf("later chunks must append; got %v", br.calls[1].input["append"])
	}
	// The full body was reassembled across the two chunks.
	var got bytes.Buffer
	for _, c := range br.calls {
		got.Write(c.input["content"].([]byte))
	}
	if !bytes.Equal(got.Bytes(), body) {
		t.Errorf("reassembled upload differs from the source body (%d vs %d bytes)", got.Len(), len(body))
	}
}

func TestUploadEmptyBodyStillTruncates(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	n, err := svc.Upload(context.Background(), "site1", "empty.txt", strings.NewReader(""))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if n != 0 {
		t.Errorf("byte count = %d, want 0", n)
	}
	// Even an empty body must issue one truncating write so "clear the file" works.
	if len(br.calls) != 1 || br.calls[0].input["append"] != false {
		t.Errorf("want a single truncating write for an empty body; got %v", br.calls)
	}
}

// ── error passthrough ─────────────────────────────────────────────────────────

func TestBrokerErrorPropagates(t *testing.T) {
	br := &fakeBroker{err: errx.New(errx.KindUpstream, "boom", "broker exploded")}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if err := svc.Mkdir(context.Background(), "site1", "d"); !errx.IsKind(err, errx.KindUpstream) {
		t.Errorf("broker error should propagate, got %v", err)
	}
}

func TestResolveErrorPropagates(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{err: errx.NotFound("no_site", "no such site")}, br)
	if _, err := svc.List(context.Background(), "missing", ""); !errx.IsKind(err, errx.KindNotFound) {
		t.Errorf("resolve error should propagate, got %v", err)
	}
	if len(br.calls) != 0 {
		t.Errorf("no broker call when the site cannot be resolved; got %v", br.calls)
	}
}

// ── copy & move ──────────────────────────────────────────────────────────────

// listingReply builds a file.list reply holding the given names, so a test can
// control what the destination's parent directory already contains.
func listingReply(names ...string) map[string]any {
	entries := make([]any, 0, len(names))
	for _, n := range names {
		entries = append(entries, map[string]any{"name": n, "kind": "file"})
	}
	return map[string]any{"path": "/srv/heropanel/sites/1", "entries": entries}
}

func TestCopySendsConfinedPayloadWhenDestinationIsFree(t *testing.T) {
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.list": {listingReply("other.txt")},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)

	dest, err := svc.Copy(context.Background(), "site1", "public/logo.png", "backup/logo.png", ConflictFail)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if dest != "backup/logo.png" {
		t.Errorf("dest = %q, want the requested path when nothing is in the way", dest)
	}
	last := br.calls[len(br.calls)-1]
	if last.capability != "file.copy" {
		t.Fatalf("last call = %q, want file.copy", last.capability)
	}
	if last.input["from"] != "public/logo.png" || last.input["to"] != "backup/logo.png" {
		t.Errorf("payload = %v, want the requested from/to", last.input)
	}
	if last.input["username"] != "hps1" || last.input["root"] != "/srv/heropanel/sites/1" {
		t.Errorf("payload must carry the site's user and root; got %v", last.input)
	}
}

// The default is to refuse rather than clobber: `cp` would overwrite silently,
// and a paste that destroys a file the operator forgot about is the failure this
// whole check exists to prevent.
func TestCopyAndMoveRefuseToOverwriteByDefault(t *testing.T) {
	for _, op := range []string{"copy", "move"} {
		br := &fakeBroker{replies: map[string][]map[string]any{
			"file.list": {listingReply("logo.png")},
		}}
		svc := NewService(fakeSites{ref: baremetalRef()}, br)

		var err error
		if op == "copy" {
			_, err = svc.Copy(context.Background(), "site1", "public/logo.png", "logo.png", ConflictFail)
		} else {
			_, err = svc.Move(context.Background(), "site1", "public/logo.png", "logo.png", ConflictFail)
		}
		if !errx.IsKind(err, errx.KindConflict) {
			t.Errorf("%s onto an existing name: want a conflict, got %v", op, err)
		}
		for _, c := range br.calls {
			if c.capability == "file.copy" || c.capability == "file.rename" {
				t.Errorf("%s: nothing should have been executed; got %v", op, c.capability)
			}
		}
	}
}

func TestCopyWithRenamePicksAFreeName(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		base     string
		want     string
	}{
		{"first duplicate", []string{"logo.png"}, "logo.png", "logo copy.png"},
		{"second duplicate", []string{"logo.png", "logo copy.png"}, "logo.png", "logo copy 2.png"},
		// A compound archive suffix stays whole: "site.tar copy.gz" would not be
		// recognised as an archive by anything.
		{"compound suffix", []string{"site.tar.gz"}, "site.tar.gz", "site copy.tar.gz"},
		// A leading dot is a dotfile, not an extension.
		{"dotfile", []string{".env"}, ".env", ".env copy"},
		{"no extension", []string{"Makefile"}, "Makefile", "Makefile copy"},
	}
	for _, tc := range cases {
		br := &fakeBroker{replies: map[string][]map[string]any{
			"file.list": {listingReply(tc.existing...)},
		}}
		svc := NewService(fakeSites{ref: baremetalRef()}, br)

		dest, err := svc.Copy(context.Background(), "site1", tc.base, tc.base, ConflictRename)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if dest != tc.want {
			t.Errorf("%s: dest = %q, want %q", tc.name, dest, tc.want)
		}
	}
}

// The free name has to keep the destination's folder, not collapse to the root.
func TestCopyWithRenameKeepsTheDestinationFolder(t *testing.T) {
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.list": {listingReply("logo.png")},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)

	dest, err := svc.Copy(context.Background(), "site1", "logo.png", "assets/logo.png", ConflictRename)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if dest != "assets/logo copy.png" {
		t.Errorf("dest = %q, want the free name inside assets/", dest)
	}
}

// Move is a rename with a destination elsewhere in the tree — there is no
// separate capability, and this pins that it stays that way.
func TestMoveUsesRenameCapability(t *testing.T) {
	br := &fakeBroker{replies: map[string][]map[string]any{
		"file.list": {listingReply()},
	}}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)

	if _, err := svc.Move(context.Background(), "site1", "a/logo.png", "b/logo.png", ConflictFail); err != nil {
		t.Fatalf("move: %v", err)
	}
	last := br.calls[len(br.calls)-1]
	if last.capability != "file.rename" {
		t.Fatalf("move should invoke file.rename, got %q", last.capability)
	}
	if last.input["from"] != "a/logo.png" || last.input["to"] != "b/logo.png" {
		t.Errorf("payload = %v", last.input)
	}
}

func TestMoveOntoItselfIsRejected(t *testing.T) {
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: baremetalRef()}, br)
	if _, err := svc.Move(context.Background(), "site1", "a.txt", "a.txt", ConflictFail); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("want a validation error, got %v", err)
	}
	if len(br.calls) != 0 {
		t.Errorf("nothing should have been sent; got %v", br.calls)
	}
}

func TestCopyAndMoveAreGatedToBaremetal(t *testing.T) {
	ref := baremetalRef()
	ref.DeployMode = "git"
	br := &fakeBroker{}
	svc := NewService(fakeSites{ref: ref}, br)

	if _, err := svc.Copy(context.Background(), "site1", "a", "b", ConflictFail); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("copy: want forbidden, got %v", err)
	}
	if _, err := svc.Move(context.Background(), "site1", "a", "b", ConflictFail); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("move: want forbidden, got %v", err)
	}
	if len(br.calls) != 0 {
		t.Errorf("the broker must not be reached at all; got %v", br.calls)
	}
}
