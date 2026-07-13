package broker_test

import (
	"context"
	"strings"
	"testing"

	brokerd "github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestDBCreatePipesSQLViaStdin(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "db.create",
		Input:      mustJSON(t, map[string]string{"name": "acme_db"}),
	}); err != nil {
		t.Fatalf("db.create: %v", err)
	}
	last, _ := fake.Last()
	if last.Path != "/usr/bin/mysql" {
		t.Fatalf("path = %q", last.Path)
	}
	// SQL is passed on stdin (never argv) so secrets don't appear in `ps`.
	if !strings.Contains(string(last.Stdin), "CREATE DATABASE IF NOT EXISTS `acme_db`") {
		t.Fatalf("unexpected SQL: %q", last.Stdin)
	}
	if strings.Join(last.Args, " ") != "--protocol=socket" {
		t.Fatalf("args = %v (SQL must not be in argv)", last.Args)
	}
}

func TestDBUserCreatePasswordNotInArgv(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "db.user.create",
		Input:      mustJSON(t, map[string]string{"username": "acme", "host": "localhost", "password": "s3cret-pw!'x"}),
	}); err != nil {
		t.Fatalf("db.user.create: %v", err)
	}
	last, _ := fake.Last()
	// Password must be in the stdin SQL (escaped), not in argv.
	if strings.Contains(strings.Join(last.Args, " "), "s3cret") {
		t.Fatal("password must not appear in argv")
	}
	sql := string(last.Stdin)
	if !strings.Contains(sql, "CREATE USER IF NOT EXISTS 'acme'@'localhost'") {
		t.Fatalf("missing CREATE USER: %q", sql)
	}
	if !strings.Contains(sql, `s3cret-pw!\'x`) { // single quote escaped
		t.Fatalf("password not SQL-escaped: %q", sql)
	}
}

func TestDBCreateRejectsBadName(t *testing.T) {
	fake := &exec.FakeRunner{}
	b, _ := newTestBroker(t, policy.Default(), fake)
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "db.create",
		Input:      mustJSON(t, map[string]string{"name": "bad-name; DROP"}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatal("no SQL should run for an invalid name")
	}
}

func TestDBGrantValidatesPrivileges(t *testing.T) {
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b, _ := newTestBroker(t, policy.Default(), fake)

	if _, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "db.grant",
		Input: mustJSON(t, map[string]any{
			"database": "acme_db", "username": "acme", "host": "localhost", "privileges": []string{"SELECT", "INSERT"},
		}),
	}); err != nil {
		t.Fatalf("db.grant: %v", err)
	}
	last, _ := fake.Last()
	if !strings.Contains(string(last.Stdin), "GRANT SELECT, INSERT ON `acme_db`.* TO 'acme'@'localhost'") {
		t.Fatalf("unexpected grant SQL: %q", last.Stdin)
	}

	// An unknown privilege is rejected.
	_, err := b.Invoke(context.Background(), brokerd.Request{
		Capability: "db.grant",
		Input: mustJSON(t, map[string]any{
			"database": "acme_db", "username": "acme", "privileges": []string{"SUPER"},
		}),
	})
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for disallowed privilege, got %v", err)
	}
}
