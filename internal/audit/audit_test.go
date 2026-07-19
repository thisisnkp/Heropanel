package audit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRepo is an in-memory Repo. It appends in call order, which is exactly the
// property the chain depends on.
type fakeRepo struct {
	mu      sync.Mutex
	entries []Entry
	failOn  int // 1-based index of the Append to fail; 0 = never
	calls   int
}

func (f *fakeRepo) Append(_ context.Context, e *Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failOn == f.calls {
		return errors.New("boom")
	}
	e.ID = int64(len(f.entries) + 1)
	f.entries = append(f.entries, *e)
	return nil
}

func (f *fakeRepo) Head(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		return "", nil
	}
	return f.entries[len(f.entries)-1].RowHash, nil
}

func (f *fakeRepo) List(_ context.Context, _ Filter) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Entry, len(f.entries))
	for i, e := range f.entries {
		out[len(f.entries)-1-i] = e // newest-first, as the real repo returns
	}
	return out, nil
}

func mustRecord(t *testing.T, s *Service, action string) Entry {
	t.Helper()
	e, err := s.Record(context.Background(), Record{
		ActorUserID: 7,
		ActorKind:   ActorUser,
		Action:      action,
		Outcome:     OutcomeSuccess,
	})
	if err != nil {
		t.Fatalf("Record(%s): %v", action, err)
	}
	return e
}

func TestChainLinksEachEntryToItsPredecessor(t *testing.T) {
	repo := &fakeRepo{}
	s := NewService(repo)

	first := mustRecord(t, s, "a")
	second := mustRecord(t, s, "b")
	third := mustRecord(t, s, "c")

	if first.PrevHash != "" {
		t.Errorf("first entry PrevHash = %q, want empty (nothing precedes it)", first.PrevHash)
	}
	if second.PrevHash != first.RowHash {
		t.Errorf("second.PrevHash = %q, want first.RowHash %q", second.PrevHash, first.RowHash)
	}
	if third.PrevHash != second.RowHash {
		t.Errorf("third.PrevHash = %q, want second.RowHash %q", third.PrevHash, second.RowHash)
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Errorf("Verify on an untouched chain: %v", err)
	}
}

// The whole point of the chain: editing a committed row must be detectable.
func TestVerifyDetectsATamperedEntry(t *testing.T) {
	repo := &fakeRepo{}
	s := NewService(repo)
	mustRecord(t, s, "POST /api/v1/databases")
	mustRecord(t, s, "DELETE /api/v1/databases/{uid}")
	mustRecord(t, s, "POST /api/v1/sites")

	// Someone rewrites history to hide the deletion.
	repo.entries[1].Action = "GET /api/v1/databases"

	err := s.Verify(context.Background())
	if err == nil {
		t.Fatal("Verify accepted a chain whose middle entry was edited")
	}
	if !strings.Contains(err.Error(), "tampered") {
		t.Errorf("Verify error = %v, want it to name the tampering", err)
	}
}

func TestVerifyDetectsARemovedEntry(t *testing.T) {
	repo := &fakeRepo{}
	s := NewService(repo)
	mustRecord(t, s, "a")
	mustRecord(t, s, "b")
	mustRecord(t, s, "c")

	// Drop the middle row entirely — the classic "delete the evidence" move.
	repo.entries = []Entry{repo.entries[0], repo.entries[2]}

	if err := s.Verify(context.Background()); err == nil {
		t.Fatal("Verify accepted a chain with a deleted entry")
	}
}

// A failed insert must not advance the chain head: the next entry would then
// point at a row that does not exist and Verify would blame the wrong record.
func TestFailedAppendDoesNotAdvanceTheChain(t *testing.T) {
	repo := &fakeRepo{failOn: 2}
	s := NewService(repo)

	first := mustRecord(t, s, "a")
	if _, err := s.Record(context.Background(), Record{Action: "b"}); err == nil {
		t.Fatal("Record succeeded despite the repository failing")
	}
	third := mustRecord(t, s, "c")

	if third.PrevHash != first.RowHash {
		t.Errorf("after a failed append, next PrevHash = %q, want the last *committed* hash %q",
			third.PrevHash, first.RowHash)
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Errorf("Verify after a failed append: %v", err)
	}
}

// The chain is a linked list; two writers reading the same head would fork it.
// Run with -race.
func TestConcurrentRecordsProduceOneUnbrokenChain(t *testing.T) {
	repo := &fakeRepo{}
	s := NewService(repo)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Record(context.Background(), Record{Action: "concurrent"}); err != nil {
				t.Errorf("Record: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(repo.entries) != n {
		t.Fatalf("got %d entries, want %d", len(repo.entries), n)
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Errorf("Verify after %d concurrent appends: %v", n, err)
	}
}

// The service resumes from what is already in the table, or a restart would
// start a second chain that Verify reports as a break at the seam.
func TestChainResumesFromThePersistedHead(t *testing.T) {
	repo := &fakeRepo{}
	first := mustRecord(t, NewService(repo), "before restart")

	// A fresh Service over the same table — as hpd would be after a restart.
	revived := NewService(repo)
	second := mustRecord(t, revived, "after restart")

	if second.PrevHash != first.RowHash {
		t.Errorf("after restart PrevHash = %q, want the persisted head %q", second.PrevHash, first.RowHash)
	}
	if err := revived.Verify(context.Background()); err != nil {
		t.Errorf("Verify across a restart: %v", err)
	}
}

// The hash covers the timestamp as persisted, and MariaDB's DATETIME(6) always
// hands back six fractional digits. Rendering fewer here would mean every row
// read back from MariaDB hashed differently than it was written.
func TestTimestampIsHashedInItsPersistedForm(t *testing.T) {
	at := time.Date(2026, 7, 17, 10, 30, 15, 123456789, time.UTC)
	repo := &fakeRepo{}
	s := NewService(repo)

	e, err := s.Record(context.Background(), Record{Action: "a", At: at})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if e.CreatedAt != "2026-07-17 10:30:15.123456" {
		t.Fatalf("CreatedAt = %q, want microsecond precision to match DATETIME(6)", e.CreatedAt)
	}
	// Round-trip: rehashing the entry exactly as stored must reproduce row_hash.
	if got := hashEntry(e); got != e.RowHash {
		t.Errorf("rehash of the persisted entry = %q, want %q", got, e.RowHash)
	}
}

// A whole-second time must still render six digits: MariaDB pads it to
// ".000000" on the way out regardless of what was written.
func TestWholeSecondTimestampStillRendersMicroseconds(t *testing.T) {
	at := time.Date(2026, 7, 17, 10, 30, 15, 0, time.UTC)
	if got := FormatTime(at); got != "2026-07-17 10:30:15.000000" {
		t.Errorf("FormatTime = %q, want the zero-padded form MariaDB returns", got)
	}
}

func TestEmptyDetailIsValidJSON(t *testing.T) {
	repo := &fakeRepo{}
	e := mustRecord(t, NewService(repo), "a")
	// MariaDB types this column as JSON and rejects "".
	if e.Detail != "{}" {
		t.Errorf("Detail = %q, want %q so the JSON column accepts it", e.Detail, "{}")
	}
}

func TestRecordDefaultsAnonymousActorAndSuccess(t *testing.T) {
	repo := &fakeRepo{}
	s := NewService(repo)
	e, err := s.Record(context.Background(), Record{Action: "a"})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if e.ActorKind != ActorAnonymous {
		t.Errorf("ActorKind = %q, want %q", e.ActorKind, ActorAnonymous)
	}
	if e.Outcome != OutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", e.Outcome, OutcomeSuccess)
	}
}

// A nil Service is what a datastore-less hpd holds. It must no-op, not panic.
func TestNilServiceRecordsNothingAndDoesNotPanic(t *testing.T) {
	var s *Service
	if _, err := s.Record(context.Background(), Record{Action: "a"}); err != nil {
		t.Errorf("Record on a nil Service: %v", err)
	}
}

func TestDetailJSONIsStableAcrossRuns(t *testing.T) {
	// Go randomizes map iteration; the detail is hashed, so key order must not be.
	build := func() string {
		a := NewAnnotation()
		ctx := WithAnnotation(context.Background(), a)
		AddDetail(ctx, "zebra", 1)
		AddDetail(ctx, "alpha", "two")
		AddDetail(ctx, "middle", true)
		return a.DetailJSON()
	}
	want := `{"alpha":"two","middle":true,"zebra":1}`
	for i := 0; i < 20; i++ {
		if got := build(); got != want {
			t.Fatalf("DetailJSON() = %s, want %s", got, want)
		}
	}
}

func TestAnnotationHelpersNoOpWithoutAContext(t *testing.T) {
	// Services called outside an HTTP request (a sweeper, a test) must not care.
	ctx := context.Background()
	SetResource(ctx, "sites", "abc")
	SetActor(ctx, 1, ActorUser)
	AddDetail(ctx, "k", "v")
	Force(ctx)
	// Reaching here without a panic is the assertion.
}
