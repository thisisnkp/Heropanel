package tenancy_test

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/internal/tenancy"
)

// mkUser inserts a user (optionally under a parent) and returns its id.
func mkUser(t *testing.T, repo *repository.UserRepository, email string, parentID int64) int64 {
	t.Helper()
	u := &repository.User{Email: email, Username: email, DisplayName: email, Status: "active"}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
	if parentID != 0 {
		if err := repo.SetParent(context.Background(), u.ID, parentID); err != nil {
			t.Fatalf("set parent %s: %v", email, err)
		}
	}
	return u.ID
}

func newRepo(t *testing.T) *repository.UserRepository {
	t.Helper()
	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "t.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repository.NewUserRepository(db)
}

// A two-reseller tree: each reseller owns clients; the resolver must see its own
// subtree and never the sibling's.
func TestSubtreeVisibilityAndAccess(t *testing.T) {
	repo := newRepo(t)
	res := tenancy.NewResolver(repo, nil)
	ctx := context.Background()

	resellerA := mkUser(t, repo, "resA", 0)
	resellerB := mkUser(t, repo, "resB", 0)
	a1 := mkUser(t, repo, "a1", resellerA)
	a2 := mkUser(t, repo, "a2", resellerA)
	a1sub := mkUser(t, repo, "a1sub", a1) // grandchild under A
	b1 := mkUser(t, repo, "b1", resellerB)

	// Reseller A sees itself, its clients, and the grandchild — never B's tenant.
	got, err := res.VisibleOwnerIDs(ctx, resellerA)
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	want := []int64{resellerA, a1, a2, a1sub}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(got) != len(want) {
		t.Fatalf("visible set = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("visible set = %v, want %v", got, want)
		}
	}

	// Access checks: A reaches its whole subtree, not B's.
	for _, id := range []int64{resellerA, a1, a2, a1sub} {
		ok, err := res.CanAccessOwner(ctx, resellerA, id)
		if err != nil || !ok {
			t.Fatalf("A should reach %d (ok=%v err=%v)", id, ok, err)
		}
	}
	for _, id := range []int64{resellerB, b1} {
		ok, err := res.CanAccessOwner(ctx, resellerA, id)
		if err != nil || ok {
			t.Fatalf("A must NOT reach %d (ok=%v err=%v)", id, ok, err)
		}
	}
	// A client cannot reach up to its reseller or sideways to a sibling.
	if ok, _ := res.CanAccessOwner(ctx, a1, resellerA); ok {
		t.Fatal("a client must not reach its parent")
	}
	if ok, _ := res.CanAccessOwner(ctx, a1, a2); ok {
		t.Fatal("a client must not reach a sibling")
	}
	// But a client reaches its own descendant.
	if ok, _ := res.CanAccessOwner(ctx, a1, a1sub); !ok {
		t.Fatal("a client should reach its own descendant")
	}
}

// A resolver without a repository imposes no scoping (the wiring-only case).
func TestDisabledResolverAllowsAll(t *testing.T) {
	var res *tenancy.Resolver = tenancy.NewResolver(nil, nil)
	if res.Enabled() {
		t.Fatal("a repo-less resolver must report disabled")
	}
	ok, err := res.CanAccessOwner(context.Background(), 1, 999)
	if err != nil || !ok {
		t.Fatalf("disabled resolver should allow all (ok=%v err=%v)", ok, err)
	}
}
