package repository_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/config"
	dnspkg "github.com/thisisnkp/nexpanel/internal/database"
	dns "github.com/thisisnkp/nexpanel/internal/dns"
	mail "github.com/thisisnkp/nexpanel/internal/mail"
	"github.com/thisisnkp/nexpanel/internal/repository"
	site "github.com/thisisnkp/nexpanel/internal/site"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// TestResourceOwnerStore proves the generic owner lookup resolves every kind —
// including the nested ones (a DNS record through its zone, a mailbox through its
// domain) — to the right owning user, and reports not-found for a bogus uid.
func TestResourceOwnerStore(t *testing.T) {
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "own.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	users := repository.NewUserRepository(db)
	owner := repository.NewResourceOwnerStore(db)

	// Two owners so the nested lookups can't accidentally pass by returning the
	// only user present.
	a := &repository.User{Email: "a@h.io", Username: "a", DisplayName: "a", Status: "active"}
	b := &repository.User{Email: "b@h.io", Username: "b", DisplayName: "b", Status: "active"}
	if err := users.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	// Site owned by A.
	siteStore := repository.NewSiteStore(db)
	sRec := &site.Record{OwnerID: a.ID, Name: "s", PrimaryDomain: "s.example.com", Type: "static", DeployMode: "baremetal", Status: "active", Webserver: "openlitespeed"}
	if err := siteStore.Insert(ctx, sRec); err != nil {
		t.Fatal(err)
	}

	// DNS zone owned by A, with a record beneath it.
	dnsStore := repository.NewDNSStore(db)
	zone := &dns.ZoneRow{OwnerID: a.ID, Name: "a.example.com", PrimaryNS: "ns1.a.example.com", AdminEmail: "hostmaster.a.example.com", Serial: 1, TTL: 3600, Status: "active"}
	if err := dnsStore.InsertZone(ctx, zone); err != nil {
		t.Fatal(err)
	}
	rec := &dns.RecordRow{ZoneID: zone.ID, Name: "www", Type: "A", Content: "203.0.113.1", TTL: 3600}
	if err := dnsStore.InsertRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// Mail domain owned by A, with a mailbox beneath it.
	mailStore := repository.NewMailStore(db)
	dom := &mail.DomainRecord{UID: "mdom", OwnerID: a.ID, Domain: "a.example.com", DKIMSelector: "np", Status: "active"}
	if err := mailStore.InsertDomain(ctx, dom); err != nil {
		t.Fatal(err)
	}
	acct := &mail.AccountRecord{UID: "macct", DomainID: dom.ID, LocalPart: "info", PasswordHash: "x", QuotaMB: 0, Status: "active"}
	if err := mailStore.InsertAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}

	// Database instance owned by B.
	dbStore := repository.NewDatabaseStore(db)
	inst := &dnspkg.InstanceRecord{OwnerID: b.ID, Engine: "mariadb", Name: "app", Charset: "utf8mb4", Status: "active"}
	if err := dbStore.InsertDatabase(ctx, inst); err != nil {
		t.Fatal(err)
	}

	// Database user owned by B — the resource a grant/revoke body references.
	dbUser := &dnspkg.UserRecord{OwnerID: b.ID, Engine: "mariadb", Username: "app_rw", Host: "%"}
	if err := dbStore.InsertUser(ctx, dbUser); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		kind repository.ResourceKind
		uid  string
		want int64
	}{
		{repository.KindSite, sRec.UID, a.ID},
		{repository.KindDNSZone, zone.UID, a.ID},
		{repository.KindDNSRecord, rec.UID, a.ID}, // through the zone
		{repository.KindMailDomain, dom.UID, a.ID},
		{repository.KindMailAccount, acct.UID, a.ID}, // through the domain
		{repository.KindDBInstance, inst.UID, b.ID},
		{repository.KindDBUser, dbUser.UID, b.ID},
	}
	for _, c := range cases {
		got, err := owner.OwnerOf(ctx, c.kind, c.uid)
		if err != nil {
			t.Fatalf("%s(%s): %v", c.kind, c.uid, err)
		}
		if got != c.want {
			t.Errorf("%s(%s) owner = %d, want %d", c.kind, c.uid, got, c.want)
		}
	}

	// A uid that does not exist is a clean not-found, not a zero owner.
	if _, err := owner.OwnerOf(ctx, repository.KindDNSZone, "nope"); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("unknown uid = %v, want not-found", err)
	}
}
