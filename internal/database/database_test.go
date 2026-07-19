package database_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/thisisnkp/heropanel/internal/config"
	"github.com/thisisnkp/heropanel/internal/database"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

type mockGateway struct {
	calls   []call
	results map[string]map[string]any // canned results per capability
	failOn  string
}
type call struct {
	capability string
	input      map[string]any
}

func (m *mockGateway) Invoke(_ context.Context, c string, input any) (map[string]any, error) {
	in, _ := input.(map[string]any)
	m.calls = append(m.calls, call{c, in})
	if m.results != nil {
		if res, ok := m.results[c]; ok {
			return res, nil
		}
	}
	if m.failOn == c {
		return nil, errx.New(errx.KindUpstream, "boom", "simulated broker failure")
	}
	return map[string]any{"ok": true}, nil
}
func (m *mockGateway) Health(context.Context) error { return nil }

func (m *mockGateway) last(capability string) *call {
	for i := len(m.calls) - 1; i >= 0; i-- {
		if m.calls[i].capability == capability {
			return &m.calls[i]
		}
	}
	return nil
}

func newSvc(t *testing.T) (*database.Service, *mockGateway) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "db.db")
	dbh, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if _, err := repository.Migrate(context.Background(), dbh); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = repository.NewUserRepository(dbh).Create(context.Background(),
		&repository.User{Email: "o@x.com", Username: "o"})
	gw := &mockGateway{}
	return database.NewService(repository.NewDatabaseStore(dbh), gw), gw
}

// newSSOSvc is newSvc with the Adminer hand-off wired, and hands back the store
// so a test can inspect what the sweeper will see.
func newSSOSvc(t *testing.T) (*database.Service, *mockGateway, *repository.DatabaseStore) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "db.db")
	dbh, err := repository.Open(config.Database{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if _, err := repository.Migrate(context.Background(), dbh); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = repository.NewUserRepository(dbh).Create(context.Background(),
		&repository.User{Email: "o@x.com", Username: "o"})
	store := repository.NewDatabaseStore(dbh)
	gw := &mockGateway{}
	svc := database.NewService(store, gw).WithAdminer("https://panel.test/adminer/", store)
	return svc, gw, store
}

func TestCreateDatabaseAndUserAndGrant(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()

	dbi, err := svc.CreateDatabase(ctx, 1, "acme_db")
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if dbi.Name != "acme_db" || dbi.Engine != "mariadb" {
		t.Fatalf("unexpected db: %+v", dbi)
	}

	usr, err := svc.CreateUser(ctx, 1, "acme", "localhost", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := svc.Grant(ctx, dbi.UID, usr.UID, []string{"ALL"}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// Broker was asked to create the DB, the user, and grant, in that order.
	if len(gw.calls) != 3 ||
		gw.calls[0].capability != "db.create" ||
		gw.calls[1].capability != "db.user.create" ||
		gw.calls[2].capability != "db.grant" {
		t.Fatalf("unexpected broker calls: %+v", gw.calls)
	}
	if gw.calls[2].input["database"] != "acme_db" || gw.calls[2].input["username"] != "acme" {
		t.Fatalf("grant input = %+v", gw.calls[2].input)
	}

	// Listing reflects persistence.
	dbs, _ := svc.ListDatabases(ctx, 0, 50, 0)
	users, _ := svc.ListUsers(ctx, 0, 50, 0)
	if len(dbs) != 1 || len(users) != 1 {
		t.Fatalf("list dbs=%d users=%d", len(dbs), len(users))
	}
}

func TestCreateDatabaseValidatesName(t *testing.T) {
	svc, gw := newSvc(t)
	if _, err := svc.CreateDatabase(context.Background(), 1, "Bad-Name"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation, got %v", err)
	}
	if len(gw.calls) != 0 {
		t.Fatal("no broker call for invalid name")
	}
}

func TestCreateUserWeakPassword(t *testing.T) {
	svc, _ := newSvc(t)
	if _, err := svc.CreateUser(context.Background(), 1, "acme", "localhost", "short"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for weak password, got %v", err)
	}
}

func TestDeleteDatabaseDropsAndRemoves(t *testing.T) {
	svc, gw := newSvc(t)
	ctx := context.Background()
	dbi, _ := svc.CreateDatabase(ctx, 1, "gone_db")
	if err := svc.DeleteDatabase(ctx, dbi.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// db.create then db.drop.
	if gw.calls[len(gw.calls)-1].capability != "db.drop" {
		t.Fatalf("expected db.drop, got %+v", gw.calls)
	}
	dbs, _ := svc.ListDatabases(ctx, 0, 50, 0)
	if len(dbs) != 0 {
		t.Fatalf("expected 0 dbs after delete, got %d", len(dbs))
	}
}
