package audit

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// Annotation is a per-request scratchpad the HTTP edge creates and any layer
// below may enrich.
//
// The edge can only see what the URL and status code say. It knows a POST to
// /sites succeeded; it cannot know which site was created, because the uid is
// minted inside the service. Rather than teach the middleware about every route,
// the handler that does know says so. Everything here is optional: a route that
// annotates nothing is still audited, just more coarsely.
type Annotation struct {
	mu           sync.Mutex
	resourceType string
	resourceID   string
	actorUserID  int64
	actorKind    ActorKind
	actorSet     bool
	detail       map[string]any
	force        bool
}

// NewAnnotation constructs an empty Annotation.
func NewAnnotation() *Annotation { return &Annotation{} }

type annotationKey struct{}

// WithAnnotation returns a context carrying a.
func WithAnnotation(ctx context.Context, a *Annotation) context.Context {
	return context.WithValue(ctx, annotationKey{}, a)
}

// annotationFrom returns the annotation on ctx, if any. All the mutators below
// are no-ops without one, so a service called outside an HTTP request (a
// scheduler, a test) does not need to care.
func annotationFrom(ctx context.Context) *Annotation {
	a, _ := ctx.Value(annotationKey{}).(*Annotation)
	return a
}

// SetResource records which resource the request acted on.
func SetResource(ctx context.Context, resourceType, resourceID string) {
	a := annotationFrom(ctx)
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if resourceType != "" {
		a.resourceType = resourceType
	}
	if resourceID != "" {
		a.resourceID = resourceID
	}
}

// SetActor records who acted, for the requests the edge cannot work it out from.
// A login is the motivating case: the principal does not exist in the request
// context yet — creating it *is* the request — so without this every successful
// login would be filed as anonymous.
func SetActor(ctx context.Context, userID int64, kind ActorKind) {
	a := annotationFrom(ctx)
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actorUserID = userID
	a.actorKind = kind
	a.actorSet = true
}

// AddDetail attaches a key/value pair to the entry's detail JSON.
//
// Never pass a secret: audit rows are readable by any operator with audit.read
// and are meant to be exported to a SIEM. Pass the fact, not the credential —
// "auth_kind":"ssh_key", never the key.
func AddDetail(ctx context.Context, key string, value any) {
	a := annotationFrom(ctx)
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.detail == nil {
		a.detail = make(map[string]any, 4)
	}
	a.detail[key] = value
}

// Force marks a request for auditing that would not otherwise qualify.
//
// The edge audits every unsafe method, which is the right default but not a
// complete rule: GET /databases/{uid}/export changes nothing and copies the
// entire database to the caller. Reads that disclose that much are audited by
// asking.
func Force(ctx context.Context) {
	a := annotationFrom(ctx)
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.force = true
}

// Forced reports whether the request opted in to auditing.
func (a *Annotation) Forced() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.force
}

// Resource returns the annotated resource type and id.
func (a *Annotation) Resource() (string, string) {
	if a == nil {
		return "", ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resourceType, a.resourceID
}

// Actor returns the annotated actor and whether one was set.
func (a *Annotation) Actor() (int64, ActorKind, bool) {
	if a == nil {
		return 0, "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.actorUserID, a.actorKind, a.actorSet
}

// DetailJSON renders the accumulated detail as a JSON object, or "" if empty.
// Keys are sorted so the same annotations always hash to the same bytes —
// Go randomizes map iteration, and the detail is covered by row_hash.
func (a *Annotation) DetailJSON() string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.detail) == 0 {
		return ""
	}
	keys := make([]string, 0, len(a.detail))
	for k := range a.detail {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			continue
		}
		vb, err := json.Marshal(a.detail[k])
		if err != nil {
			// A value that will not marshal must not discard the whole entry.
			vb = []byte(`"<unencodable>"`)
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return string(buf)
}
