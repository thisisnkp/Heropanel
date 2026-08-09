package database_test

import (
	"context"
	"testing"
)

// SetDefaultEngine makes an engine-less create follow the setup wizard's choice:
// "postgresql" routes to the pg.* capabilities.
func TestDefaultEnginePostgres(t *testing.T) {
	svc, gw := newSvc(t)
	svc.SetDefaultEngine("postgresql")

	dbi, err := svc.CreateDatabase(context.Background(), 1, "def_pg", "") // no engine specified
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dbi.Engine != "postgres" {
		t.Fatalf("engine = %q, want postgres", dbi.Engine)
	}
	if gw.last("pg.create") == nil {
		t.Fatalf("expected pg.create with a postgres default: %+v", gw.calls)
	}
}

// "mysql" maps to MariaDB (wire-compatible, db.* capabilities).
func TestDefaultEngineMySQLMapsToMariaDB(t *testing.T) {
	svc, gw := newSvc(t)
	svc.SetDefaultEngine("mysql")

	dbi, err := svc.CreateDatabase(context.Background(), 1, "def_my", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dbi.Engine != "mariadb" {
		t.Fatalf("engine = %q, want mariadb", dbi.Engine)
	}
	if gw.last("db.create") == nil {
		t.Fatalf("expected db.create: %+v", gw.calls)
	}
}

// With no default set, an engine-less create is MariaDB (the historical default).
func TestNoDefaultEngineIsMariaDB(t *testing.T) {
	svc, gw := newSvc(t)
	dbi, err := svc.CreateDatabase(context.Background(), 1, "def_none", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dbi.Engine != "mariadb" {
		t.Fatalf("engine = %q, want mariadb", dbi.Engine)
	}
	_ = gw
}

// An explicit engine still overrides the default.
func TestExplicitEngineOverridesDefault(t *testing.T) {
	svc, gw := newSvc(t)
	svc.SetDefaultEngine("postgresql")
	if _, err := svc.CreateDatabase(context.Background(), 1, "override_my", "mariadb"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if gw.last("db.create") == nil {
		t.Fatalf("explicit mariadb should route to db.create: %+v", gw.calls)
	}
}
