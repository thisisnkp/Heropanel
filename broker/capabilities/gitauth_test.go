package capabilities_test

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// recordFS is an in-memory FS that keeps every live file and appends a marker to
// a shared event log, so a test can assert not just *what* happened but in what
// order relative to the commands that ran.
type recordFS struct {
	mu    sync.Mutex
	files map[string][]byte
	log   *[]string
}

func newRecordFS(log *[]string) *recordFS {
	return &recordFS{files: map[string][]byte{}, log: log}
}

func (f *recordFS) MkdirAll(string, fs.FileMode) error { return nil }

func (f *recordFS) WriteFile(path string, data []byte, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = append([]byte(nil), data...)
	return nil
}

func (f *recordFS) ReadFile(path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (f *recordFS) Remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, path)
	return nil
}

func (f *recordFS) RemoveAll(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.files {
		if k == path || strings.HasPrefix(k, path+"/") {
			delete(f.files, k)
		}
	}
	if strings.HasPrefix(path, gitAuthDir) && f.log != nil {
		*f.log = append(*f.log, "credential-removed")
	}
	return nil
}

func (f *recordFS) Exists(path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[path]
	return ok, nil
}

// live returns the paths that still exist.
func (f *recordFS) live() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.files))
	for k := range f.files {
		out = append(out, k)
	}
	return out
}

const gitAuthDir = "/run/heropanel/gitauth"

func gitAuthCtx(r exec.Runner, f *recordFS) capability.Context {
	return capability.Context{Ctx: context.Background(), Runner: r, Policy: policy.Default(), FS: f}
}

// commandText flattens a command into one searchable string, so a test can ask
// the question that actually matters: "does this secret appear anywhere a
// process listing would show it?"
func commandText(c exec.Command) string {
	return c.Path + " " + strings.Join(c.Args, " ")
}

func anyCallContains(calls []exec.Command, needle string) bool {
	for _, c := range calls {
		if strings.Contains(commandText(c), needle) {
			return true
		}
	}
	return false
}

const testToken = "ghp_TOPSECRETTOKEN"

const testKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAAB
SECRETKEYMATERIAL
-----END OPENSSH PRIVATE KEY-----`

func tokenInput() map[string]any {
	in := mutate(validDeployInput(), "repo_url", "https://github.com/acme/private.git")
	in["auth"] = map[string]any{"kind": "token", "username": "x-access-token", "secret": testToken}
	return in
}

func sshInput() map[string]any {
	in := mutate(validDeployInput(), "repo_url", "git@github.com:acme/private.git")
	in["auth"] = map[string]any{"kind": "ssh_key", "secret": testKey}
	return in
}

// The whole point of the file-based credential design: /proc/<pid>/cmdline is
// world-readable, so a token on a command line is a token every other site on
// the box can read.
func TestTokenNeverAppearsInArgv(t *testing.T) {
	fr, f := deployFake(), newRecordFS(nil)
	if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, tokenInput())); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if anyCallContains(fr.Calls, testToken) {
		t.Fatal("the access token leaked into a command line")
	}
	clone, ok := findCall(fr.Calls, "clone")
	if !ok {
		t.Fatalf("no clone; calls=%+v", fr.Calls)
	}
	text := commandText(clone)
	// It reached git the safe way: a credential file referenced by path.
	if !strings.Contains(text, "credential.helper=store --file="+gitAuthDir+"/") {
		t.Fatalf("clone did not use a credential-store file: %s", text)
	}
	// The reset of inherited helpers must precede ours, or a helper configured
	// elsewhere on the box could answer for this clone.
	resetAt := indexOfArg(clone.Args, "credential.helper=")
	oursAt := indexOfArgPrefix(clone.Args, "credential.helper=store")
	if resetAt < 0 || oursAt < 0 || resetAt > oursAt {
		t.Fatalf("inherited credential helpers were not reset first: %+v", clone.Args)
	}
}

// git runs as the site user, so it must be able to traverse the credential
// parent to reach its own key. A 0700 root parent breaks every private clone
// with "Identity file not accessible" — while the per-deploy directory below it
// stays 0700 and owned by the site user, so no site can read another's.
func TestCredentialDirectoryIsTraversableByTheSiteUser(t *testing.T) {
	fr, f := deployFake(), newRecordFS(nil)
	if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, sshInput())); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, ok := findCall(fr.Calls, "/bin/chmod", "0711", gitAuthDir); !ok {
		t.Fatalf("credential parent was not made traversable; calls=%+v", fr.Calls)
	}
	perDeploy, ok := findCall(fr.Calls, "/usr/bin/install", "-d", "-m", "0700", "-o", "hps1", "-g", "hps1")
	if !ok {
		t.Fatalf("per-deploy credential dir not created for the site user; calls=%+v", fr.Calls)
	}
	if !strings.Contains(commandText(perDeploy), gitAuthDir+"/") {
		t.Fatalf("per-deploy dir is not under the credential root: %+v", perDeploy.Args)
	}
	// And the key itself is 0600, owned by the site user.
	if _, ok := findCall(fr.Calls, "/usr/bin/install", "-m", "0600", "-o", "hps1", "-g", "hps1"); !ok {
		t.Fatalf("credential file not restricted to the site user; calls=%+v", fr.Calls)
	}
}

func TestSSHKeyNeverAppearsInArgv(t *testing.T) {
	fr, f := deployFake(), newRecordFS(nil)
	if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, sshInput())); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if anyCallContains(fr.Calls, "SECRETKEYMATERIAL") {
		t.Fatal("the deploy key leaked into a command line")
	}
	clone, ok := findCall(fr.Calls, "clone")
	if !ok {
		t.Fatalf("no clone; calls=%+v", fr.Calls)
	}
	text := commandText(clone)
	for _, want := range []string{
		"GIT_SSH_COMMAND=/usr/bin/ssh -i " + gitAuthDir + "/",
		"-o IdentitiesOnly=yes",
		"-o BatchMode=yes",
		"-o StrictHostKeyChecking=accept-new",
		"-o UserKnownHostsFile=" + gitAuthDir + "/",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("clone missing %q: %s", want, text)
		}
	}
}

// The credential file is owned by the site user (git runs as them), which means
// the site's own build script could simply read it. It must be gone first.
func TestCredentialIsDestroyedBeforeTheBuildRuns(t *testing.T) {
	var events []string
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if hasToken(cmd, "rev-parse") {
			return exec.Result{Stdout: []byte("abcdef0\n")}, nil
		}
		if hasToken(cmd, "-L") {
			return exec.Result{ExitCode: 1}, nil
		}
		if hasToken(cmd, "-lc") {
			events = append(events, "build-ran")
		}
		return exec.Result{}, nil
	}}
	f := newRecordFS(&events)

	if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, tokenInput())); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected a credential removal and a build, got %v", events)
	}
	if events[0] != "credential-removed" {
		t.Fatalf("the build ran while the credential was still readable: %v", events)
	}
	// And nothing is left on /run afterwards.
	for _, p := range f.live() {
		if strings.HasPrefix(p, gitAuthDir) {
			t.Fatalf("credential material survived the deploy: %s", p)
		}
	}
}

// The staged credential must not survive a failed deploy either.
func TestCredentialIsDestroyedWhenTheCloneFails(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if hasToken(cmd, "clone") {
			return exec.Result{ExitCode: 128, Stderr: []byte("auth failed")}, nil
		}
		return exec.Result{}, nil
	}}
	f := newRecordFS(nil)
	if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, tokenInput())); !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("want upstream error, got %v", err)
	}
	for _, p := range f.live() {
		if strings.HasPrefix(p, gitAuthDir) {
			t.Fatalf("credential material survived a failed clone: %s", p)
		}
	}
}

// The broker re-validates independently of hpd: an SSH remote is only legal
// when a deploy key came with it, and vice versa.
func TestBrokerRejectsURLAuthMismatch(t *testing.T) {
	cases := map[string]map[string]any{
		"ssh url, no auth":    mutate(validDeployInput(), "repo_url", "git@github.com:a/b.git"),
		"ssh url, token auth": mutate(tokenInput(), "repo_url", "git@github.com:a/b.git"),
		"https url, ssh key":  mutate(sshInput(), "repo_url", "https://github.com/a/b.git"),
		"ssh url traversal":   mutate(sshInput(), "repo_url", "git@github.com:../../etc/x"),
		"scp url with spaces": mutate(sshInput(), "repo_url", "git@github.com:a/b c.git"),
		"unknown auth kind":   withAuth(validDeployInput(), map[string]any{"kind": "kerberos", "secret": "x"}),
		"auth with no secret": withAuth(validDeployInput(), map[string]any{"kind": "token", "secret": ""}),
	}
	for name, in := range cases {
		fr, f := deployFake(), newRecordFS(nil)
		if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation, got %v", name, err)
		}
		if _, ok := findCall(fr.Calls, "clone"); ok {
			t.Fatalf("%s: a clone ran despite invalid input", name)
		}
	}
}

func TestSSHURLFormsAccepted(t *testing.T) {
	for _, u := range []string{
		"git@github.com:acme/app.git",
		"ssh://git@github.com/acme/app.git",
		"git@gitlab.example.com:group/sub/app.git",
		// An absolute scp-style path — what a self-hosted remote looks like.
		"git@127.0.0.1:/srv/git/app.git",
		"git@git.example.com:/home/git/repos/app.git",
	} {
		fr, f := deployFake(), newRecordFS(nil)
		if _, err := (capabilities.GitDeploy{}).Execute(gitAuthCtx(fr, f), raw(t, mutate(sshInput(), "repo_url", u))); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
}

func withAuth(in map[string]any, auth map[string]any) map[string]any {
	cp := mutate(in, "auth", auth)
	return cp
}

func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func indexOfArgPrefix(args []string, prefix string) int {
	for i, a := range args {
		if strings.HasPrefix(a, prefix) {
			return i
		}
	}
	return -1
}
