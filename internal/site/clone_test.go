package site_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/site"
)

func cloneInputFor(srcUID string) site.CloneInput {
	return site.CloneInput{
		SourceUID:     srcUID,
		Name:          "Acme Staging",
		PrimaryDomain: "staging.example.com",
		OwnerID:       1,
	}
}

func callsTo(gw *mockGateway, capability string) []gwCall {
	var out []gwCall
	for _, c := range gw.calls {
		if c.capability == capability {
			out = append(out, c)
		}
	}
	return out
}

func TestCloneProvisionsASeparateSiteAndCopiesTheContent(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})

	src, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	clone, err := svc.Clone(context.Background(), cloneInputFor(src.UID))
	if err != nil {
		t.Fatalf("clone: %v", err)
	}

	// A clone is a site in its own right: its own user, its own tree, its own
	// domain. Sharing any of those would not be a copy, it would be an alias.
	if clone.UID == src.UID {
		t.Fatal("the clone reused the source's uid")
	}
	if clone.SystemUser == src.SystemUser {
		t.Errorf("the clone shares the source's Linux user %q — the two sites could read each other's files", clone.SystemUser)
	}
	if clone.DocumentRoot == src.DocumentRoot {
		t.Errorf("the clone shares the source's document root %q", clone.DocumentRoot)
	}
	if clone.PrimaryDomain != "staging.example.com" {
		t.Errorf("primary domain = %q, want the clone's own", clone.PrimaryDomain)
	}
	if clone.Status != site.StatusActive {
		t.Errorf("status = %q, want active", clone.Status)
	}

	copies := callsTo(gw, "site.copy_tree")
	if len(copies) != 1 {
		t.Fatalf("site.copy_tree called %d times, want 1", len(copies))
	}
	in := copies[0].input
	if in["src_root"] != "/srv/heropanel/sites/1" {
		t.Errorf("src_root = %v, want the source's home", in["src_root"])
	}
	if in["dst_root"] != "/srv/heropanel/sites/2" {
		t.Errorf("dst_root = %v, want the clone's home", in["dst_root"])
	}
	// The copy must be re-owned to the clone's user, not the source's.
	if in["username"] != clone.SystemUser {
		t.Errorf("username = %v, want the clone's user %q — otherwise the tree stays owned by the source",
			in["username"], clone.SystemUser)
	}
}

func TestCloneInheritsTheSourceType(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})

	in := validInput()
	in.Type = site.TypePHP
	src, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	clone, err := svc.Clone(context.Background(), cloneInputFor(src.UID))
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if clone.Type != site.TypePHP {
		t.Errorf("type = %q, want the source's %q", clone.Type, site.TypePHP)
	}
}

// An empty clone left in the list is worse than no clone: the operator sees a
// site, assumes it holds a copy, and finds out later that it does not.
func TestCloneRemovesTheNewSiteIfTheCopyFails(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{failOn: "site.copy_tree"}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})

	src, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if _, err := svc.Clone(context.Background(), cloneInputFor(src.UID)); err == nil {
		t.Fatal("clone reported success despite the copy failing")
	}

	list, err := svc.List(context.Background(), 0, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d sites, want only the source — the half-made clone was left behind", len(list))
	}
	if list[0].UID != src.UID {
		t.Errorf("the surviving site is %q, want the source %q", list[0].UID, src.UID)
	}
}

func TestCloneRejectsAMissingSource(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})

	if _, err := svc.Clone(context.Background(), cloneInputFor("site_does_not_exist")); err == nil {
		t.Fatal("clone accepted a source that does not exist")
	}
}

func TestCloneValidatesTheNewSiteInput(t *testing.T) {
	store, _ := newStore(t)
	gw := &mockGateway{}
	svc := site.NewService(site.Deps{Repo: store, Broker: gw, Web: &fakeApplier{}})

	src, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	in := cloneInputFor(src.UID)
	in.PrimaryDomain = "not a domain"

	if _, err := svc.Clone(context.Background(), in); err == nil {
		t.Fatal("clone accepted an invalid primary domain")
	}
	// Nothing may have been provisioned for a request that was never valid.
	if n := len(callsTo(gw, "site.copy_tree")); n != 0 {
		t.Errorf("site.copy_tree was called %d times for an invalid clone", n)
	}
}
