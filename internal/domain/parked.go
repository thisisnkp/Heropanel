package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Parked domains: the missing "registrar" half of domain management.
//
// A Row (above) only exists once it is attached to a site. This is the other
// case — a domain the operator has registered ownership of in the panel with
// NO site yet, proven via a DNS TXT challenge at whatever DNS host the operator
// actually uses (HeroPanel does not need to be authoritative for it). Once
// verified, it becomes a "free" domain offered when creating a site: pick it
// and it attaches with no warning, because ownership is already proven.
//
// Trust extends to subdomains without a separate parked row for each one: once
// `acme.com` is verified, `blog.acme.com` is trusted too, on the assumption the
// operator followed the wildcard-record recommendation shown alongside the
// challenge. HeroPanel does not verify the wildcard itself — only the TXT
// ownership record is actually checked.

// Parked-domain status.
const (
	ParkedUnverified = "unverified"
	ParkedVerified   = "verified"
)

// challengeLabel is the DNS label the ownership challenge is published under.
const challengeLabel = "_heropanel-challenge"

// ParkedDomain is the API view of a parked domain.
type ParkedDomain struct {
	UID            string `json:"uid"`
	FQDN           string `json:"fqdn"`
	Status         string `json:"status"`
	ChallengeName  string `json:"challenge_name"`
	ChallengeValue string `json:"challenge_value"`
	WildcardHint   string `json:"wildcard_hint"`
	Attached       bool   `json:"attached"`
	VerifiedAt     string `json:"verified_at,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// ParkedRow is the persistence row.
type ParkedRow struct {
	ID             int64  `db:"id"`
	UID            string `db:"uid"`
	OwnerID        int64  `db:"owner_id"`
	FQDN           string `db:"fqdn"`
	Status         string `db:"status"`
	ChallengeToken string `db:"challenge_token"`
	VerifiedAt     string `db:"verified_at"`
	SiteID         int64  `db:"site_id"` // 0 = unattached (nullable column, scanned as 0)
	CreatedAt      string `db:"created_at"`
}

// ParkedRepo is the persistence contract (implemented by internal/repository).
type ParkedRepo interface {
	InsertParked(ctx context.Context, r *ParkedRow) error
	GetParkedByUID(ctx context.Context, uid string) (*ParkedRow, error)
	GetParkedByFQDN(ctx context.Context, fqdn string) (*ParkedRow, error)
	ListParked(ctx context.Context, ownerID int64) ([]ParkedRow, error)
	SetParkedVerified(ctx context.Context, uid string, verifiedAt string) error
	AttachParked(ctx context.Context, uid string, siteID int64) error
	DeleteParked(ctx context.Context, uid string) error

	// ListVerifiedFQDNs / ListActiveZoneNames return the FULL trusted sets,
	// regardless of whether they are currently attached to a site — ownership of
	// a domain (and its subdomains) is proven independently of what is using it
	// right now.
	ListVerifiedFQDNs(ctx context.Context, ownerID int64) ([]string, error)
	ListActiveZoneNames(ctx context.Context, ownerID int64) ([]string, error)
	// ListAttachedFQDNs returns every domain already in use by a site (primary
	// or extra), so the free-domain pool can exclude them.
	ListAttachedFQDNs(ctx context.Context, ownerID int64) ([]string, error)
}

// resolver returns the resolver used for live verification lookups: the system
// resolver, or a pinned address for e2e against a local authoritative server,
// mirroring internal/mail's DKIM CheckDNS.
func (s *Service) resolver() *net.Resolver {
	if s.resolverAddr == "" {
		return net.DefaultResolver
	}
	addr := s.resolverAddr
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

func challengeName(fqdn string) string   { return challengeLabel + "." + fqdn }
func challengeValue(token string) string { return "heropanel-verify=" + token }
func wildcardHint(fqdn string) string {
	return "Recommended: point *." + fqdn + " (and " + fqdn + ") at this server so subdomains resolve automatically."
}

func newChallengeToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// lookupParked fetches a parked row by FQDN, returning (nil, nil) when none
// exists — callers branch on "does this already exist" without NotFound
// masquerading as a real failure, and without a real failure masquerading as
// "doesn't exist".
func (s *Service) lookupParked(ctx context.Context, fqdn string) (*ParkedRow, error) {
	row, err := s.parked.GetParkedByFQDN(ctx, fqdn)
	if err != nil {
		if errx.KindOf(err) == errx.KindNotFound {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

// Park registers ownership of a domain with no site attached — the ordinary
// "add a domain" action. Returns the challenge instructions the operator must
// publish at their own DNS host before Verify will succeed.
func (s *Service) Park(ctx context.Context, ownerID int64, fqdn string) (*ParkedDomain, error) {
	fqdn = normalizeFQDN(fqdn)
	if !reFQDN.MatchString(fqdn) {
		return nil, errx.Validation("invalid_domain", "A valid domain name is required.",
			errx.Field{Field: "fqdn", Code: "invalid", Message: "invalid domain"})
	}
	if s.parked == nil {
		return nil, errx.New(errx.KindUnavailable, "domain_parking_unavailable", "Domain parking is not available.")
	}
	existing, err := s.lookupParked(ctx, fqdn)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errx.New(errx.KindConflict, "already_parked", "That domain is already parked.")
	}
	token, err := newChallengeToken()
	if err != nil {
		return nil, errx.Internal(err)
	}
	row := &ParkedRow{OwnerID: ownerID, FQDN: fqdn, Status: ParkedUnverified, ChallengeToken: token}
	if err := s.parked.InsertParked(ctx, row); err != nil {
		return nil, err
	}
	return parkedView(row), nil
}

// VerifyParked re-checks the live DNS TXT challenge and, on a match, marks the
// domain verified. This asks the actual DNS, not the panel's own records — the
// whole point is proving the operator controls the domain wherever it really
// resolves.
func (s *Service) VerifyParked(ctx context.Context, uid string) (*ParkedDomain, error) {
	if s.parked == nil {
		return nil, errx.New(errx.KindUnavailable, "domain_parking_unavailable", "Domain parking is not available.")
	}
	row, err := s.parked.GetParkedByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if row.Status == ParkedVerified {
		return parkedView(row), nil // already verified: idempotent
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	txts, _ := s.resolver().LookupTXT(ctx, challengeName(row.FQDN))
	want := challengeValue(row.ChallengeToken)
	found := false
	for _, txt := range txts {
		if strings.TrimSpace(txt) == want {
			found = true
			break
		}
	}
	if !found {
		return nil, errx.New(errx.KindConflict, "dns_challenge_not_found",
			"The verification TXT record was not found. DNS changes can take time to propagate — please try again shortly.")
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := s.parked.SetParkedVerified(ctx, uid, now); err != nil {
		return nil, err
	}
	row.Status, row.VerifiedAt = ParkedVerified, now
	return parkedView(row), nil
}

// ListParked returns an owner's parked domains.
func (s *Service) ListParked(ctx context.Context, ownerID int64) ([]ParkedDomain, error) {
	if s.parked == nil {
		return []ParkedDomain{}, nil
	}
	rows, err := s.parked.ListParked(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]ParkedDomain, len(rows))
	for i := range rows {
		out[i] = *parkedView(&rows[i])
	}
	return out, nil
}

// DeleteParked unparks a domain. Blocked while it is attached to a site — the
// operator must remove it from the site first, so a domain currently serving
// traffic never silently loses its proof-of-ownership record.
func (s *Service) DeleteParked(ctx context.Context, uid string) error {
	if s.parked == nil {
		return errx.New(errx.KindUnavailable, "domain_parking_unavailable", "Domain parking is not available.")
	}
	row, err := s.parked.GetParkedByUID(ctx, uid)
	if err != nil {
		return err
	}
	if row.SiteID != 0 {
		return errx.New(errx.KindConflict, "domain_attached",
			"This domain is attached to a site; remove it from the site before unparking.")
	}
	return s.parked.DeleteParked(ctx, uid)
}

// Pool is what a create-site form needs to explain a domain the operator is
// typing. Free is what they can take outright; Trusted is every domain whose
// ownership is already proven here, *including* ones a site already uses.
//
// The second list is not redundant. "blog.acme.com" is a perfectly good new
// site even when acme.com itself is serving one — Classify accepts it, because
// ownership of the parent is what was proven. Without Trusted the form can see
// only that acme.com is unavailable, and would wrongly warn that the subdomain
// needs verifying.
type Pool struct {
	Free    []string `json:"fqdns"`
	Trusted []string `json:"trusted"`
}

// DomainPool returns both lists from a single pass, so they cannot disagree
// about what is trusted.
func (s *Service) DomainPool(ctx context.Context, ownerID int64) (*Pool, error) {
	if s.parked == nil {
		return &Pool{Free: []string{}, Trusted: []string{}}, nil
	}
	trusted, err := s.trustedDomains(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	attached, err := s.parked.ListAttachedFQDNs(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	attachedSet := make(map[string]bool, len(attached))
	for _, d := range attached {
		attachedSet[d] = true
	}
	free := make([]string, 0, len(trusted))
	for _, d := range trusted {
		if !attachedSet[d] {
			free = append(free, d)
		}
	}
	return &Pool{Free: free, Trusted: trusted}, nil
}

// FreeDomains returns the domains available to pick when creating a new
// site: verified parked domains and panel-hosted DNS zones, minus whatever is
// already attached to a site.
func (s *Service) FreeDomains(ctx context.Context, ownerID int64) ([]string, error) {
	p, err := s.DomainPool(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return p.Free, nil
}

// trustedDomains is the full ownership-proven set: verified parked domains
// union active DNS zone names, regardless of current site attachment.
func (s *Service) trustedDomains(ctx context.Context, ownerID int64) ([]string, error) {
	verified, err := s.parked.ListVerifiedFQDNs(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	zones, err := s.parked.ListActiveZoneNames(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(verified)+len(zones))
	out := make([]string, 0, len(verified)+len(zones))
	for _, d := range append(verified, zones...) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out, nil
}

// Classify is what site creation calls, once, with the new site's id. It
// decides whether the site's chosen domain already has proven ownership, and
// keeps the parked-domain pool in sync with what sites actually use:
//
//   - an exact or subdomain match against a verified parked domain or an
//     active DNS zone is trusted; a subdomain match materializes no new row
//     (the parent's proof already covers it),
//   - an exact match against an existing (unattached) parked row attaches it to
//     the new site and returns its real status as-is,
//   - anything else creates a new, unverified parked row already attached to
//     the site, so it shows up ready to verify — the site is created either
//     way; this only decides whether a DNS warning is warranted.
func (s *Service) Classify(ctx context.Context, ownerID, siteID int64, fqdn string) (string, error) {
	fqdn = normalizeFQDN(fqdn)
	if s.parked == nil {
		return "", nil
	}
	trusted, err := s.trustedDomains(ctx, ownerID)
	if err != nil {
		return "", err
	}
	for _, d := range trusted {
		if isSameOrSubdomain(fqdn, d) {
			// An exact match against a parked (not zone) domain still needs
			// attaching so it leaves the free pool and DeleteParked protects it.
			// Ownership-checked: FQDNs are globally unique, but trust here comes
			// from THIS owner's own verified/zone evidence — a same-named row that
			// happens to belong to a different owner must never be attached to
			// (or have its status reported for) someone else's site.
			if existing, lerr := s.lookupParked(ctx, fqdn); lerr == nil && existing != nil &&
				existing.OwnerID == ownerID && existing.SiteID == 0 {
				if err := s.parked.AttachParked(ctx, existing.UID, siteID); err != nil {
					return "", err
				}
			}
			return ParkedVerified, nil
		}
	}
	existing, err := s.lookupParked(ctx, fqdn)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.OwnerID == ownerID {
		if existing.SiteID == 0 {
			if err := s.parked.AttachParked(ctx, existing.UID, siteID); err != nil {
				return "", err
			}
		}
		return existing.Status, nil
	}
	token, err := newChallengeToken()
	if err != nil {
		return "", errx.Internal(err)
	}
	row := &ParkedRow{OwnerID: ownerID, FQDN: fqdn, Status: ParkedUnverified, ChallengeToken: token, SiteID: siteID}
	if err := s.parked.InsertParked(ctx, row); err != nil {
		// Best-effort: the site itself is already created and must not fail
		// because its DNS bookkeeping row could not be written.
		return ParkedUnverified, nil
	}
	return ParkedUnverified, nil
}

// isSameOrSubdomain reports whether fqdn is domain itself or a subdomain of it.
func isSameOrSubdomain(fqdn, domain string) bool {
	return fqdn == domain || strings.HasSuffix(fqdn, "."+domain)
}

// normalizeFQDN lowercases and trims a domain the same way validateAdd does.
func normalizeFQDN(fqdn string) string { return NormalizeFQDN(fqdn) }

func parkedView(r *ParkedRow) *ParkedDomain {
	return &ParkedDomain{
		UID: r.UID, FQDN: r.FQDN, Status: r.Status,
		ChallengeName: challengeName(r.FQDN), ChallengeValue: challengeValue(r.ChallengeToken),
		WildcardHint: wildcardHint(r.FQDN), Attached: r.SiteID != 0,
		VerifiedAt: r.VerifiedAt, CreatedAt: r.CreatedAt,
	}
}
