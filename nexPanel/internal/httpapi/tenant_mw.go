package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/nexpanel/internal/auth"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// tenantGuard enforces reseller-tenant isolation on every owned, uid-addressed
// resource route.
//
// Like the auditor, this is middleware rather than a call in each handler on
// purpose: there are dozens of /sites/{uid}/…, /dns/zones/{uid}/…,
// /mail/domains/{uid}/… routes, and "check the caller owns this resource"
// enforced by convention is a rule that holds until the first new route forgets
// it. As group middleware the property is structural — every current and future
// sub-route is covered because it is mounted, and the only way to lose it is to
// mount outside this group.
//
// Each entry pins a route-pattern prefix to the resource kind its uid names and
// the permissions that gate it. The guard runs before the per-route permission
// gate, so it defers to it: a caller who does not even hold the resource's read
// or write permission is passed through untouched, and requirePermission
// produces the authoritative 401/403. Tenancy is an *additional* constraint on
// top of having the permission. Nested resources (a DNS record, a mailbox)
// resolve their owner through the parent, so isolation holds at the grain the
// resource is addressed.
//
// A denied or unknown resource is reported as 404, so the boundary never leaks
// which resources belong to other tenants.
type tenantRule struct {
	prefix    string
	kind      repository.ResourceKind
	readPerm  string
	writePerm string
}

// tenantRules pairs a route-pattern prefix with the resource it addresses. Order
// does not matter: the prefixes are mutually distinct (records vs zones,
// accounts vs domains, databases vs database-users), so at most one matches.
var tenantRules = []tenantRule{
	{"/api/v1/sites/{uid}", repository.KindSite, "site.read", "site.write"},
	{"/api/v1/dns/zones/{uid}", repository.KindDNSZone, "dns.read", "dns.write"},
	{"/api/v1/dns/records/{uid}", repository.KindDNSRecord, "dns.read", "dns.write"},
	{"/api/v1/mail/domains/{uid}", repository.KindMailDomain, "mail.read", "mail.write"},
	{"/api/v1/mail/accounts/{uid}", repository.KindMailAccount, "mail.read", "mail.write"},
	{"/api/v1/databases/{uid}", repository.KindDBInstance, "database.read", "database.write"},
}

func tenantGuard(d Deps) mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Tenancy not wired (no datastore): behave exactly as before (admins
			// only), never fail closed.
			if !d.Tenancy.Enabled() {
				next.ServeHTTP(w, r)
				return
			}
			p, ok := auth.FromContext(r.Context())
			if !ok || p.Can("*") {
				// Anonymous (rejected later by requireAuth) or a superuser (who sees
				// every tenant): no scoping to apply.
				next.ServeHTTP(w, r)
				return
			}
			pattern := ""
			if rc := chi.RouteContext(r.Context()); rc != nil {
				pattern = rc.RoutePattern()
			}
			rule, matched := matchTenantRule(pattern)
			if !matched {
				next.ServeHTTP(w, r)
				return
			}
			// Defer to the permission gate for callers without the resource's access.
			if !p.Can(rule.readPerm) && !p.Can(rule.writePerm) {
				next.ServeHTTP(w, r)
				return
			}

			ownerID, err := d.Tenancy.OwnerOf(r.Context(), rule.kind, chi.URLParam(r, "uid"))
			if err != nil {
				writeError(w, r, err) // 404 for an unknown resource
				return
			}
			allowed, err := d.Tenancy.CanAccessOwner(r.Context(), p.UserID, ownerID)
			if err != nil {
				writeError(w, r, err)
				return
			}
			if !allowed {
				// Indistinguishable from "no such resource" — the tenant boundary
				// must not disclose the existence of another tenant's resources.
				writeAPIError(w, r, http.StatusNotFound, "resource_not_found", "No such resource.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// assertBodyResourceInTenant enforces tenancy on a resource named in a request
// *body* rather than the {uid} path — which tenantGuard never sees. The grant and
// revoke endpoints are the case: the {uid} guards the database instance, but the
// db_user they act on is a second owned resource carried in the body, so without
// this a reseller could grant a foreign tenant's user onto their own instance.
// A superuser (or an unwired panel) bypasses; a foreign or unknown resource is
// 404, matching the guard's non-leak convention.
func assertBodyResourceInTenant(d Deps, r *http.Request, kind repository.ResourceKind, uid string) error {
	if !d.Tenancy.Enabled() {
		return nil
	}
	p, ok := auth.FromContext(r.Context())
	if !ok || p == nil || p.Can("*") {
		return nil
	}
	ownerID, err := d.Tenancy.OwnerOf(r.Context(), kind, uid)
	if err != nil {
		return err // 404 for an unknown resource
	}
	allowed, err := d.Tenancy.CanAccessOwner(r.Context(), p.UserID, ownerID)
	if err != nil {
		return err
	}
	if !allowed {
		return errx.NotFound("resource_not_found", "No such resource.")
	}
	return nil
}

// visibleOwners reports how a list endpoint should be scoped for the caller:
// superuser (list everything) or the set of owner ids in their tenant subtree.
// With tenancy unwired it reports superuser, preserving pre-tenancy behavior.
func visibleOwners(d Deps, r *http.Request) (superuser bool, owners []int64, err error) {
	p, ok := auth.FromContext(r.Context())
	if !ok || p == nil {
		return true, nil, nil // unreachable behind requirePermission; list-all is the safe default
	}
	if p.Can("*") || !d.Tenancy.Enabled() {
		return true, nil, nil
	}
	ids, err := d.Tenancy.VisibleOwnerIDs(r.Context(), p.UserID)
	return false, ids, err
}

// listForTenant runs a per-owner list function once for a superuser (ownerID 0 =
// all) or once per visible owner and concatenates the results — the tenant-scoped
// listing built on each service's existing single-owner List, with no change to
// its repository contract.
func listForTenant[T any](d Deps, r *http.Request, listFn func(ownerID int64) ([]T, error)) ([]T, error) {
	super, owners, err := visibleOwners(d, r)
	if err != nil {
		return nil, err
	}
	if super {
		return listFn(0)
	}
	out := []T{}
	for _, ownerID := range owners {
		items, err := listFn(ownerID)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

// matchTenantRule finds the rule whose prefix the route pattern starts with.
func matchTenantRule(pattern string) (tenantRule, bool) {
	for _, rule := range tenantRules {
		if strings.HasPrefix(pattern, rule.prefix) {
			return rule, true
		}
	}
	return tenantRule{}, false
}
