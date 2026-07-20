package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// ── file.compress ────────────────────────────────────────────────────────────

func TestFileCompressSelectsToolAndRunsAsUser(t *testing.T) {
	cases := map[string]string{
		"backup.zip":    "/usr/bin/zip",
		"backup.tar.gz": "/bin/tar",
		"backup.tgz":    "/bin/tar",
	}
	for archive, tool := range cases {
		fr := &exec.FakeRunner{}
		in := map[string]any{
			"root": siteRoot, "username": "hps1",
			"sources": []string{"assets/a.txt", "assets/b.txt"},
			"archive": archive,
		}
		if _, err := (capabilities.FileCompress{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
			t.Fatalf("compress %s: %v", archive, err)
		}
		c := fr.Calls[0]
		assertRunAsUser(t, c, "hps1")
		if !hasToken(c, tool) {
			t.Errorf("%s should use %s; args = %v", archive, tool, c.Args)
		}
		// Sources are archived by *basename* from a working directory, so the
		// archive does not embed the absolute server path.
		if !hasToken(c, "a.txt") || !hasToken(c, "b.txt") {
			t.Errorf("sources missing from argv: %v", c.Args)
		}
		for _, a := range c.Args {
			if strings.Contains(a, siteRoot+"/assets/a.txt") {
				t.Errorf("sources must be relative, not absolute: %v", c.Args)
			}
		}
	}
}

func TestFileCompressRejectsUnsupportedFormat(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{
		"root": siteRoot, "username": "hps1",
		"sources": []string{"a.txt"}, "archive": "out.rar",
	}
	if _, err := (capabilities.FileCompress{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("an unsupported archive format must be refused, got %v", err)
	}
}

func TestFileCompressClampsSourcesAndRefusesMixedParents(t *testing.T) {
	// A traversing source clamps back under the root; because the clamped parent
	// then differs from the archive's working directory, it is refused rather
	// than silently archiving something else.
	fr := &exec.FakeRunner{}
	in := map[string]any{
		"root": siteRoot, "username": "hps1",
		"sources": []string{"assets/a.txt", "../../../../etc/passwd"},
		"archive": "out.zip",
	}
	if _, err := (capabilities.FileCompress{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("sources from different folders must be refused, got %v", err)
	}
	for _, c := range fr.Calls {
		for _, a := range c.Args {
			if strings.Contains(a, "/etc/passwd") && !strings.HasPrefix(a, siteRoot) {
				t.Fatalf("a source escaped the site root: %v", c.Args)
			}
		}
	}
}

func TestFileCompressRefusesEmptySelection(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "username": "hps1", "sources": []string{}, "archive": "o.zip"}
	if _, err := (capabilities.FileCompress{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("compressing nothing must be refused, got %v", err)
	}
}

// ── file.chown ───────────────────────────────────────────────────────────────

// chown is the one file op that runs as root, so what it can *target* is the
// whole security story: always the site's own user, never root, never outside
// the site root, and never through a symlink.
func TestFileChownTargetsOnlyTheSiteUser(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "public", "username": "hps1"}
	if _, err := (capabilities.FileChown{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("chown: %v", err)
	}
	c := fr.Calls[0]
	if c.Path != "/bin/chown" {
		t.Fatalf("path = %q, want /bin/chown", c.Path)
	}
	if !hasToken(c, "hps1:hps1") {
		t.Errorf("owner must be <site-user>:<site-user>; args = %v", c.Args)
	}
	if !hasToken(c, "-Rh") {
		t.Errorf("chown must not follow symlinks (-h); args = %v", c.Args)
	}
	if got := lastArg(c); got != siteRoot+"/public" {
		t.Errorf("target = %q, want the confined path", got)
	}
}

func TestFileChownRefusesRootOwner(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "x", "username": "root"}
	if _, err := (capabilities.FileChown{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("chown to root must be refused, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Errorf("no command should run when refusing root; got %v", fr.Calls)
	}
}

func TestFileChownClampsTraversal(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "../../../../etc", "username": "hps1"}
	if _, err := (capabilities.FileChown{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("chown: %v", err)
	}
	if got := lastArg(fr.Calls[0]); got != siteRoot+"/etc" {
		t.Errorf("target = %q, want it clamped under the site root (never /etc)", got)
	}
}

// ── file.search ──────────────────────────────────────────────────────────────

func TestFileSearchByNameUsesFindAsUser(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "", "username": "hps1", "query": "config"}
	if _, err := (capabilities.FileSearch{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("search: %v", err)
	}
	c := fr.Calls[0]
	assertRunAsUser(t, c, "hps1")
	if !hasToken(c, "/usr/bin/find") || !hasToken(c, "*config*") {
		t.Errorf("name search args = %v", c.Args)
	}
}

func TestFileSearchByContentUsesFixedStringGrep(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "", "username": "hps1", "query": "a.*b[", "mode": "content"}
	if _, err := (capabilities.FileSearch{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("search: %v", err)
	}
	c := fr.Calls[0]
	assertRunAsUser(t, c, "hps1")
	// -F means the term is a fixed string, so a user's search box can never
	// become a regular expression (and never a ReDoS).
	if !hasToken(c, "/bin/grep") || !hasToken(c, "-rlIFs") {
		t.Errorf("content search must use fixed-string grep; args = %v", c.Args)
	}
	if !hasToken(c, "a.*b[") {
		t.Errorf("query should be passed verbatim as one argv element; args = %v", c.Args)
	}
}

func TestFileSearchRejectsEmptyQuery(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "", "username": "hps1", "query": "   "}
	if _, err := (capabilities.FileSearch{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("an empty query must be refused, got %v", err)
	}
}

func TestFileSearchRejectsRootOutsidePolicy(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": "/etc", "path": "", "username": "hps1", "query": "passwd"}
	if _, err := (capabilities.FileSearch{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("a root outside the policy roots must be forbidden, got %v", err)
	}
}

// ── file.copy ────────────────────────────────────────────────────────────────

func TestFileCopyRunsAsUserWithConfinedPaths(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{
		"root": siteRoot, "username": "hps1",
		"from": "public/logo.png", "to": "backup/logo.png",
	}
	if _, err := (capabilities.FileCopy{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("copy: %v", err)
	}
	c := fr.Calls[0]
	assertRunAsUser(t, c, "hps1")
	if !hasToken(c, "/bin/cp") {
		t.Errorf("copy should use cp; args = %v", c.Args)
	}
	// -aT: recurse and preserve attributes, and treat the destination as the new
	// path itself. Without -T, copying onto an existing directory would land the
	// source *inside* it, at a path that was never confined.
	if !hasToken(c, "-aT") {
		t.Errorf("copy must pass -aT; args = %v", c.Args)
	}
	if !hasToken(c, siteRoot+"/public/logo.png") || !hasToken(c, siteRoot+"/backup/logo.png") {
		t.Errorf("both ends must be absolute confined paths; args = %v", c.Args)
	}
}

func TestFileCopyClampsTraversalOnBothEnds(t *testing.T) {
	for _, in := range []map[string]any{
		{"root": siteRoot, "username": "hps1", "from": "../../etc/passwd", "to": "stolen"},
		{"root": siteRoot, "username": "hps1", "from": "a.txt", "to": "../../../tmp/evil"},
	} {
		fr := &exec.FakeRunner{}
		if _, err := (capabilities.FileCopy{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
			t.Fatalf("copy: %v", err)
		}
		for _, a := range fr.Calls[0].Args {
			if strings.HasPrefix(a, "/etc") || strings.HasPrefix(a, "/tmp") {
				t.Fatalf("a traversing path escaped the site root: %v", fr.Calls[0].Args)
			}
		}
	}
}

func TestFileCopyRefusesSelfAndSubtree(t *testing.T) {
	cases := map[string]map[string]any{
		"same path": {"root": siteRoot, "username": "hps1", "from": "public", "to": "public"},
		// Copying a folder into its own subtree recurses until the disk fills.
		"into itself": {"root": siteRoot, "username": "hps1", "from": "public", "to": "public/nested/copy"},
	}
	for name, in := range cases {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.FileCopy{}).Execute(gitCtx(fr), raw(t, in))
		if !errx.IsKind(err, errx.KindValidation) {
			t.Errorf("%s: want a validation error, got %v", name, err)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("%s: nothing should be executed; got %v", name, fr.Calls)
		}
	}
}

func TestFileCopyRejectsBadUsername(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "username": "root; rm -rf /", "from": "a", "to": "b"}
	if _, err := (capabilities.FileCopy{}).Execute(gitCtx(fr), raw(t, in)); err == nil {
		t.Error("an invalid username must be refused")
	}
	if len(fr.Calls) != 0 {
		t.Errorf("nothing should be executed; got %v", fr.Calls)
	}
}
