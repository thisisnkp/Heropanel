package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/repository"
)

func newAuditService(t *testing.T) (*audit.Service, *repository.AuditRepository) {
	t.Helper()
	repo := repository.NewAuditRepository(newTestDB(t))
	return audit.NewService(repo), repo
}

func TestAuditHeadIsEmptyOnAFreshTable(t *testing.T) {
	_, repo := newAuditService(t)
	head, err := repo.Head(context.Background())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != "" {
		t.Errorf("Head on an empty table = %q, want empty", head)
	}
}

// The chain must survive the database: hashes are computed in Go over values the
// column has to store and return byte-for-byte. This is the test that would have
// caught a timestamp whose precision the column silently drops.
func TestAuditChainVerifiesAfterARoundTripThroughSQLite(t *testing.T) {
	svc, _ := newAuditService(t)
	ctx := context.Background()

	for i, action := range []string{
		"POST /api/v1/sites",
		"PUT /api/v1/sites/{uid}/git",
		"POST /api/v1/sites/{uid}/git/deploy",
		"DELETE /api/v1/databases/{uid}",
	} {
		_, err := svc.Record(ctx, audit.Record{
			ActorUserID:  1,
			ActorIP:      "203.0.113.7",
			ActorKind:    audit.ActorUser,
			Action:       action,
			ResourceType: "sites",
			ResourceID:   "site_1",
			Outcome:      audit.OutcomeSuccess,
			Detail:       `{"n":` + string(rune('0'+i)) + `}`,
		})
		if err != nil {
			t.Fatalf("Record(%s): %v", action, err)
		}
	}

	if err := svc.Verify(ctx); err != nil {
		t.Errorf("Verify after a round trip through the database: %v", err)
	}
}

func TestAuditListReturnsNewestFirst(t *testing.T) {
	svc, repo := newAuditService(t)
	ctx := context.Background()

	for _, a := range []string{"first", "second", "third"} {
		if _, err := svc.Record(ctx, audit.Record{Action: a, ActorKind: audit.ActorSystem}); err != nil {
			t.Fatalf("Record(%s): %v", a, err)
		}
	}
	got, err := repo.List(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].Action != "third" {
		t.Errorf("first result = %q, want the newest entry %q", got[0].Action, "third")
	}
}

func TestAuditListFiltersByResource(t *testing.T) {
	svc, repo := newAuditService(t)
	ctx := context.Background()

	_, _ = svc.Record(ctx, audit.Record{Action: "a", ResourceType: "sites", ResourceID: "s1"})
	_, _ = svc.Record(ctx, audit.Record{Action: "b", ResourceType: "databases", ResourceID: "d1"})
	_, _ = svc.Record(ctx, audit.Record{Action: "c", ResourceType: "sites", ResourceID: "s1"})

	got, err := repo.List(ctx, audit.Filter{ResourceType: "sites", ResourceID: "s1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries for sites/s1, want 2", len(got))
	}
}

func TestAuditListFiltersByActor(t *testing.T) {
	svc, repo := newAuditService(t)
	ctx := context.Background()

	_, _ = svc.Record(ctx, audit.Record{Action: "a", ActorUserID: 1, ActorKind: audit.ActorUser})
	_, _ = svc.Record(ctx, audit.Record{Action: "b", ActorUserID: 2, ActorKind: audit.ActorUser})
	_, _ = svc.Record(ctx, audit.Record{Action: "c", ActorUserID: 1, ActorKind: audit.ActorUser})

	got, err := repo.List(ctx, audit.Filter{ActorUserID: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries for actor 1, want 2", len(got))
	}
}

// An anonymous actor is stored as NULL, not 0: 0 is not a user id, and a foreign
// key to users(id) would reject it.
func TestAuditAnonymousActorIsStoredAsNull(t *testing.T) {
	svc, repo := newAuditService(t)
	ctx := context.Background()

	if _, err := svc.Record(ctx, audit.Record{
		Action:    "POST /hooks/git/{uid}",
		ActorKind: audit.ActorAnonymous,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := repo.List(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got[0].ActorUserID != 0 {
		t.Errorf("ActorUserID = %d, want 0 for an anonymous actor", got[0].ActorUserID)
	}
	if got[0].ActorKind != audit.ActorAnonymous {
		t.Errorf("ActorKind = %q, want %q", got[0].ActorKind, audit.ActorAnonymous)
	}
}

// A restart re-reads the head from the table rather than starting a new chain.
func TestAuditChainResumesAcrossServiceInstances(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewAuditRepository(db)
	ctx := context.Background()

	first, err := audit.NewService(repo).Record(ctx, audit.Record{Action: "before"})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	revived := audit.NewService(repo)
	second, err := revived.Record(ctx, audit.Record{Action: "after"})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if second.PrevHash != first.RowHash {
		t.Errorf("PrevHash after restart = %q, want the persisted head %q", second.PrevHash, first.RowHash)
	}
	if err := revived.Verify(ctx); err != nil {
		t.Errorf("Verify across a restart: %v", err)
	}
}

// A row edited directly in the database — the threat the chain exists for.
func TestVerifyDetectsAnUpdateMadeDirectlyInSQL(t *testing.T) {
	db := newTestDB(t)
	svc := audit.NewService(repository.NewAuditRepository(db))
	ctx := context.Background()

	_, _ = svc.Record(ctx, audit.Record{Action: "POST /api/v1/sites", At: time.Now()})
	_, _ = svc.Record(ctx, audit.Record{Action: "DELETE /api/v1/sites/{uid}", ResourceID: "site_1"})
	_, _ = svc.Record(ctx, audit.Record{Action: "POST /api/v1/databases"})

	// Someone with SQL access quietly rewrites the deletion away.
	if _, err := db.ExecContext(ctx,
		`UPDATE audit_log SET action = 'GET /api/v1/sites' WHERE action = 'DELETE /api/v1/sites/{uid}'`); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if err := svc.Verify(ctx); err == nil {
		t.Fatal("Verify accepted a row that was edited behind its back")
	}
}
