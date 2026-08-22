package database_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/database"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// ticketOf pulls the ticket out of the hand-off URL, which is all the browser
// ever receives.
func ticketOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("hand-off URL: %v", err)
	}
	tk := u.Query().Get("np_ticket")
	if tk == "" {
		t.Fatalf("hand-off URL carries no ticket: %s", raw)
	}
	return tk
}

// The hand-off gives the browser a URL and nothing else.
//
// This is the property the whole design exists for: a page that never contains
// a database password cannot leak one through a screenshot, a history entry, a
// proxy log or a browser extension.
func TestPMAHandoffCarriesNoCredentials(t *testing.T) {
	svc, gw, _ := newSSOSvc(t)
	dbUID, _ := seed(t, svc)

	h, err := svc.StartPMASession(context.Background(), dbUID, 7)
	if err != nil {
		t.Fatalf("hand-off: %v", err)
	}
	if !strings.HasPrefix(h.URL, "https://panel.test/adminer/?np_ticket=") {
		t.Fatalf("URL = %q", h.URL)
	}
	if h.ExpiresAt == "" {
		t.Fatal("hand-off has no expiry")
	}
	// No hand-off account yet. Minting one at hand-off time would mean a live
	// database login for every click, including the ones nobody follows through.
	// (The seed creates an ordinary user, so the check is for an npsso_ one.)
	if mintedHandoffAccount(gw) {
		t.Fatalf("an account was minted before the ticket was redeemed: %+v", gw.calls)
	}
}

// Redeeming mints a throwaway account granted on exactly one database.
func TestRedeemMintsAnAccountScopedToOneDatabase(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	h, err := svc.StartPMASession(ctx, dbUID, 7)
	if err != nil {
		t.Fatalf("hand-off: %v", err)
	}
	creds, err := svc.RedeemPMATicket(ctx, ticketOf(t, h.URL))
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if creds.Database != "acme_db" || creds.Server != "localhost" {
		t.Fatalf("credentials = %+v", creds)
	}
	// A throwaway account, clearly marked as ours.
	if !strings.HasPrefix(creds.Username, "npsso_") {
		t.Fatalf("username is not a hand-off account: %q", creds.Username)
	}
	if len(creds.Password) < 24 {
		t.Fatalf("session password looks too weak: %q", creds.Password)
	}

	created := gw.last("db.user.create")
	if created == nil || created.input["username"] != creds.Username ||
		created.input["password"] != creds.Password {
		t.Fatalf("db.user.create input = %+v", created)
	}
	granted := gw.last("db.grant")
	if granted == nil || granted.input["database"] != "acme_db" ||
		granted.input["username"] != creds.Username {
		t.Fatalf("db.grant input = %+v", granted)
	}

	// It is tracked, so the sweeper can drop it.
	rows, err := store.ListExpiredSSOSessions(ctx, "9999-01-01 00:00:00")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Username != creds.Username {
		t.Fatalf("session not tracked: %+v", rows)
	}
}

// A ticket is spent the first time it is redeemed.
//
// Single use is the property that makes it safe to put in a URL at all: a
// ticket that survives its own redemption is a database login sitting in the
// browser history, and the fifteen-minute account it opens is still live.
func TestRedeemIsSingleUse(t *testing.T) {
	svc, _, _ := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	h, _ := svc.StartPMASession(ctx, dbUID, 7)
	ticket := ticketOf(t, h.URL)

	if _, err := svc.RedeemPMATicket(ctx, ticket); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := svc.RedeemPMATicket(ctx, ticket); !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("a spent ticket was redeemed again: %v", err)
	}
}

// An unknown ticket is refused, and refused the same way a spent one is.
func TestRedeemRefusesAnUnknownTicket(t *testing.T) {
	svc, gw, _ := newSSOSvc(t)
	seed(t, svc)

	_, err := svc.RedeemPMATicket(context.Background(), "not-a-real-ticket")
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	if mintedHandoffAccount(gw) {
		t.Fatal("an unknown ticket minted an account")
	}
}

// mintedHandoffAccount reports whether any npsso_ account was created. The
// seeded fixtures create ordinary users too, so counting db.user.create calls
// would not answer the question being asked.
func mintedHandoffAccount(gw *mockGateway) bool {
	for _, c := range gw.calls {
		if c.capability != "db.user.create" {
			continue
		}
		if u, _ := c.input["username"].(string); strings.HasPrefix(u, "npsso_") {
			return true
		}
	}
	return false
}

// Every hand-off gets its own ticket and its own credential, so revoking one
// cannot cut another session short.
func TestEachHandoffIsIndependent(t *testing.T) {
	svc, _, _ := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	h1, _ := svc.StartPMASession(ctx, dbUID, 7)
	h2, _ := svc.StartPMASession(ctx, dbUID, 7)
	if h1.URL == h2.URL {
		t.Fatal("two hand-offs shared a ticket")
	}
	a, err := svc.RedeemPMATicket(ctx, ticketOf(t, h1.URL))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := svc.RedeemPMATicket(ctx, ticketOf(t, h2.URL))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.Username == b.Username || a.Password == b.Password {
		t.Fatal("two hand-offs shared a credential")
	}
}

// If the grant fails the account must not survive: an ungranted npsso_ user is
// still a live login on the server.
func TestRedeemDropsTheAccountWhenTheGrantFails(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	h, _ := svc.StartPMASession(ctx, dbUID, 7)
	gw.failOn = "db.grant"
	if _, err := svc.RedeemPMATicket(ctx, ticketOf(t, h.URL)); err == nil {
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

func TestPMAHandoffRequiresAConfiguredClient(t *testing.T) {
	svc, _ := newSvc(t) // no WithAdminer
	dbUID, _ := seed(t, svc)
	if _, err := svc.StartPMASession(context.Background(), dbUID, 1); !errx.IsKind(err, errx.KindUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestPMAHandoffRejectsUnknownDatabase(t *testing.T) {
	svc, _, _ := newSSOSvc(t)
	if _, err := svc.StartPMASession(context.Background(), "nope", 1); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestSweepDropsExpiredAccountsAndLeavesLiveOnes(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()
	dbUID, _ := seed(t, svc)

	h, err := svc.StartPMASession(ctx, dbUID, 1)
	if err != nil {
		t.Fatalf("hand-off: %v", err)
	}
	live, err := svc.RedeemPMATicket(ctx, ticketOf(t, h.URL))
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	// A session that expired in the past, as the sweeper would find it.
	expired := &database.SSOSessionRecord{
		DBInstanceID: 1, Username: "npsso_expired1", ExpiresAt: "2000-01-01 00:00:00",
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
	if dropped == nil || dropped.input["username"] != "npsso_expired1" {
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
		DBInstanceID: 1, Username: "npsso_expired1", ExpiresAt: "2000-01-01 00:00:00",
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
