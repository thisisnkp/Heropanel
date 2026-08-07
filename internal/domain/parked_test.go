package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// fakeParkedRepo is an in-memory domain.ParkedRepo, mirroring fakeRepo's style
// above. Zones/attached are seeded directly by tests that need them.
type fakeParkedRepo struct {
	rows  []*domain.ParkedRow
	zones []string // active DNS zone names, owner-scoped by test setup
	seq   int64
}

func (r *fakeParkedRepo) InsertParked(_ context.Context, row *domain.ParkedRow) error {
	for _, x := range r.rows {
		if x.FQDN == row.FQDN {
			return errx.New(errx.KindConflict, "domain_exists", "already exists")
		}
	}
	r.seq++
	row.ID = r.seq
	if row.UID == "" {
		row.UID = "p-" + row.FQDN
	}
	cp := *row
	r.rows = append(r.rows, &cp)
	return nil
}

func (r *fakeParkedRepo) GetParkedByUID(_ context.Context, uid string) (*domain.ParkedRow, error) {
	for _, x := range r.rows {
		if x.UID == uid {
			cp := *x
			return &cp, nil
		}
	}
	return nil, errx.NotFound("domain_not_found", "no such parked domain")
}

func (r *fakeParkedRepo) GetParkedByFQDN(_ context.Context, fqdn string) (*domain.ParkedRow, error) {
	for _, x := range r.rows {
		if x.FQDN == fqdn {
			cp := *x
			return &cp, nil
		}
	}
	return nil, errx.NotFound("domain_not_found", "no such parked domain")
}

func (r *fakeParkedRepo) ListParked(_ context.Context, ownerID int64) ([]domain.ParkedRow, error) {
	var out []domain.ParkedRow
	for _, x := range r.rows {
		if x.OwnerID == ownerID {
			out = append(out, *x)
		}
	}
	return out, nil
}

func (r *fakeParkedRepo) SetParkedVerified(_ context.Context, uid, verifiedAt string) error {
	for _, x := range r.rows {
		if x.UID == uid {
			x.Status, x.VerifiedAt = domain.ParkedVerified, verifiedAt
			return nil
		}
	}
	return errx.NotFound("domain_not_found", "no such parked domain")
}

func (r *fakeParkedRepo) AttachParked(_ context.Context, uid string, siteID int64) error {
	for _, x := range r.rows {
		if x.UID == uid {
			x.SiteID = siteID
			return nil
		}
	}
	return errx.NotFound("domain_not_found", "no such parked domain")
}

func (r *fakeParkedRepo) DeleteParked(_ context.Context, uid string) error {
	for i, x := range r.rows {
		if x.UID == uid {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *fakeParkedRepo) ListVerifiedFQDNs(_ context.Context, ownerID int64) ([]string, error) {
	var out []string
	for _, x := range r.rows {
		if x.OwnerID == ownerID && x.Status == domain.ParkedVerified {
			out = append(out, x.FQDN)
		}
	}
	return out, nil
}

func (r *fakeParkedRepo) ListActiveZoneNames(_ context.Context, _ int64) ([]string, error) {
	return append([]string(nil), r.zones...), nil
}

func (r *fakeParkedRepo) ListAttachedFQDNs(_ context.Context, ownerID int64) ([]string, error) {
	var out []string
	for _, x := range r.rows {
		if x.OwnerID == ownerID && x.SiteID != 0 {
			out = append(out, x.FQDN)
		}
	}
	return out, nil
}

func newParkedSvc(t *testing.T) (*domain.Service, *fakeParkedRepo) {
	t.Helper()
	repo := &fakeParkedRepo{}
	svc := domain.NewService(newRepo(), fakeSites{}).WithParked(repo, "127.0.0.1:1") // port 1: nothing listens, LookupTXT fails fast
	return svc, repo
}

// Park validates the domain, rejects a duplicate, and returns the DNS
// challenge instructions the operator must publish.
func TestParkReturnsChallengeAndRejectsDuplicate(t *testing.T) {
	svc, _ := newParkedSvc(t)
	ctx := context.Background()

	pd, err := svc.Park(ctx, 1, "Acme.Test.")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if pd.FQDN != "acme.test" {
		t.Fatalf("fqdn not normalized: %q", pd.FQDN)
	}
	if pd.Status != domain.ParkedUnverified {
		t.Fatalf("status = %q, want unverified", pd.Status)
	}
	if !strings.HasPrefix(pd.ChallengeName, "_heropanel-challenge.") || !strings.Contains(pd.ChallengeName, "acme.test") {
		t.Fatalf("challenge name = %q", pd.ChallengeName)
	}
	if !strings.HasPrefix(pd.ChallengeValue, "heropanel-verify=") {
		t.Fatalf("challenge value = %q", pd.ChallengeValue)
	}
	if pd.Attached {
		t.Fatal("a freshly parked domain must not be attached")
	}

	if _, err := svc.Park(ctx, 1, "acme.test"); !errx.IsKind(err, errx.KindConflict) {
		t.Fatalf("duplicate park: want conflict, got %v", err)
	}
}

func TestParkValidatesFQDN(t *testing.T) {
	svc, _ := newParkedSvc(t)
	if _, err := svc.Park(context.Background(), 1, "not a domain"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation, got %v", err)
	}
}

// VerifyParked is idempotent for an already-verified domain — no DNS lookup
// needed to confirm what is already true.
func TestVerifyParkedIdempotentWhenAlreadyVerified(t *testing.T) {
	svc, repo := newParkedSvc(t)
	ctx := context.Background()
	pd, err := svc.Park(ctx, 1, "acme.test")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := repo.SetParkedVerified(ctx, pd.UID, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("seed verified: %v", err)
	}
	got, err := svc.VerifyParked(ctx, pd.UID)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Status != domain.ParkedVerified {
		t.Fatalf("status = %q, want verified", got.Status)
	}
}

// With no DNS answer (the pinned resolver points at a closed port), an
// unverified domain fails verification with a conflict, not a crash, and stays
// unverified.
func TestVerifyParkedFailsWithoutTheChallengeRecord(t *testing.T) {
	svc, repo := newParkedSvc(t)
	ctx := context.Background()
	pd, err := svc.Park(ctx, 1, "acme.test")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if _, err := svc.VerifyParked(ctx, pd.UID); !errx.IsKind(err, errx.KindConflict) {
		t.Fatalf("want conflict (challenge not found), got %v", err)
	}
	row, _ := repo.GetParkedByUID(ctx, pd.UID)
	if row.Status != domain.ParkedUnverified {
		t.Fatalf("a failed verify must not flip status, got %q", row.Status)
	}
}

func TestVerifyParkedUnknownUID(t *testing.T) {
	svc, _ := newParkedSvc(t)
	if _, err := svc.VerifyParked(context.Background(), "nope"); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

// DeleteParked is blocked while the domain is attached to a site.
func TestDeleteParkedBlockedWhileAttached(t *testing.T) {
	svc, repo := newParkedSvc(t)
	ctx := context.Background()
	pd, err := svc.Park(ctx, 1, "acme.test")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := repo.AttachParked(ctx, pd.UID, 42); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := svc.DeleteParked(ctx, pd.UID); !errx.IsKind(err, errx.KindConflict) {
		t.Fatalf("want conflict while attached, got %v", err)
	}
	// Detach, then delete succeeds.
	if err := repo.AttachParked(ctx, pd.UID, 0); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if err := svc.DeleteParked(ctx, pd.UID); err != nil {
		t.Fatalf("delete after detach: %v", err)
	}
	if _, err := repo.GetParkedByUID(ctx, pd.UID); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatal("row should be gone")
	}
}

// FreeDomains unions verified parked domains and active zones, excluding
// whatever is already attached to a site.
func TestFreeDomainsUnionsAndExcludesAttached(t *testing.T) {
	svc, repo := newParkedSvc(t)
	ctx := context.Background()

	free, err := svc.Park(ctx, 1, "free.test")
	if err != nil {
		t.Fatalf("park free: %v", err)
	}
	if err := repo.SetParkedVerified(ctx, free.UID, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("verify free: %v", err)
	}
	unverified, err := svc.Park(ctx, 1, "unverified.test")
	if err != nil {
		t.Fatalf("park unverified: %v", err)
	}
	_ = unverified
	attached, err := svc.Park(ctx, 1, "attached.test")
	if err != nil {
		t.Fatalf("park attached: %v", err)
	}
	if err := repo.SetParkedVerified(ctx, attached.UID, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("verify attached: %v", err)
	}
	if err := repo.AttachParked(ctx, attached.UID, 7); err != nil {
		t.Fatalf("attach: %v", err)
	}
	repo.zones = []string{"zone.test"}

	got, err := svc.FreeDomains(ctx, 1)
	if err != nil {
		t.Fatalf("free domains: %v", err)
	}
	want := map[string]bool{"free.test": true, "zone.test": true}
	if len(got) != len(want) {
		t.Fatalf("free domains = %v, want exactly %v", got, want)
	}
	for _, d := range got {
		if !want[d] {
			t.Errorf("unexpected free domain %q (unverified/attached leaked into the pool)", d)
		}
	}
}

// Classify: an exact match against a free verified parked domain attaches it
// and reports verified; a subdomain of a verified/zone domain is trusted
// without creating a new row; anything else is auto-parked unverified and
// already attached to the new site.
func TestClassify(t *testing.T) {
	svc, repo := newParkedSvc(t)
	ctx := context.Background()

	// Exact match: attaches the existing free parked domain.
	free, err := svc.Park(ctx, 1, "free.test")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := repo.SetParkedVerified(ctx, free.UID, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	status, err := svc.Classify(ctx, 1, 100, "free.test")
	if err != nil || status != domain.ParkedVerified {
		t.Fatalf("classify exact match: status=%q err=%v", status, err)
	}
	row, _ := repo.GetParkedByUID(ctx, free.UID)
	if row.SiteID != 100 {
		t.Fatalf("exact match should attach to the new site, got site_id=%d", row.SiteID)
	}

	// Subdomain of a zone: trusted, no new row materialized.
	repo.zones = []string{"zoned.test"}
	before := len(repo.rows)
	status, err = svc.Classify(ctx, 1, 101, "blog.zoned.test")
	if err != nil || status != domain.ParkedVerified {
		t.Fatalf("classify zone subdomain: status=%q err=%v", status, err)
	}
	if len(repo.rows) != before {
		t.Fatalf("a trusted subdomain must not materialize a parked row: %d -> %d", before, len(repo.rows))
	}

	// Anything else: auto-parked unverified, already attached to the new site.
	status, err = svc.Classify(ctx, 1, 102, "brand-new.test")
	if err != nil || status != domain.ParkedUnverified {
		t.Fatalf("classify unknown domain: status=%q err=%v", status, err)
	}
	np, err := repo.GetParkedByFQDN(ctx, "brand-new.test")
	if err != nil {
		t.Fatalf("auto-parked row missing: %v", err)
	}
	if np.SiteID != 102 || np.Status != domain.ParkedUnverified {
		t.Fatalf("auto-parked row = %+v, want site_id=102 unverified", np)
	}
}

// A trusted domain (e.g. its owner also parked/zoned it) does not leak across
// owners: Classify only trusts the caller's own owner scope.
func TestClassifyIsOwnerScoped(t *testing.T) {
	svc, repo := newParkedSvc(t)
	ctx := context.Background()
	other, err := svc.Park(ctx, 2, "someone-elses.test")
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if err := repo.SetParkedVerified(ctx, other.UID, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	status, err := svc.Classify(ctx, 1, 200, "someone-elses.test")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if status != domain.ParkedUnverified {
		t.Fatalf("another owner's verified domain must not be trusted here, got %q", status)
	}
}
