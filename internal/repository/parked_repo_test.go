package repository_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/domain"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// seedOwner inserts a minimal user and returns its id, for parked-domain
// owner-scoping tests.
func seedOwner(t *testing.T, db *repository.DB, uid string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO users (uid, username, email, password_hash, status) VALUES (?, ?, ?, '', 'active')`,
		uid, uid, uid+"@example.test")
	if err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedSiteForOwner inserts a minimal site owned by ownerID and returns its id.
func seedSiteForOwner(t *testing.T, db *repository.DB, ownerID int64, uid, primaryDomain string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO sites (uid, owner_id, name, primary_domain) VALUES (?, ?, ?, ?)`,
		uid, ownerID, uid, primaryDomain)
	if err != nil {
		t.Fatalf("seed site: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedExtraDomain attaches an extra (alias) domain row to a site.
func seedExtraDomain(t *testing.T, db *repository.DB, siteID int64, uid, fqdn string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO domains (uid, site_id, fqdn, kind) VALUES (?, ?, ?, 'alias')`,
		uid, siteID, fqdn); err != nil {
		t.Fatalf("seed extra domain: %v", err)
	}
}

// seedZone inserts a minimal active DNS zone owned by ownerID.
func seedZone(t *testing.T, db *repository.DB, ownerID int64, uid, name string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO dns_zones (uid, owner_id, name, primary_ns, admin_email, status)
		 VALUES (?, ?, ?, 'ns1.'||?, 'hostmaster@'||?, 'active')`,
		uid, ownerID, name, name, name); err != nil {
		t.Fatalf("seed zone: %v", err)
	}
}

func TestParkedDomainStoreCRUD(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewParkedDomainStore(db)
	ctx := context.Background()
	owner := seedOwner(t, db, "u-parked-crud")

	row := &domain.ParkedRow{OwnerID: owner, FQDN: "acme.test", Status: domain.ParkedUnverified, ChallengeToken: "tok123"}
	if err := store.InsertParked(ctx, row); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if row.UID == "" || row.ID == 0 {
		t.Fatalf("insert did not populate uid/id: %+v", row)
	}

	byUID, err := store.GetParkedByUID(ctx, row.UID)
	if err != nil {
		t.Fatalf("get by uid: %v", err)
	}
	if byUID.FQDN != "acme.test" || byUID.SiteID != 0 || byUID.VerifiedAt != "" {
		t.Fatalf("unattached/unverified row should scan as zero-value site_id/verified_at: %+v", byUID)
	}

	byFQDN, err := store.GetParkedByFQDN(ctx, "acme.test")
	if err != nil || byFQDN.UID != row.UID {
		t.Fatalf("get by fqdn = %+v, err=%v", byFQDN, err)
	}

	if err := store.SetParkedVerified(ctx, row.UID, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("set verified: %v", err)
	}
	verified, err := store.GetParkedByUID(ctx, row.UID)
	if err != nil || verified.Status != domain.ParkedVerified || verified.VerifiedAt == "" {
		t.Fatalf("after verify: %+v, err=%v", verified, err)
	}

	if err := store.AttachParked(ctx, row.UID, 42); err != nil {
		t.Fatalf("attach: %v", err)
	}
	attached, err := store.GetParkedByUID(ctx, row.UID)
	if err != nil || attached.SiteID != 42 {
		t.Fatalf("after attach: %+v, err=%v", attached, err)
	}

	rows, err := store.ListParked(ctx, owner)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %+v, err=%v", rows, err)
	}

	if err := store.DeleteParked(ctx, row.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetParkedByUID(ctx, row.UID); !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("after delete: want not_found, got %v", err)
	}
}

// A duplicate FQDN is refused with a conflict — the panel-wide uniqueness the
// service layer relies on to detect an existing parked/registered domain.
func TestParkedDomainStoreRejectsDuplicateFQDN(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewParkedDomainStore(db)
	ctx := context.Background()
	owner := seedOwner(t, db, "u-parked-dup")

	first := &domain.ParkedRow{OwnerID: owner, FQDN: "dup.test", Status: domain.ParkedUnverified, ChallengeToken: "t1"}
	if err := store.InsertParked(ctx, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	second := &domain.ParkedRow{OwnerID: owner, FQDN: "dup.test", Status: domain.ParkedUnverified, ChallengeToken: "t2"}
	if err := store.InsertParked(ctx, second); !errx.IsKind(err, errx.KindConflict) {
		t.Fatalf("duplicate fqdn: want conflict, got %v", err)
	}
}

// The three read-side queries the free-domain pool depends on: verified parked
// FQDNs, active zone names, and every domain already attached to a site
// (primary or extra) — each owner-scoped.
func TestParkedDomainStoreListQueries(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewParkedDomainStore(db)
	ctx := context.Background()
	owner := seedOwner(t, db, "u-parked-lists")
	other := seedOwner(t, db, "u-parked-lists-other")

	verified := &domain.ParkedRow{OwnerID: owner, FQDN: "verified.test", Status: domain.ParkedVerified, ChallengeToken: "t"}
	if err := store.InsertParked(ctx, verified); err != nil {
		t.Fatalf("insert verified: %v", err)
	}
	unverified := &domain.ParkedRow{OwnerID: owner, FQDN: "unverified.test", Status: domain.ParkedUnverified, ChallengeToken: "t"}
	if err := store.InsertParked(ctx, unverified); err != nil {
		t.Fatalf("insert unverified: %v", err)
	}
	// A different owner's verified domain must not leak into this owner's list.
	otherVerified := &domain.ParkedRow{OwnerID: other, FQDN: "other-verified.test", Status: domain.ParkedVerified, ChallengeToken: "t"}
	if err := store.InsertParked(ctx, otherVerified); err != nil {
		t.Fatalf("insert other's verified: %v", err)
	}

	seedZone(t, db, owner, "zone-1", "zoned.test")
	seedZone(t, db, other, "zone-2", "other-zoned.test")

	siteID := seedSiteForOwner(t, db, owner, "site-1", "primary.test")
	seedExtraDomain(t, db, siteID, "extra-1", "alias.test")

	verifiedFQDNs, err := store.ListVerifiedFQDNs(ctx, owner)
	if err != nil {
		t.Fatalf("list verified: %v", err)
	}
	if len(verifiedFQDNs) != 1 || verifiedFQDNs[0] != "verified.test" {
		t.Fatalf("verified fqdns = %v, want exactly [verified.test]", verifiedFQDNs)
	}

	zones, err := store.ListActiveZoneNames(ctx, owner)
	if err != nil {
		t.Fatalf("list zones: %v", err)
	}
	if len(zones) != 1 || zones[0] != "zoned.test" {
		t.Fatalf("zones = %v, want exactly [zoned.test]", zones)
	}

	attached, err := store.ListAttachedFQDNs(ctx, owner)
	if err != nil {
		t.Fatalf("list attached: %v", err)
	}
	attachedSet := map[string]bool{}
	for _, d := range attached {
		attachedSet[d] = true
	}
	if !attachedSet["primary.test"] || !attachedSet["alias.test"] {
		t.Fatalf("attached = %v, want primary.test and alias.test", attached)
	}
	if attachedSet["other-verified.test"] || attachedSet["zoned.test"] {
		t.Fatalf("attached leaked unrelated domains: %v", attached)
	}
}
