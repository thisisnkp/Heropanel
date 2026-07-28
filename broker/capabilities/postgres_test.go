package capabilities_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/broker/capabilities"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/fsys"
)

// pgLast returns the last command and its stdin as a string.
func pgLast(fr *exec.FakeRunner) (exec.Command, string) {
	c := fr.Calls[len(fr.Calls)-1]
	return c, string(c.Stdin)
}

func TestPGCreateRunsAsPostgres(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.PGCreate{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{"name": "acme"}))
	if err != nil {
		t.Fatalf("pg.create: %v", err)
	}
	c, stdin := pgLast(fr)
	// Everything runs as the postgres OS user via runuser (no shell), SQL on stdin.
	if c.Path != "/usr/sbin/runuser" {
		t.Errorf("path = %q, want runuser", c.Path)
	}
	argv := strings.Join(c.Args, " ")
	if !strings.Contains(argv, "-u postgres") || !strings.Contains(argv, "/usr/bin/psql") {
		t.Errorf("argv = %q, want runuser -u postgres … psql", argv)
	}
	if !strings.Contains(stdin, `CREATE DATABASE "acme"`) {
		t.Errorf("stdin = %q, want a quoted CREATE DATABASE", stdin)
	}
}

func TestPGCreateForgivesAlreadyExists(t *testing.T) {
	fr := &exec.FakeRunner{Fn: func(exec.Command) (exec.Result, error) {
		return exec.Result{ExitCode: 1, Stderr: []byte(`ERROR:  database "acme" already exists`)}, nil
	}}
	if _, err := (capabilities.PGCreate{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{"name": "acme"})); err != nil {
		t.Fatalf("an already-existing database must be forgiven, got %v", err)
	}
}

func TestPGUserCreateUsesDOBlockAndEscapesPassword(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.PGUserCreate{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"username": "acme", "password": "pa'ss'word",
	}))
	if err != nil {
		t.Fatalf("pg.user.create: %v", err)
	}
	_, stdin := pgLast(fr)
	if !strings.Contains(stdin, "DO $hp$") || !strings.Contains(stdin, `CREATE ROLE "acme" LOGIN`) {
		t.Errorf("stdin = %q, want a create-or-update DO block", stdin)
	}
	// The single quotes in the password must be doubled, not left to break the literal.
	if !strings.Contains(stdin, "pa''ss''word") {
		t.Errorf("password not escaped for a PG literal: %q", stdin)
	}
}

func TestPGUserCreateRejectsWeakPassword(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.PGUserCreate{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"username": "acme", "password": "short",
	})); err == nil {
		t.Fatal("a short password must be rejected")
	}
	if len(fr.Calls) != 0 {
		t.Error("nothing should run for a rejected password")
	}
}

func TestPGGrantRunsInsideTargetDatabase(t *testing.T) {
	fr := &exec.FakeRunner{}
	_, err := (capabilities.PGGrant{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"database": "acme", "username": "app", "privileges": []string{"ALL"},
	}))
	if err != nil {
		t.Fatalf("pg.grant: %v", err)
	}
	c, stdin := pgLast(fr)
	argv := strings.Join(c.Args, " ")
	// Schema/table grants must run against the target database (-d acme).
	if !strings.Contains(argv, "-d acme") {
		t.Errorf("grant must run inside the target db: %q", argv)
	}
	for _, want := range []string{`GRANT CONNECT ON DATABASE "acme" TO "app"`, "ALL ON ALL TABLES IN SCHEMA public", "ALTER DEFAULT PRIVILEGES"} {
		if !strings.Contains(stdin, want) {
			t.Errorf("grant SQL missing %q:\n%s", want, stdin)
		}
	}
}

func TestPGGrantRejectsUnknownPrivilege(t *testing.T) {
	fr := &exec.FakeRunner{}
	if _, err := (capabilities.PGGrant{}).Execute(appCtx(fr, fsys.NewFake()), raw(t, map[string]any{
		"database": "acme", "username": "app", "privileges": []string{"DROP DATABASE"},
	})); err == nil {
		t.Fatal("an unsupported privilege must be rejected")
	}
}

func TestPGCapabilitiesValidateIdentifiers(t *testing.T) {
	fr := &exec.FakeRunner{}
	ctx := appCtx(fr, fsys.NewFake())
	if _, err := (capabilities.PGCreate{}).Execute(ctx, raw(t, map[string]any{"name": "bad; DROP"})); err == nil {
		t.Error("pg.create accepted an invalid name")
	}
	if _, err := (capabilities.PGDrop{}).Execute(ctx, raw(t, map[string]any{"name": "../etc"})); err == nil {
		t.Error("pg.drop accepted an invalid name")
	}
	if _, err := (capabilities.PGUserDrop{}).Execute(ctx, raw(t, map[string]any{"username": "a b"})); err == nil {
		t.Error("pg.user.drop accepted an invalid username")
	}
	if _, err := (capabilities.PGSize{}).Execute(ctx, raw(t, map[string]any{"name": "Bad-Name"})); err == nil {
		t.Error("pg.size accepted an invalid name")
	}
	if len(fr.Calls) != 0 {
		t.Error("no invalid identifier may reach the runner")
	}
}
