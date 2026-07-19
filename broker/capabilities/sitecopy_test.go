package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
)

func copyInput() map[string]any {
	return map[string]any{
		"src_root": "/srv/heropanel/sites/1",
		"dst_root": "/srv/heropanel/sites/2",
		"username": "hps2",
	}
}

func mutateCopy(in map[string]any, k string, v any) map[string]any {
	out := map[string]any{}
	for key, val := range in {
		out[key] = val
	}
	out[k] = v
	return out
}

func TestSiteCopyTreeCopiesThenReowns(t *testing.T) {
	fr := &exec.FakeRunner{}
	res, err := (capabilities.SiteCopyTree{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, copyInput()))
	if err != nil {
		t.Fatalf("copy_tree: %v", err)
	}
	if len(fr.Calls) != 2 {
		t.Fatalf("got %d commands, want cp then chown", len(fr.Calls))
	}

	cp := fr.Calls[0]
	if cp.Path != "/bin/cp" {
		t.Errorf("path = %q", cp.Path)
	}
	// -a preserves modes and does not follow symlinks: a link in the source
	// pointing at /etc/shadow is copied as a link, not as the secret behind it.
	// The trailing "/." copies the contents rather than nesting public/public.
	want := []string{"-a", "--", "/srv/heropanel/sites/1/public/.", "/srv/heropanel/sites/2/public"}
	if strings.Join(cp.Args, " ") != strings.Join(want, " ") {
		t.Errorf("cp args = %v, want %v", cp.Args, want)
	}

	// Without the chown the clone's tree stays owned by the *source's* user, and
	// the two sites can read and write each other's files — the exact isolation
	// the per-site user exists to provide.
	ch := fr.Calls[1]
	if ch.Path != "/bin/chown" {
		t.Errorf("path = %q", ch.Path)
	}
	wantCh := []string{"-R", "-h", "--", "hps2:hps2", "/srv/heropanel/sites/2/public"}
	if strings.Join(ch.Args, " ") != strings.Join(wantCh, " ") {
		t.Errorf("chown args = %v, want %v", ch.Args, wantCh)
	}
	if res.Data["copied"] != true {
		t.Errorf("result = %+v", res.Data)
	}
}

func TestSiteCopyTreeConfinesBothEnds(t *testing.T) {
	bad := []string{"/etc", "/root", "/srv/heropanel/sites/../../etc", "relative"}
	for _, p := range bad {
		for _, field := range []string{"src_root", "dst_root"} {
			fr := &exec.FakeRunner{}
			_, err := (capabilities.SiteCopyTree{}).Execute(sliceCtx(fr, fsys.NewFake()),
				raw(t, mutateCopy(copyInput(), field, p)))
			if err == nil {
				t.Errorf("accepted %s = %q", field, p)
			}
			if len(fr.Calls) != 0 {
				t.Errorf("%s = %q reached the runner", field, p)
			}
		}
	}
}

func TestSiteCopyTreeRejectsCopyingASiteOntoItself(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.SiteCopyTree{}).Execute(sliceCtx(fr, fsys.NewFake()),
		raw(t, mutateCopy(copyInput(), "dst_root", "/srv/heropanel/sites/1")))
	if err == nil {
		t.Fatal("accepted a copy from a site onto itself")
	}
	if len(fr.Calls) != 0 {
		t.Error("the runner was called for a self-copy")
	}
}

func TestSiteCopyTreeValidatesTheUsername(t *testing.T) {
	for _, u := range []string{"", "root; rm -rf /", "../hps1", "hps2 hps3", "-rf"} {
		fr := &exec.FakeRunner{}
		_, err := (capabilities.SiteCopyTree{}).Execute(sliceCtx(fr, fsys.NewFake()),
			raw(t, mutateCopy(copyInput(), "username", u)))
		if err == nil {
			t.Errorf("accepted username %q", u)
		}
		if len(fr.Calls) != 0 {
			t.Errorf("username %q reached the runner", u)
		}
	}
}

func TestSiteCopyTreeFailsWhenTheCopyFails(t *testing.T) {
	fr := &exec.FakeRunner{Result: exec.Result{ExitCode: 1}}
	_, err := (capabilities.SiteCopyTree{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, copyInput()))
	if err == nil {
		t.Fatal("reported success despite cp exiting non-zero")
	}
	// Crucially, chown must not have run over a tree that was never copied.
	if len(fr.Calls) != 1 {
		t.Errorf("got %d commands, want the sequence to stop at cp", len(fr.Calls))
	}
}

func TestSiteCopyTreeFailsWhenTheReownFails(t *testing.T) {
	n := 0
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		n++
		if n == 2 {
			return exec.Result{ExitCode: 1}, nil
		}
		return exec.Result{}, nil
	}}
	// A copy that is left owned by the source's user is not a usable clone, so
	// this must surface rather than report a half-done success.
	if _, err := (capabilities.SiteCopyTree{}).Execute(sliceCtx(fr, fsys.NewFake()), raw(t, copyInput())); err == nil {
		t.Fatal("reported success despite chown exiting non-zero")
	}
}
