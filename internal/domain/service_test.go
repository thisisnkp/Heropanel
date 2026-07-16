package domain_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/domain"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

type fakeSites struct{}

func (fakeSites) Resolve(_ context.Context, uid string) (*domain.SiteRef, error) {
	return &domain.SiteRef{ID: 1, UID: uid}, nil
}

type fakeRepo struct {
	rows []*domain.Row
	seq  int64
}

func newRepo() *fakeRepo {
	return &fakeRepo{rows: []*domain.Row{
		{ID: 1, UID: "d-primary", SiteID: 1, FQDN: "acme.test", Kind: domain.KindPrimary},
	}, seq: 1}
}

func (r *fakeRepo) Insert(_ context.Context, row *domain.Row) error {
	r.seq++
	row.ID = r.seq
	if row.UID == "" {
		row.UID = "d-" + row.FQDN
	}
	cp := *row
	r.rows = append(r.rows, &cp)
	return nil
}
func (r *fakeRepo) ListBySiteID(_ context.Context, siteID int64) ([]domain.Row, error) {
	var out []domain.Row
	for _, x := range r.rows {
		if x.SiteID == siteID {
			out = append(out, *x)
		}
	}
	return out, nil
}
func (r *fakeRepo) GetByUID(_ context.Context, uid string) (*domain.Row, error) {
	for _, x := range r.rows {
		if x.UID == uid {
			cp := *x
			return &cp, nil
		}
	}
	return nil, errx.NotFound("domain_not_found", "no domain")
}
func (r *fakeRepo) Delete(_ context.Context, uid string) error {
	for i, x := range r.rows {
		if x.UID == uid {
			r.rows = append(r.rows[:i], r.rows[i+1:]...)
			break
		}
	}
	return nil
}
func (r *fakeRepo) SetForceHTTPSForSite(_ context.Context, siteID int64, on bool) error {
	for _, x := range r.rows {
		if x.SiteID == siteID {
			x.ForceHTTPS = on
		}
	}
	return nil
}

func newSvc(t *testing.T) (*domain.Service, *fakeRepo, *int) {
	t.Helper()
	repo := newRepo()
	applies := 0
	svc := domain.NewService(repo, fakeSites{}).
		WithReapply(func(context.Context) error { applies++; return nil })
	return svc, repo, &applies
}

func TestAddAliasReappliesWebserver(t *testing.T) {
	svc, _, applies := newSvc(t)
	d, err := svc.Add(context.Background(), "s", domain.AddInput{FQDN: "www.acme.test"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if d.Kind != domain.KindAlias || d.FQDN != "www.acme.test" {
		t.Fatalf("domain = %+v", d)
	}
	if *applies != 1 {
		t.Fatalf("expected the vhost to be re-applied once, got %d", *applies)
	}
}

func TestAddRedirectRequiresAbsoluteURL(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()

	// A redirect without a valid absolute target is rejected.
	for _, bad := range []string{"", "new.acme.test", "ftp://x.test"} {
		_, err := svc.Add(ctx, "s", domain.AddInput{FQDN: "old.acme.test", Kind: domain.KindRedirect, RedirectTo: bad})
		if !errx.IsKind(err, errx.KindValidation) {
			t.Fatalf("redirect_to %q should be rejected, got %v", bad, err)
		}
	}
	// A valid one defaults to 301.
	d, err := svc.Add(ctx, "s", domain.AddInput{
		FQDN: "old.acme.test", Kind: domain.KindRedirect, RedirectTo: "https://new.acme.test",
	})
	if err != nil {
		t.Fatalf("add redirect: %v", err)
	}
	if d.RedirectCode != 301 || d.RedirectTo != "https://new.acme.test" {
		t.Fatalf("redirect = %+v", d)
	}
	// An unsupported status is rejected.
	if _, err := svc.Add(ctx, "s", domain.AddInput{
		FQDN: "x.acme.test", Kind: domain.KindRedirect, RedirectTo: "https://n.test", RedirectCode: 418,
	}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("bad redirect code should be rejected, got %v", err)
	}
}

func TestAddValidatesFQDNAndKind(t *testing.T) {
	svc, _, _ := newSvc(t)
	ctx := context.Background()
	if _, err := svc.Add(ctx, "s", domain.AddInput{FQDN: "not a domain"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("want validation for bad fqdn, got %v", err)
	}
	if _, err := svc.Add(ctx, "s", domain.AddInput{FQDN: "a.test", Kind: "primary"}); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("kind=primary must not be addable, got %v", err)
	}
}

func TestDeletePrimaryIsRefused(t *testing.T) {
	svc, _, _ := newSvc(t)
	if err := svc.Delete(context.Background(), "d-primary"); !errx.IsKind(err, errx.KindValidation) {
		t.Fatalf("deleting the primary domain must be refused, got %v", err)
	}
}

func TestDeleteAliasReapplies(t *testing.T) {
	svc, _, applies := newSvc(t)
	ctx := context.Background()
	d, err := svc.Add(ctx, "s", domain.AddInput{FQDN: "www.acme.test"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.Delete(ctx, d.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if *applies != 2 { // once for add, once for delete
		t.Fatalf("expected 2 re-applies, got %d", *applies)
	}
	list, _ := svc.List(ctx, "s")
	if len(list) != 1 || list[0].Kind != domain.KindPrimary {
		t.Fatalf("only the primary should remain: %+v", list)
	}
}

func TestSetForceHTTPS(t *testing.T) {
	svc, repo, applies := newSvc(t)
	if err := svc.SetForceHTTPS(context.Background(), "s", true); err != nil {
		t.Fatalf("force https: %v", err)
	}
	if !repo.rows[0].ForceHTTPS {
		t.Fatal("force_https should be on")
	}
	if *applies != 1 {
		t.Fatalf("expected a re-apply, got %d", *applies)
	}
}
