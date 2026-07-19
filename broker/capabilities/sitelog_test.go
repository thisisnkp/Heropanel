package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestSiteReadLogTailsTheRightFile(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte("line one\nline two\n")}}
	res, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"root": "/srv/heropanel/sites/1", "kind": "access", "lines": 20,
	}))
	if err != nil {
		t.Fatalf("read_log: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("got %d commands, want 1", len(fr.Calls))
	}
	cmd := fr.Calls[0]
	if cmd.Path != "/usr/bin/tail" {
		t.Errorf("path = %q", cmd.Path)
	}
	// The "--" matters: without it a path that began with "-" would be read as a
	// flag. Paths here are policy-confined, but the habit is the point.
	want := []string{"-n", "20", "--", "/srv/heropanel/sites/1/logs/access.log"}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}
	if res.Data["content"] != "line one\nline two\n" {
		t.Errorf("content = %v", res.Data["content"])
	}
	if res.Data["lines"] != 2 {
		t.Errorf("lines = %v, want 2", res.Data["lines"])
	}
	if res.Data["exists"] != true {
		t.Errorf("exists = %v, want true", res.Data["exists"])
	}
}

func TestSiteReadLogMapsErrorKind(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte("")}}
	if _, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"root": "/srv/heropanel/sites/1", "kind": "error", "lines": 5,
	})); err != nil {
		t.Fatalf("read_log: %v", err)
	}
	if got := fr.Calls[0].Args[3]; got != "/srv/heropanel/sites/1/logs/error.log" {
		t.Errorf("path = %q, want the error log", got)
	}
}

// The kind is concatenated into a path. Anything but an allowlist here would
// make this capability an arbitrary-file-read running as root.
func TestSiteReadLogRejectsAKindOutsideTheAllowlist(t *testing.T) {
	for _, kind := range []string{
		"../../../../etc/shadow",
		"access.log",
		"../../.ssh/id_rsa",
		"",
		"ACCESS",
	} {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
			"root": "/srv/heropanel/sites/1", "kind": kind, "lines": 10,
		}))
		if err == nil {
			t.Errorf("accepted kind %q", kind)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("kind %q reached the runner", kind)
		}
	}
}

func TestSiteReadLogConfinesTheRoot(t *testing.T) {
	for _, root := range []string{"/etc", "/srv/heropanel/sites/../../etc", "/root", "relative/path"} {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
			"root": root, "kind": "access", "lines": 10,
		}))
		if err == nil {
			t.Errorf("accepted root %q", root)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("root %q reached the runner", root)
		}
	}
}

func TestSiteReadLogBoundsTheTailDepth(t *testing.T) {
	for _, lines := range []int{-1, 5001, 1 << 20} {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
			"root": "/srv/heropanel/sites/1", "kind": "access", "lines": lines,
		}))
		if err == nil {
			t.Errorf("accepted lines=%d", lines)
		}
	}
}

func TestSiteReadLogDefaultsTheTailDepth(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{Stdout: []byte("x\n")}}
	if _, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"root": "/srv/heropanel/sites/1", "kind": "access",
	})); err != nil {
		t.Fatalf("read_log: %v", err)
	}
	if fr.Calls[0].Args[1] != "200" {
		t.Errorf("default lines = %q, want 200", fr.Calls[0].Args[1])
	}
}

// A site nobody has visited has no log file. tail exits non-zero; that is not a
// fault to report, it is "no requests yet".
func TestSiteReadLogTreatsAMissingFileAsEmpty(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{ExitCode: 1, Stderr: []byte("No such file or directory")}}
	res, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"root": "/srv/heropanel/sites/1", "kind": "access", "lines": 10,
	}))
	if err != nil {
		t.Fatalf("read_log on a missing file returned an error: %v", err)
	}
	if res.Data["exists"] != false {
		t.Errorf("exists = %v, want false", res.Data["exists"])
	}
	if res.Data["content"] != "" {
		t.Errorf("content = %v, want empty", res.Data["content"])
	}
}

func TestSiteReadLogRejectsBadInput(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.SiteReadLog{}).Execute(sliceCtx(fr, fsys.NewFake()), []byte(`{"root":`))
	if err == nil {
		t.Fatal("accepted malformed JSON")
	}
	if !errx.IsKind(err, errx.KindValidation) {
		t.Errorf("kind = %v, want validation", errx.KindOf(err))
	}
}
