package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

const siteRoot = "/srv/heropanel/sites/1"

// lastArg returns a command's final argument (the target path for most file ops).
func lastArg(c exec.Command) string {
	if len(c.Args) == 0 {
		return ""
	}
	return c.Args[len(c.Args)-1]
}

// assertRunAsUser checks the command is the no-shell runuser wrapper for the
// given user: `runuser -u <user> -- /usr/bin/env <tool> …`. This is the core
// security invariant — every file op drops to the site's uid and never touches a
// shell.
func assertRunAsUser(t *testing.T, c exec.Command, user string) {
	t.Helper()
	if c.Path != "/usr/sbin/runuser" {
		t.Fatalf("path = %q, want /usr/sbin/runuser (ops must drop to the site user)", c.Path)
	}
	if len(c.Args) < 4 || c.Args[0] != "-u" || c.Args[1] != user || c.Args[2] != "--" || c.Args[3] != "/usr/bin/env" {
		t.Fatalf("args = %v, want [-u %s -- /usr/bin/env …]", c.Args, user)
	}
}

func TestFileWriteClampsTraversalAndRunsAsUser(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{
		"root": siteRoot, "path": "../../etc/passwd", "username": "hps1",
		"content": []byte("pwned"),
	}
	if _, err := (capabilities.FileWrite{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(fr.Calls))
	}
	c := fr.Calls[0]
	assertRunAsUser(t, c, "hps1")
	// The traversal is clamped *under* the site root, never /etc/passwd.
	if got := lastArg(c); got != siteRoot+"/etc/passwd" {
		t.Errorf("write target = %q, want it clamped under the site root", got)
	}
	if !hasToken(c, "/usr/bin/tee") {
		t.Errorf("write should use tee; args = %v", c.Args)
	}
	// The bytes go through stdin, never argv (argv is world-readable via /proc).
	if string(c.Stdin) != "pwned" {
		t.Errorf("stdin = %q, want the content", c.Stdin)
	}
	for _, a := range c.Args {
		if strings.Contains(a, "pwned") {
			t.Errorf("content leaked into argv: %v", c.Args)
		}
	}
}

func TestFileWriteAppendFlag(t *testing.T) {
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "path": "up.bin", "username": "hps1", "content": []byte("x"), "append": true}
	if _, err := (capabilities.FileWrite{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !hasToken(fr.Calls[0], "-a") {
		t.Errorf("append write must pass tee -a; args = %v", fr.Calls[0].Args)
	}
}

func TestFileReadChunkAndEOF(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte("hello"), ExitCode: 0}}
	in := map[string]any{"root": siteRoot, "path": "index.php", "username": "hps1", "offset": 10, "length": 100}
	res, err := (capabilities.FileRead{}).Execute(gitCtx(fr), raw(t, in))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	c := fr.Calls[0]
	assertRunAsUser(t, c, "hps1")
	if !hasToken(c, "/bin/dd") || !hasToken(c, "if="+siteRoot+"/index.php") ||
		!hasToken(c, "skip=10") || !hasToken(c, "bs=100") || !hasToken(c, "iflag=skip_bytes,fullblock") {
		t.Errorf("dd read args = %v", c.Args)
	}
	// count_bytes would make count=1 mean a single byte and truncate every read;
	// it must never appear in the dd invocation.
	for _, a := range c.Args {
		if strings.Contains(a, "count_bytes") {
			t.Errorf("dd read must not use count_bytes (it truncates to 1 byte): %v", c.Args)
		}
	}
	// 5 bytes read for a 100-byte request => end of file.
	if res.Data["eof"] != true {
		t.Errorf("eof = %v, want true (short read)", res.Data["eof"])
	}
	if string(res.Data["content"].([]byte)) != "hello" {
		t.Errorf("content = %v", res.Data["content"])
	}
}

func TestFileExtractSelectsToolBySuffix(t *testing.T) {
	cases := map[string]string{
		"backup.zip":     "/usr/bin/unzip",
		"src.tar.gz":     "/bin/tar",
		"src.tgz":        "/bin/tar",
		"archive.tar.xz": "/bin/tar",
	}
	for name, tool := range cases {
		fr := &exec.FakeRunner{}
		in := map[string]any{"root": siteRoot, "archive": name, "dest": "out", "username": "hps1"}
		if _, err := (capabilities.FileExtract{}).Execute(gitCtx(fr), raw(t, in)); err != nil {
			t.Fatalf("extract %s: %v", name, err)
		}
		// Call 0 is mkdir -p dest; the extractor is the last call.
		last := fr.Calls[len(fr.Calls)-1]
		if !hasToken(last, tool) {
			t.Errorf("%s should extract with %s; args = %v", name, tool, last.Args)
		}
	}
	// An unknown/dangerous suffix is refused, not shelled out.
	fr := &exec.FakeRunner{}
	in := map[string]any{"root": siteRoot, "archive": "evil.sh", "dest": "out", "username": "hps1"}
	if _, err := (capabilities.FileExtract{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("unsupported archive should be rejected, got %v", err)
	}
}

func TestFileChmodValidatesMode(t *testing.T) {
	fr := &exec.FakeRunner{}
	bad := map[string]any{"root": siteRoot, "path": "x", "username": "hps1", "mode": "999"}
	if _, err := (capabilities.FileChmod{}).Execute(gitCtx(fr), raw(t, bad)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("mode 999 should be rejected, got %v", err)
	}
	good := map[string]any{"root": siteRoot, "path": "x", "username": "hps1", "mode": "0644"}
	if _, err := (capabilities.FileChmod{}).Execute(gitCtx(fr), raw(t, good)); err != nil {
		t.Fatalf("chmod 0644: %v", err)
	}
	if !hasToken(fr.Calls[0], "/bin/chmod") || !hasToken(fr.Calls[0], "0644") {
		t.Errorf("chmod args = %v", fr.Calls[0].Args)
	}
}

func TestFileRemoveRefusesSiteRoot(t *testing.T) {
	fr := &exec.FakeRunner{}
	// An empty path clamps to the site root itself, which must never be rm -rf'd.
	in := map[string]any{"root": siteRoot, "path": "", "username": "hps1"}
	if _, err := (capabilities.FileRemove{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("removing the site root must be refused, got %v", err)
	}
	if len(fr.Calls) != 0 {
		t.Errorf("no command should run when refusing the root; got %v", fr.Calls)
	}
}

func TestFileOpRejectsRootOutsidePolicy(t *testing.T) {
	fr := &exec.FakeRunner{}
	// A root that is not under an allowed policy root is forbidden outright.
	in := map[string]any{"root": "/etc", "path": "passwd", "username": "hps1"}
	if _, err := (capabilities.FileList{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindForbidden) {
		t.Errorf("a root outside the policy roots must be forbidden, got %v", err)
	}
}
