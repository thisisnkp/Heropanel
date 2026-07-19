package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
)

type recordingRepo struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (r *recordingRepo) Append(_ context.Context, e *audit.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, *e)
	return nil
}

func (r *recordingRepo) Head(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == 0 {
		return "", nil
	}
	return r.entries[len(r.entries)-1].RowHash, nil
}

func (r *recordingRepo) List(context.Context, audit.Filter) ([]audit.Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Entry(nil), r.entries...), nil
}

func (r *recordingRepo) only(t *testing.T) audit.Entry {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) != 1 {
		t.Fatalf("got %d audit entries, want exactly 1", len(r.entries))
	}
	return r.entries[0]
}

func (r *recordingRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// auditRig mounts h at pattern behind the auditor, as the router does.
func auditRig(pattern string, h http.HandlerFunc) (*chi.Mux, *recordingRepo) {
	repo := &recordingRepo{}
	svc := audit.NewService(repo)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	r := chi.NewRouter()
	r.Use(auditor(svc, quiet))
	r.Handle(pattern, h)
	return r, repo
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestAuditorRecordsEveryMutation(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			r, repo := auditRig("/api/v1/databases/{uid}", okHandler)
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/api/v1/databases/db_123", nil))

			e := repo.only(t)
			if want := method + " /api/v1/databases/{uid}"; e.Action != want {
				t.Errorf("Action = %q, want %q", e.Action, want)
			}
			if e.ResourceType != "databases" {
				t.Errorf("ResourceType = %q, want %q", e.ResourceType, "databases")
			}
			if e.ResourceID != "db_123" {
				t.Errorf("ResourceID = %q, want the uid from the URL", e.ResourceID)
			}
			if e.Outcome != audit.OutcomeSuccess {
				t.Errorf("Outcome = %q, want %q", e.Outcome, audit.OutcomeSuccess)
			}
		})
	}
}

// A read changes nothing; auditing every GET would bury the signal in traffic.
func TestAuditorIgnoresReads(t *testing.T) {
	r, repo := auditRig("/api/v1/databases", okHandler)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/databases", nil))

	if n := repo.count(); n != 0 {
		t.Errorf("got %d audit entries for a GET, want 0", n)
	}
}

// ...unless the read discloses enough to be worth a row (the export path).
func TestAuditorRecordsAForcedRead(t *testing.T) {
	r, repo := auditRig("/api/v1/databases/{uid}/export", func(w http.ResponseWriter, req *http.Request) {
		audit.Force(req.Context())
		w.WriteHeader(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/databases/db_9/export", nil))

	e := repo.only(t)
	if e.Action != "GET /api/v1/databases/{uid}/export" {
		t.Errorf("Action = %q", e.Action)
	}
	if e.ResourceID != "db_9" {
		t.Errorf("ResourceID = %q, want db_9", e.ResourceID)
	}
}

// The entry a reviewer actually goes looking for.
func TestAuditorRecordsARefusedRequestAsDenied(t *testing.T) {
	r, repo := auditRig("/api/v1/sites", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil))

	if e := repo.only(t); e.Outcome != audit.OutcomeDenied {
		t.Errorf("Outcome = %q, want %q", e.Outcome, audit.OutcomeDenied)
	}
}

func TestAuditorRecordsAFailureAsFailure(t *testing.T) {
	r, repo := auditRig("/api/v1/sites", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil))

	if e := repo.only(t); e.Outcome != audit.OutcomeFailure {
		t.Errorf("Outcome = %q, want %q", e.Outcome, audit.OutcomeFailure)
	}
}

func TestAuditorAttributesTheRequestToItsPrincipal(t *testing.T) {
	r, repo := auditRig("/api/v1/sites", okHandler)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{UserID: 42, Kind: auth.KindUser}))
	r.ServeHTTP(httptest.NewRecorder(), req)

	e := repo.only(t)
	if e.ActorUserID != 42 {
		t.Errorf("ActorUserID = %d, want 42", e.ActorUserID)
	}
	if e.ActorKind != audit.ActorUser {
		t.Errorf("ActorKind = %q, want %q", e.ActorKind, audit.ActorUser)
	}
}

// A key acting on its own is a different event from a human at a keyboard.
func TestAuditorDistinguishesAnAPIKeyFromAUser(t *testing.T) {
	r, repo := auditRig("/api/v1/sites", okHandler)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), &auth.Principal{UserID: 42, Kind: auth.KindAPIKey}))
	r.ServeHTTP(httptest.NewRecorder(), req)

	if e := repo.only(t); e.ActorKind != audit.ActorAPIKey {
		t.Errorf("ActorKind = %q, want %q", e.ActorKind, audit.ActorAPIKey)
	}
}

func TestAuditorRecordsAnUnauthenticatedCallerAsAnonymous(t *testing.T) {
	r, repo := auditRig("/hooks/git/{uid}", okHandler)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/hooks/git/src_1", nil))

	e := repo.only(t)
	if e.ActorKind != audit.ActorAnonymous {
		t.Errorf("ActorKind = %q, want %q", e.ActorKind, audit.ActorAnonymous)
	}
	if e.ActorUserID != 0 {
		t.Errorf("ActorUserID = %d, want 0", e.ActorUserID)
	}
}

func TestHandlerAnnotationsReachTheEntry(t *testing.T) {
	r, repo := auditRig("/api/v1/sites", func(w http.ResponseWriter, req *http.Request) {
		// A POST /sites cannot know the uid from the URL — only the service does.
		audit.SetResource(req.Context(), "sites", "site_abc")
		audit.AddDetail(req.Context(), "primary_domain", "example.com")
		w.WriteHeader(http.StatusCreated)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil))

	e := repo.only(t)
	if e.ResourceID != "site_abc" {
		t.Errorf("ResourceID = %q, want the annotated uid", e.ResourceID)
	}
	if !strings.Contains(e.Detail, `"primary_domain":"example.com"`) {
		t.Errorf("Detail = %s, want the annotated domain", e.Detail)
	}
}

// A login has no principal on the way in — creating one is the request.
func TestHandlerCanNameTheActorForALogin(t *testing.T) {
	r, repo := auditRig("/api/v1/auth/login", func(w http.ResponseWriter, req *http.Request) {
		audit.SetActor(req.Context(), 5, audit.ActorUser)
		w.WriteHeader(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))

	e := repo.only(t)
	if e.ActorUserID != 5 || e.ActorKind != audit.ActorUser {
		t.Errorf("actor = (%d, %q), want (5, %q)", e.ActorUserID, e.ActorKind, audit.ActorUser)
	}
}

// Requests through the router must chain, not just be recorded.
func TestRequestsThroughTheEdgeFormAVerifiableChain(t *testing.T) {
	r, repo := auditRig("/api/v1/sites/{uid}", okHandler)
	for _, uid := range []string{"a", "b", "c"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, "/api/v1/sites/"+uid, nil))
	}
	if got := repo.count(); got != 3 {
		t.Fatalf("got %d entries, want 3", got)
	}
	if err := audit.Verify(repo.entries, ""); err != nil {
		t.Errorf("chain built through the edge does not verify: %v", err)
	}
}

// hpd runs without a datastore in dev; the edge must still serve.
func TestAuditorWithNoServicePassesTheRequestThrough(t *testing.T) {
	r := chi.NewRouter()
	r.Use(auditor(nil, slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.Post("/api/v1/sites", okHandler)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with auditing disabled", rec.Code)
	}
}

// The audit write happens after the response; a repository failure must not
// change what the client already received.
func TestAuditFailureDoesNotBreakTheResponse(t *testing.T) {
	svc := audit.NewService(failingRepo{})
	r := chi.NewRouter()
	r.Use(auditor(svc, slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.Post("/api/v1/sites", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil))
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 even though the audit write failed", rec.Code)
	}
}

type failingRepo struct{}

func (failingRepo) Append(context.Context, *audit.Entry) error { return io.ErrUnexpectedEOF }
func (failingRepo) Head(context.Context) (string, error)       { return "", nil }
func (failingRepo) List(context.Context, audit.Filter) ([]audit.Entry, error) {
	return nil, nil
}
