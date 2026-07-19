package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

func TestStartSSOMintsAThrowawayAccountScopedToOneDatabase(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	sess, err := svc.StartSSO(ctx, dbUID)
	if err != nil {
		t.Fatalf("sso: %v", err)
	}
	if sess.URL != "https://panel.test/adminer/" || sess.Database != "acme_db" || sess.Server != "localhost" {
		t.Fatalf("session = %+v", sess)
	}
	// A throwaway account, clearly marked as ours.
	if !strings.HasPrefix(sess.Username, "hpsso_") {
		t.Fatalf("username is not a hand-off account: %q", sess.Username)
	}
	if len(sess.Password) < 24 {
		t.Fatalf("session password looks too weak: %q", sess.Password)
	}
	if sess.ExpiresAt == "" {
		t.Fatal("session has no expiry")
	}

	// It was created and granted on exactly the one database.
	created := gw.last("db.user.create")
	if created == nil || created.input["username"] != sess.Username ||
		created.input["password"] != sess.Password {
		t.Fatalf("db.user.create input = %+v", created)
	}
	granted := gw.last("db.grant")
	if granted == nil || granted.input["database"] != "acme_db" ||
		granted.input["username"] != sess.Username {
		t.Fatalf("db.grant input = %+v", granted)
	}

	// It is tracked, so the sweeper can drop it.
	rows, err := store.ListExpiredSSOSessions(ctx, "9999-01-01 00:00:00")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Username != sess.Username {
		t.Fatalf("session not tracked: %+v", rows)
	}
}

// Every hand-off gets its own credential, so revoking one cannot cut another
// session short.
func TestStartSSOIssuesAFreshAccountEveryTime(t *testing.T) {
	svc, _, _ := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	a, err := svc.StartSSO(ctx, dbUID)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := svc.StartSSO(ctx, dbUID)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.Username == b.Username || a.Password == b.Password {
		t.Fatal("two hand-offs shared a credential")
	}
}

// If the grant fails the account must not survive: an ungranted hpsso_ user is
// still a live login on the server.
func TestStartSSODropsTheAccountWhenTheGrantFails(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	gw.failOn = "db.grant"
	if _, err := svc.StartSSO(ctx, dbUID); err == nil {
		t.Fatal("a failing grant should surface an error")
	}
	if gw.last("db.user.drop") == nil {
		t.Fatal("the half-created account was left on the server")
	}
	rows, _ := store.ListExpiredSSOSessions(ctx, "9999-01-01 00:00:00")
	if len(rows) != 0 {
		t.Fatalf("a failed hand-off left a session row: %+v", rows)
	}
}

func TestStartSSORequiresAConfiguredClient(t *testing.T) {
	svc, _ := newSvc(t) // no WithAdminer
	dbUID, _ := seed(t, svc)
	if _, err := svc.StartSSO(context.Background(), dbUID); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestStartSSORejectsUnknownDatabase(t *testing.T) {
	svc, _, _ := newSSOSvc(t)
	if _, err := svc.StartSSO(context.Background(), "nope"); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestSweepDropsExpiredAccountsAndLeavesLiveOnes(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	live, err := svc.StartSSO(ctx, dbUID)
	if err != nil {
		t.Fatalf("sso: %v", err)
	}
	// A session that expired in the past, as the sweeper would find it.
	expired := &database.SSOSessionRecord{
		DBInstanceID: 1, Username: "hpsso_expired1", ExpiresAt: "2000-01-01 00:00:00",
	}
	if err := store.InsertSSOSession(ctx, expired); err != nil {
		t.Fatalf("insert: %v", err)
	}

	n, err := svc.SweepSSO(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	dropped := gw.last("db.user.drop")
	if dropped == nil || dropped.input["username"] != "hpsso_expired1" {
		t.Fatalf("the expired account was not dropped: %+v", dropped)
	}
	// The live session is untouched.
	rows, _ := store.ListExpiredSSOSessions(ctx, "9999-01-01 00:00:00")
	if len(rows) != 1 || rows[0].Username != live.Username {
		t.Fatalf("sweep removed a live session: %+v", rows)
	}
}

// A drop that fails must leave the row behind, or a live account is stranded
// with nothing tracking it.
func TestSweepKeepsTheRowWhenTheDropFails(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	seed(t, svc)

	if err := store.InsertSSOSession(ctx, &database.SSOSessionRecord{
		DBInstanceID: 1, Username: "hpsso_expired1", ExpiresAt: "2000-01-01 00:00:00",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	gw.failOn = "db.user.drop"
	n, err := svc.SweepSSO(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d despite the drop failing", n)
	}
	rows, _ := store.ListExpiredSSOSessions(ctx, "9999-01-01 00:00:00")
	if len(rows) != 1 {
		t.Fatal("the row was removed while the account is still live")
	}
}

// The sweeper drops accounts. It must never be able to touch a real user, even
// if a row somehow names one.
func TestSweepRefusesToDropNonHandoffAccounts(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	seed(t, svc)

	if err := store.InsertSSOSession(ctx, &database.SSOSessionRecord{
		DBInstanceID: 1, Username: "root", ExpiresAt: "2000-01-01 00:00:00",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := svc.SweepSSO(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if gw.last("db.user.drop") != nil {
		t.Fatal("the sweeper tried to drop an account it did not mint")
	}
}
