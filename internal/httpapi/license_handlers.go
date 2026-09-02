package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/internal/audit"
	"github.com/thisisnkp/nexpanel/internal/license"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// The licence surface (docs/27).
//
// Reading is system.read, because every screen in the panel needs to know
// whether to show the banner. Activating, refreshing and deactivating are
// system.write and force-audited: which key this machine runs under is a
// commercial fact about the installation, and the audit row is the only
// durable record of who changed it.

// licenseView is what the panel and the CLI render. It is Status plus the two
// things that come from outside the ladder: the banner text and when the server
// was last reached.
type licenseView struct {
	license.Status
	Banner      string    `json:"banner"`
	LastContact time.Time `json:"last_contact,omitzero"`
	// Enforced is false in a build that pins no signing key. Reported rather
	// than hidden: a panel that shows "active" while enforcing nothing would be
	// telling an operator something untrue about their own installation.
	Enforced bool `json:"enforced"`
}

func viewOf(svc *license.Service) licenseView {
	st := svc.Status()
	return licenseView{
		Status:      st,
		Banner:      st.Banner(),
		LastContact: svc.LastContact(),
		Enforced:    svc.Pinned(),
	}
}

// getLicenseHandler reports where this installation sits on the ladder.
//
// It never fails and never calls the network: the answer comes from the token
// on disk. A licence screen that spins because the licence server is slow is a
// licence screen nobody can use during exactly the outage they opened it for.
func getLicenseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.License == nil {
			writeJSON(w, r, http.StatusOK, licenseView{
				Status: license.Status{State: license.StateActive, Activated: true,
					Reason: "this build does not enforce licensing"},
				Enforced: false,
			})
			return
		}
		writeJSON(w, r, http.StatusOK, viewOf(d.License))
	}
}

// activateLicenseHandler binds this machine to a key.
func activateLicenseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.License == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "license_unavailable",
				"This build does not enforce licensing, so there is nothing to activate."))
			return
		}
		var req struct {
			Key string `json:"key"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Key) == "" {
			writeError(w, r, errx.Validation("key_required", "A licence key is required."))
			return
		}
		// The key itself is never audited — it is a bearer credential, and an
		// audit trail is exactly the log an attacker reads. That this machine
		// was activated, and by whom, is the fact worth keeping.
		audit.AddDetail(r.Context(), "action", "activate")

		st, err := d.License.Activate(r.Context(), req.Key)
		if err != nil {
			writeError(w, r, activationError(err))
			return
		}
		audit.AddDetail(r.Context(), "lid", st.LID)
		audit.AddDetail(r.Context(), "plan", st.Plan)
		writeJSON(w, r, http.StatusOK, viewOf(d.License))
	}
}

// refreshLicenseHandler is the "Refresh licence" button: one heartbeat, now.
//
// A failure here is reported but is *not* a licence failure — the response
// still carries the current state, which is whatever the stored lease says.
// An operator pressing refresh during an outage should see "could not reach the
// licence server", not a panel that has decided it is unlicensed.
func refreshLicenseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.License == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "license_unavailable",
				"This build does not enforce licensing."))
			return
		}
		if _, err := d.License.Refresh(r.Context()); err != nil {
			view := viewOf(d.License)
			view.Reason = "could not reach the licence server: " + err.Error()
			writeJSON(w, r, http.StatusOK, view)
			return
		}
		writeJSON(w, r, http.StatusOK, viewOf(d.License))
	}
}

// deactivateLicenseHandler releases this machine's activation slot, which is
// what an operator does before decommissioning a server.
func deactivateLicenseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.License == nil {
			writeError(w, r, errx.New(errx.KindUnavailable, "license_unavailable",
				"This build does not enforce licensing."))
			return
		}
		if err := d.License.Deactivate(r.Context()); err != nil {
			writeError(w, r, activationError(err))
			return
		}
		writeJSON(w, r, http.StatusOK, viewOf(d.License))
	}
}

// activationError turns the licence server's codes into the panel's error
// envelope, keeping the sentence the server wrote.
//
// The server's messages are written for the person holding the key — "all your
// activation slots are in use, release one" — and rewriting them here would
// mean two places to keep in step and one of them going stale.
func activationError(err error) error {
	var se *license.ServerError
	if !errors.As(err, &se) {
		// Not a refusal: the network, DNS, or something that is not the licence
		// server. Unavailable rather than internal, because nothing is broken
		// here and the operator's next step is to try again.
		return errx.Wrap(err, errx.KindUnavailable, "license_unreachable",
			"Could not reach the licence server. Nothing has changed; try again shortly.")
	}
	switch se.Code {
	case license.CodeInvalidKey:
		return errx.Validation("invalid_key", se.Message)
	case license.CodeKeyRevoked, license.CodeSubscriptionExpired,
		license.CodeSeatLimitReached, license.CodeFingerprintChangeLimit:
		return errx.New(errx.KindPaymentRequired, strings.ToLower(se.Code), se.Message)
	case license.CodeRateLimited:
		return errx.New(errx.KindUnavailable, "rate_limited", se.Message)
	default:
		return errx.New(errx.KindUpstream, strings.ToLower(se.Code), se.Message)
	}
}

// lockedKey carries a locked licence into the request, for requirePermission to
// act on *after* it has decided the caller is allowed at all.
type lockedCtxKey struct{}

// licenseLock marks a request that a lapsed licence would refuse.
//
// It marks rather than refuses, and the ordering is the reason. Authorisation
// has to be answered first: a caller who was never allowed to create a site
// must be told "you do not have permission", not "renew your licence" — the
// second is misleading, and it leaks the installation's commercial state to
// anyone who can reach the API. So this sets a flag and requirePermission
// enforces it once the permission check has passed, giving 401, then 403, then
// 402, in that order.
//
// What it does **not** touch is the point of it: no service is stopped, no site
// is unpublished, no data is removed. The panel's own control surface narrows
// to the licence routes and the ones needed to reach them, and everything a
// customer's visitors touch carries on exactly as before — served by
// OpenLiteSpeed and php-fpm, which have never heard of this middleware.
//
// Deliberately *not* the only enforcement. It is a UI-shaped gate on one
// router; the feature handlers each check for themselves (see
// internal/license), so removing this middleware would narrow the blast radius
// of a lock, not lift it.
func licenseLock(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d.License == nil || lockAllowed(r) {
				next.ServeHTTP(w, r)
				return
			}
			if d.License.Status().State == license.StateLocked {
				r = r.WithContext(context.WithValue(r.Context(), lockedCtxKey{}, true))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// lockedError is the refusal a locked panel gives, or nil. Read by
// requirePermission after it has satisfied itself the caller is allowed.
func lockedError(r *http.Request) error {
	if v, _ := r.Context().Value(lockedCtxKey{}).(bool); !v {
		return nil
	}
	return errx.New(errx.KindPaymentRequired, "license_locked",
		"This licence has lapsed. The panel is limited to reactivation until it is renewed. "+
			"Your websites, mail, databases and backups are unaffected and still running.")
}

// lockAllowed lists what still answers while locked.
//
// Short on purpose, and every entry earns its place: without the auth routes
// nobody can sign in to fix it; without the licence routes there is nothing to
// fix it with; without system/info the SPA cannot boot far enough to render the
// page that says what is wrong.
func lockAllowed(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/v1/auth/"):
		return true
	case strings.HasPrefix(p, "/api/v1/system/license"):
		return true
	case p == "/api/v1/system/info", p == "/api/v1/openapi.json":
		return true
	}
	return false
}
