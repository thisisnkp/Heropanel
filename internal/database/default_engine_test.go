package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/database"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Every create lands on MariaDB and the db.* capabilities, whether the caller
// names the engine, names its wire-compatible alias, or names nothing at all.
func TestCreateAlwaysUsesMariaDB(t *testing.T) {
	for _, engine := range []string{"", "mariadb", "mysql"} {
		svc, gw := newSvc(t)
		dbi, err := svc.CreateDatabase(context.Background(), 1, "def_db", engine)
		if err != nil {
			t.Fatalf("create %q: %v", engine, err)
		}
		if dbi.Engine != "mariadb" {
			t.Fatalf("engine %q => %q, want mariadb", engine, dbi.Engine)
		}
		if gw.last("db.create") == nil {
			t.Fatalf("engine %q should route to db.create: %+v", engine, gw.calls)
		}
	}
}

// An engine the panel does not manage is refused at the edge rather than
// recorded and then failing at the broker.
//
// "postgres" is named explicitly because it used to work: a client or a script
// written against an older release will still send it, and the error it gets
// back should say what is actually true now.
func TestUnsupportedEngineRefused(t *testing.T) {
	for _, engine := range []string{"postgres", "postgresql", "sqlite"} {
		svc, gw := newSvc(t)
		_, err := svc.CreateDatabase(context.Background(), 1, "nope_db", engine)
		if err == nil {
			t.Fatalf("engine %q was accepted; only MariaDB is managed", engine)
		}
		if !strings.Contains(err.Error(), "MariaDB") {
			t.Errorf("engine %q: error should name the engine that is supported, got %v", engine, err)
		}
		if len(gw.calls) != 0 {
			t.Errorf("engine %q reached the broker: %+v", engine, gw.calls)
		}
	}
}

// A row left behind by a release that supported PostgreSQL is refused, not
// quietly re-aimed at MariaDB.
//
// This is the upgrade case, and it is the dangerous one. brokerCap now always
// returns a db.* capability, so without this guard "drop the database called
// reports" — recorded against PostgreSQL — would be sent to MariaDB. At best it
// fails with a confusing error; at worst a MariaDB database of the same name
// exists and gets dropped instead.
func TestPostgresRowFromAnOlderReleaseIsRefused(t *testing.T) {
	svc, gw, store := newSSOSvc(t)
	ctx := context.Background()

	// Written straight to the store: the service will not create one any more.
	rec := &database.InstanceRecord{OwnerID: 1, Engine: "postgres", Name: "reports", Charset: "UTF8", Status: "active"}
	if err := store.InsertDatabase(ctx, rec); err != nil {
		t.Fatalf("seed pg row: %v", err)
	}

	err := svc.DeleteDatabase(ctx, rec.UID)
	if !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("dropping a postgres row must be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("the error should name the engine the row belongs to, got %v", err)
	}
	for _, c := range gw.calls {
		if strings.HasPrefix(c.capability, "db.") {
			t.Fatalf("a postgres row reached a MariaDB capability: %s", c.capability)
		}
	}
}
