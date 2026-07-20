package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/internal/terminal"
)

// seedSite inserts a minimal site row and returns its id, so a recording has a
// real site to join against.
func seedSite(t *testing.T, db *repository.DB, uid, name, domain string) int64 {
	t.Helper()
	ctx := context.Background()
	res, err := db.ExecContext(ctx,
		`INSERT INTO users (uid, username, email, password_hash, status) VALUES (?, ?, ?, '', 'active')`,
		"u-"+uid, uid, uid+"@example.test")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ownerID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx,
		`INSERT INTO sites (uid, owner_id, name, primary_domain) VALUES (?, ?, ?, ?)`,
		uid, ownerID, name, domain)
	if err != nil {
		t.Fatalf("seed site: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func newRecording(siteID int64, actor string) *terminal.Recording {
	now := time.Now()
	return &terminal.Recording{
		SiteID:     siteID,
		ActorEmail: actor,
		ActorIP:    "10.0.0.1",
		SystemUser: "hps1",
		Path:       "2026-07-21/x.cast",
		StartedAt:  terminal.FormatTime(now),
		ExpiresAt:  terminal.FormatTime(now.Add(terminal.DefaultRetention)),
	}
}

// A cross-site listing is the view an auditor actually reads, and it is
// unusable without the site each session ran on. site_id is deliberately not
// exposed, so the site's uid and name have to be joined in — and a join that
// silently yields empty strings looks exactly like a working query.
func TestRecordingListCarriesTheSiteItRanOn(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	store := repository.NewRecordingStore(db)

	siteA := seedSite(t, db, "site-a", "Alpha", "alpha.test")
	siteB := seedSite(t, db, "site-b", "Bravo", "bravo.test")

	for _, r := range []*terminal.Recording{
		newRecording(siteA, "a@example.test"),
		newRecording(siteB, "b@example.test"),
	} {
		if err := store.Create(ctx, r); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	all, err := store.List(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("listed %d recordings across all sites, want 2", len(all))
	}
	for _, rec := range all {
		if rec.SiteUID == "" || rec.SiteName == "" {
			t.Errorf("recording %s came back with no site (uid %q, name %q) — the cross-site list cannot say where the session ran",
				rec.UID, rec.SiteUID, rec.SiteName)
		}
	}

	// Newest first, and scoping still works with the join in place.
	scoped, err := store.List(ctx, siteB, 0, 0)
	if err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	if len(scoped) != 1 {
		t.Fatalf("scoped list returned %d, want 1", len(scoped))
	}
	if scoped[0].SiteUID != "site-b" || scoped[0].SiteName != "Bravo" {
		t.Errorf("scoped to site B, got site uid %q name %q", scoped[0].SiteUID, scoped[0].SiteName)
	}

	// Get carries it too: the player's header names the site.
	one, err := store.Get(ctx, scoped[0].UID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if one.SiteUID != "site-b" {
		t.Errorf("Get returned site uid %q, want site-b", one.SiteUID)
	}
}

// A site can be deleted while its recordings are still inside the retention
// window. The transcript of what someone did on a site that no longer exists is
// the *last* thing an audit trail should drop, so the listing must survive the
// missing join row rather than error the whole page.
func TestRecordingListSurvivesADeletedSite(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	store := repository.NewRecordingStore(db)

	siteID := seedSite(t, db, "site-gone", "Gone", "gone.test")
	rec := newRecording(siteID, "a@example.test")
	if err := store.Create(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, siteID); err != nil {
		t.Fatalf("delete site: %v", err)
	}

	out, err := store.List(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("list after the site was deleted: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("listed %d recordings, want the 1 whose site is gone", len(out))
	}
	if out[0].SiteUID != "" {
		t.Errorf("site uid %q for a deleted site, want empty", out[0].SiteUID)
	}
}
