// Package domain manages a site's domains: the primary domain, aliases and
// subdomains that map to the same vhost, redirect domains that 301 elsewhere,
// and the force-HTTPS toggle. Changing any of them re-renders the site's
// web-server config (via a hook into the site service). See docs/10 Phase 2.
package domain

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Kinds of domain attached to a site.
const (
	KindPrimary  = "primary"  // the site's canonical domain (exactly one, not removable)
	KindAlias    = "alias"    // an extra domain/subdomain serving the same content
	KindRedirect = "redirect" // a domain that redirects to RedirectTo
)

var validKinds = map[string]bool{KindAlias: true, KindRedirect: true}

// validRedirectCodes is the allowlist of HTTP redirect statuses.
var validRedirectCodes = map[int]bool{301: true, 302: true, 307: true, 308: true}

// Domain is the API view of a site domain.
type Domain struct {
	UID          string `json:"uid"`
	FQDN         string `json:"fqdn"`
	Kind         string `json:"kind"`
	ForceHTTPS   bool   `json:"force_https"`
	RedirectTo   string `json:"redirect_to,omitempty"`
	RedirectCode int    `json:"redirect_code,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// Row is the persistence row.
type Row struct {
	ID           int64  `db:"id"`
	UID          string `db:"uid"`
	SiteID       int64  `db:"site_id"`
	FQDN         string `db:"fqdn"`
	Kind         string `db:"kind"`
	ForceHTTPS   bool   `db:"force_https"`
	RedirectTo   string `db:"redirect_to"`
	RedirectCode int    `db:"redirect_code"`
	CreatedAt    string `db:"created_at"`
}

// Repo is the persistence contract (implemented by internal/repository).
type Repo interface {
	Insert(ctx context.Context, r *Row) error
	ListBySiteID(ctx context.Context, siteID int64) ([]Row, error)
	GetByUID(ctx context.Context, uid string) (*Row, error)
	Delete(ctx context.Context, uid string) error
	SetForceHTTPSForSite(ctx context.Context, siteID int64, on bool) error
}

// SiteRef is the minimal site identity the domain service needs.
type SiteRef struct {
	ID  int64
	UID string
}

// Sites resolves a site by UID (adapter over the site repository).
type Sites interface {
	Resolve(ctx context.Context, siteUID string) (*SiteRef, error)
}

// AddInput is the request to attach a domain to a site.
type AddInput struct {
	FQDN         string
	Kind         string
	RedirectTo   string
	RedirectCode int
}

// reFQDN is a strict domain check (also used to build web-server config).
var reFQDN = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func validateAdd(in *AddInput) error {
	in.FQDN = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(in.FQDN, ".")))
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.RedirectTo = strings.TrimSpace(in.RedirectTo)

	if !reFQDN.MatchString(in.FQDN) {
		return errx.Validation("invalid_domain", "A valid domain name is required.",
			errx.Field{Field: "fqdn", Code: "invalid", Message: "invalid domain"})
	}
	if in.Kind == "" {
		in.Kind = KindAlias
	}
	if !validKinds[in.Kind] {
		return errx.Validation("invalid_kind", "Kind must be alias or redirect.",
			errx.Field{Field: "kind", Code: "unsupported", Message: in.Kind})
	}
	if in.Kind == KindRedirect {
		if in.RedirectCode == 0 {
			in.RedirectCode = 301
		}
		if !validRedirectCodes[in.RedirectCode] {
			return errx.Validation("invalid_redirect_code", "Redirect code must be 301, 302, 307, or 308.")
		}
		// A full absolute URL keeps the rendered rewrite unambiguous.
		u, err := url.Parse(in.RedirectTo)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			strings.ContainsAny(in.RedirectTo, " \t\r\n\"") {
			return errx.Validation("invalid_redirect_to",
				"redirect_to must be an absolute http(s) URL, e.g. https://example.com.",
				errx.Field{Field: "redirect_to", Code: "invalid", Message: "must be an absolute URL"})
		}
		in.RedirectTo = strings.TrimSuffix(in.RedirectTo, "/")
	} else {
		in.RedirectTo, in.RedirectCode = "", 0
	}
	return nil
}
