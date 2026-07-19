package audit

import (
	"context"
	"sync"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Repo persists audit entries. Append must not assign PrevHash/RowHash — the
// chain belongs to the Service, which is the only thing that may decide what
// links to what.
type Repo interface {
	Append(ctx context.Context, e *Entry) error
	Head(ctx context.Context) (string, error)
	List(ctx context.Context, f Filter) ([]Entry, error)
}

// Filter narrows a listing. A zero Limit means the repository's default.
type Filter struct {
	ActorUserID  int64
	ResourceType string
	ResourceID   string
	Action       string
	Limit        int
	Offset       int
}

// Service appends to and reads the audit chain.
//
// Appends are serialized by mu: the chain is a linked list, so two concurrent
// writers reading the same head would produce two rows claiming the same
// predecessor and permanently fork the chain. This is correct for one hpd
// process, which is what HeroPanel runs today. A multi-node deployment (the
// Phase 10 HA path) needs the head to be claimed in the database instead — a
// SELECT ... FOR UPDATE on MariaDB — and this comment is the marker for that
// work.
type Service struct {
	mu     sync.Mutex
	repo   Repo
	prev   string
	loaded bool
}

// NewService constructs a Service over repo.
func NewService(repo Repo) *Service { return &Service{repo: repo} }

// Record commits r to the chain.
func (s *Service) Record(ctx context.Context, r Record) (Entry, error) {
	if s == nil || s.repo == nil {
		return Entry{}, nil // auditing is not configured (no datastore)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Load the head once, lazily: hpd must boot even when the audit table is
	// momentarily unreachable, and the first write is a natural retry point.
	if !s.loaded {
		head, err := s.repo.Head(ctx)
		if err != nil {
			return Entry{}, err
		}
		s.prev = head
		s.loaded = true
	}

	at := r.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	detail := r.Detail
	if detail == "" {
		detail = emptyDetail
	}
	outcome := r.Outcome
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	kind := r.ActorKind
	if kind == "" {
		kind = ActorAnonymous
	}

	e := Entry{
		UID:          idgen.NewULID(),
		ActorUserID:  r.ActorUserID,
		ActorIP:      r.ActorIP,
		ActorKind:    kind,
		Action:       r.Action,
		ResourceType: r.ResourceType,
		ResourceID:   r.ResourceID,
		Outcome:      outcome,
		Detail:       detail,
		PrevHash:     s.prev,
		CreatedAt:    FormatTime(at),
	}
	e.RowHash = hashEntry(e)

	if err := s.repo.Append(ctx, &e); err != nil {
		// Leave s.prev where it was. Advancing it after a failed insert would
		// orphan the next row: it would point at a predecessor that does not
		// exist, and Verify would report a break for a row that is innocent.
		return Entry{}, err
	}
	s.prev = e.RowHash
	return e, nil
}

// List returns entries newest-first.
func (s *Service) List(ctx context.Context, f Filter) ([]Entry, error) {
	if s == nil || s.repo == nil {
		return nil, errx.New(errx.KindUnavailable, "audit_unavailable", "The audit log is not available.")
	}
	return s.repo.List(ctx, f)
}

// Verify walks the whole chain from the beginning and reports the first break.
// It is deliberately not paginated: a partial verification that starts from a
// hash already in the table proves nothing, because that hash is exactly what an
// attacker rewriting the chain would have recomputed.
func (s *Service) Verify(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return errx.New(errx.KindUnavailable, "audit_unavailable", "The audit log is not available.")
	}
	entries, err := s.repo.List(ctx, Filter{Limit: -1, Offset: 0})
	if err != nil {
		return err
	}
	// List is newest-first; the chain reads oldest-first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return Verify(entries, "")
}
