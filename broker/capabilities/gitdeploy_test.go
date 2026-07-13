package capabilities_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func gitCtx(r exec.Runner) capability.Context {
	return capability.Context{Ctx: context.Background(), Runner: r, Policy: policy.Default()}
}

func raw(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func hasToken(cmd exec.Command, tok string) bool {
	if cmd.Path == tok {
		return true
	}
	for _, a := range cmd.Args {
		if a == tok {
			return true
		}
	}
	return false
}

func findCall(calls []exec.Command, tokens ...string) (exec.Command, bool) {
	for _, c := range calls {
		ok := true
		for _, tk := range tokens {
			if !hasToken(c, tk) {
				ok = false
				break
			}
		}
		if ok {
			return c, true
		}
	}
	return exec.Command{}, false
}

func validDeployInput() map[string]any {
	return map[string]any{
		"username":      "hps1",
		"home":          "/srv/heropanel/sites/1",
		"repo_url":      "https://github.com/acme/app.git",
		"branch":        "main",
		"build_command": "npm ci && npm run build",
		"web_root":      "dist",
		"release_id":    "01HZZZAAAABBBBCCCCDDDDEEEE",
	}
}

// deployFake returns exit 0 for everything, a canned commit for rev-parse, and
// exit 1 for `test -L` so the first-deploy public-symlink conversion runs.
func deployFake() *exec.FakeRunner {
	return &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if hasToken(cmd, "rev-parse") {
			return exec.Result{Stdout: []byte("abcdef0\n")}, nil
		}
		if hasToken(cmd, "-L") { // test -L public → not yet a symlink
			return exec.Result{ExitCode: 1}, nil
		}
		return exec.Result{}, nil
	}}
}

func TestGitDeployHappyPathArgv(t *testing.T) {
	fr := deployFake()
	res, err := (capabilities.GitDeploy{}).Execute(gitCtx(fr), raw(t, validDeployInput()))
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if res.Data["commit"] != "abcdef0" || res.Data["activated"] != true {
		t.Fatalf("result = %+v", res.Data)
	}

	// The clone is a single shell-free argv: branch, repo, and release dir are all
	// distinct tokens, and it runs through runuser + /usr/bin/env.
	clone, ok := findCall(fr.Calls, "clone", "--single-branch", "main",
		"https://github.com/acme/app.git", "/srv/heropanel/sites/1/releases/01HZZZAAAABBBBCCCCDDDDEEEE")
	if !ok {
		t.Fatalf("clone call not found; calls=%+v", fr.Calls)
	}
	if len(clone.Args) < 4 || clone.Args[0] != "-u" || clone.Args[1] != "hps1" ||
		clone.Args[2] != "--" || clone.Args[3] != "/usr/bin/env" {
		t.Fatalf("clone not run as user via env: %+v", clone.Args)
	}

	// The build command reaches sh as ONE argument — never split or interpolated.
	if _, ok := findCall(fr.Calls, "/bin/sh", "-lc", "npm ci && npm run build"); !ok {
		t.Fatalf("build command not passed as a single arg; calls=%+v", fr.Calls)
	}

	// The activation is the atomic swap: a temp symlink renamed over `current`.
	if _, ok := findCall(fr.Calls, "-sfn", "/srv/heropanel/sites/1/releases/01HZZZAAAABBBBCCCCDDDDEEEE",
		"/srv/heropanel/sites/1/.current.tmp"); !ok {
		t.Fatalf("current.tmp symlink not created; calls=%+v", fr.Calls)
	}
	last := fr.Calls[len(fr.Calls)-1]
	if !hasToken(last, "-Tf") || !hasToken(last, "/srv/heropanel/sites/1/current") {
		t.Fatalf("final call is not the atomic rename over current: %+v", last.Args)
	}
}

func TestGitDeployRejectsBadInput(t *testing.T) {
	cases := map[string]map[string]any{
		"non-https url":    mutate(validDeployInput(), "repo_url", "http://insecure/x"),
		"url with creds":   mutate(validDeployInput(), "repo_url", "https://user:pw@github.com/a/b.git"),
		"branch injection": mutate(validDeployInput(), "branch", "main;rm -rf /"),
		"branch traversal": mutate(validDeployInput(), "branch", "../evil"),
		"web_root escape":  mutate(validDeployInput(), "web_root", "../../etc"),
		"bad release id":   mutate(validDeployInput(), "release_id", "../etc"),
	}
	for name, in := range cases {
		fr := deployFake()
		if _, err := (capabilities.GitDeploy{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
		if len(fr.Calls) != 0 {
			t.Fatalf("%s: no commands should run for invalid input, ran %d", name, len(fr.Calls))
		}
	}
}

func TestGitDeployRejectsPathOutsideRoot(t *testing.T) {
	fr := deployFake()
	in := mutate(validDeployInput(), "home", "/etc")
	if _, err := (capabilities.GitDeploy{}).Execute(gitCtx(fr), raw(t, in)); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("home outside root should be forbidden, got %v", err)
	}
}

func TestGitDeployBuildFailureCleansUp(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		if hasToken(cmd, "-lc") { // the build command fails
			return exec.Result{ExitCode: 2, Stderr: []byte("boom")}, nil
		}
		if hasToken(cmd, "rev-parse") {
			return exec.Result{Stdout: []byte("abcdef0\n")}, nil
		}
		return exec.Result{}, nil
	}}
	if _, err := (capabilities.GitDeploy{}).Execute(gitCtx(fr), raw(t, validDeployInput())); !errx.IsKind(err, errx.KindUpstream) {
		t.Fatalf("build failure should be an upstream error, got %v", err)
	}
	// The broken release is cleaned up; the live site is never touched.
	if _, ok := findCall(fr.Calls, "-rf", "/srv/heropanel/sites/1/releases/01HZZZAAAABBBBCCCCDDDDEEEE"); !ok {
		t.Fatalf("broken release was not cleaned up; calls=%+v", fr.Calls)
	}
	// No activation happened.
	if _, ok := findCall(fr.Calls, "-Tf", "/srv/heropanel/sites/1/current"); ok {
		t.Fatal("a failed build must not activate a release")
	}
}

func TestGitRollbackActivatesExistingRelease(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(cmd exec.Command) (exec.Result, error) {
		return exec.Result{}, nil // test -d returns 0 (release exists)
	}}
	res, err := (capabilities.GitRollback{}).Execute(gitCtx(fr), raw(t, map[string]any{
		"username":    "hps1",
		"home":        "/srv/heropanel/sites/1",
		"release_dir": "/srv/heropanel/sites/1/releases/01OLD",
	}))
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if res.Data["activated"] != true {
		t.Fatalf("result = %+v", res.Data)
	}
	if _, ok := findCall(fr.Calls, "-sfn", "/srv/heropanel/sites/1/releases/01OLD", "/srv/heropanel/sites/1/.current.tmp"); !ok {
		t.Fatalf("rollback did not point current at the target release; calls=%+v", fr.Calls)
	}
	last := fr.Calls[len(fr.Calls)-1]
	if !hasToken(last, "-Tf") || !hasToken(last, "/srv/heropanel/sites/1/current") {
		t.Fatalf("rollback final call is not the atomic swap: %+v", last.Args)
	}
}

func TestGitRollbackRejectsForeignRelease(t *testing.T) {
	fr := &exec.FakeRunner{}
	// A release dir that is not under this site's releases/ must be refused even
	// though it is inside the allowed root.
	if _, err := (capabilities.GitRollback{}).Execute(gitCtx(fr), raw(t, map[string]any{
		"username":    "hps1",
		"home":        "/srv/heropanel/sites/1",
		"release_dir": "/srv/heropanel/sites/1/public",
	})); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for foreign release, got %v", err)
	}
}

func mutate(m map[string]any, key string, val any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	cp[key] = val
	return cp
}
