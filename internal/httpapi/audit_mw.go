package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
)

// auditor records every mutating request to the hash-chained audit log.
//
// This is middleware, not a call in each handler, on purpose. "Audit every
// mutation" enforced by convention is a rule that holds until the first person
// forgets — and the panel shipped three modules' worth of handlers, and three
// docs claiming audit coverage, before anyone noticed nothing was writing to the
// table at all. As middleware the property is structural: a route added tomorrow
// is audited because it is mounted, and the only way to lose that is to
// deliberately mount outside this group.
//
// What it can infer alone is the route, the actor, and the outcome. Handlers add
// what only they know (which uid a POST created) via audit.SetResource /
// audit.AddDetail.
func auditor(svc *audit.Service, log *slog.Logger) mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				next.ServeHTTP(w, r)
				return
			}

			ann := audit.NewAnnotation()
			r = r.WithContext(audit.WithAnnotation(r.Context(), ann))
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			if !isUnsafeMethod(r.Method) && !ann.Forced() {
				return
			}

			rec := audit.Record{
				ActorIP:   clientIP(r),
				Action:    auditAction(r),
				Outcome:   outcomeFor(ww.Status()),
				Detail:    ann.DetailJSON(),
				ActorKind: audit.ActorAnonymous,
			}
			resType, resID := ann.Resource()
			if resType == "" {
				resType = resourceTypeFromPattern(r)
			}
			if resID == "" {
				resID = chi.URLParam(r, "uid")
			}
			rec.ResourceType, rec.ResourceID = resType, resID

			if uid, kind, ok := ann.Actor(); ok {
				rec.ActorUserID, rec.ActorKind = uid, kind
			} else if p, ok := auth.FromContext(r.Context()); ok {
				rec.ActorUserID = p.UserID
				rec.ActorKind = actorKindOf(p.Kind)
			}

			// The mutation already happened and the response is already on the
			// wire, so a failed audit write cannot fail the request — by the time
			// we get here there is nothing left to refuse. Log it loudly instead:
			// an operator seeing this line knows the chain has a hole, which is
			// the honest outcome. Recording an intent row *before* the handler
			// would close that window at the cost of doubling the table; the
			// broker's own chain already covers the privileged half of any such
			// gap, so this is the deliberate trade.
			if _, err := svc.Record(r.Context(), rec); err != nil {
				log.Error("audit write failed; this mutation is not in the chain",
					"action", rec.Action, "resource_id", rec.ResourceID, "error", err)
			}
		})
	}
}

// auditAction names the action as "<METHOD> <route pattern>", e.g.
// "POST /api/v1/sites/{uid}/git/deploy".
//
// The pattern is used rather than the concrete path so that every call to one
// endpoint groups under one action, instead of scattering across a distinct
// action string per uid.
func auditAction(r *http.Request) string {
	pattern := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		pattern = rc.RoutePattern()
	}
	if pattern == "" {
		pattern = r.URL.Path
	}
	return r.Method + " " + pattern
}

// resourceTypeFromPattern takes the first path segment below /api/v1 as the
// resource type: /api/v1/sites/{uid}/git → "sites".
func resourceTypeFromPattern(r *http.Request) string {
	pattern := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		pattern = rc.RoutePattern()
	}
	if pattern == "" {
		pattern = r.URL.Path
	}
	pattern = strings.TrimPrefix(pattern, "/api/v1")
	for _, seg := range strings.Split(strings.Trim(pattern, "/"), "/") {
		if seg != "" && !strings.HasPrefix(seg, "{") {
			return seg
		}
	}
	return ""
}

// outcomeFor maps a status code onto an outcome. 401/403 are called out
// separately from other failures: a refused attempt is the signal a reviewer
// scans for, and it should not be buried among validation errors.
func outcomeFor(status int) audit.Outcome {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return audit.OutcomeDenied
	case status >= 400:
		return audit.OutcomeFailure
	case status == 0:
		// The handler never wrote a status. net/http would send 200, so record
		// what the client actually sees rather than an implausible zero.
		return audit.OutcomeSuccess
	default:
		return audit.OutcomeSuccess
	}
}

func actorKindOf(k auth.Kind) audit.ActorKind {
	if k == auth.KindAPIKey {
		return audit.ActorAPIKey
	}
	return audit.ActorUser
}
