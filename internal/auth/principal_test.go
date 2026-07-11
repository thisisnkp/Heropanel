package auth_test

import (
	"context"
	"testing"

	"github.com/thisisnkp/heropanel/internal/auth"
)

func TestPrincipalCan(t *testing.T) {
	exact := &auth.Principal{Permissions: []string{"site.read", "dns.read"}}
	if !exact.Can("site.read") {
		t.Fatal("should hold site.read")
	}
	if exact.Can("site.write") {
		t.Fatal("should not hold site.write")
	}

	super := &auth.Principal{Permissions: []string{"*"}}
	if !super.Can("anything.at.all") {
		t.Fatal("wildcard should grant any permission")
	}

	none := &auth.Principal{}
	if none.Can("site.read") {
		t.Fatal("empty principal grants nothing")
	}
}

func TestPrincipalContextRoundTrip(t *testing.T) {
	if _, ok := auth.FromContext(context.Background()); ok {
		t.Fatal("empty context should have no principal")
	}
	p := &auth.Principal{UserID: 7, Email: "a@b.c"}
	ctx := auth.WithPrincipal(context.Background(), p)
	got, ok := auth.FromContext(ctx)
	if !ok || got.UserID != 7 {
		t.Fatalf("round trip failed: %+v ok=%v", got, ok)
	}
}
