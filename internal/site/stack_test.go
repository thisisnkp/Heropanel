package site_test

import (
	"context"
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/internal/site"
)

// The site view names the stack, so no client has to infer it.
//
// The proxy cases are the point. "proxy" is how the vhost is built, and Node,
// Python and a Docker-backed app all share it — a client holding only Type would
// have to guess, and would be wrong for two of the three.
func TestSiteViewNamesTheStack(t *testing.T) {
	cases := []struct {
		name  string
		typ   site.Type
		lang  string // what the runtime reports; "" means no runtime configured
		stack string
	}{
		{"static", site.TypeStatic, "", "static"},
		{"php", site.TypePHP, "", "php"},
		{"node app", site.TypeProxy, "node", "node"},
		{"python app", site.TypeProxy, "python", "python"},
		// A proxy site whose runtime is generic, or which has none configured at
		// all, is an app — and "app" is the honest answer. Reporting "node" here
		// would put a JS badge on a site that may be neither.
		{"generic runtime", site.TypeProxy, "generic", "app"},
		{"no runtime yet", site.TypeProxy, "", "app"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newStore(t)
			rt := &fakeRuntime{port: 3000, lang: c.lang}
			svc := site.NewService(site.Deps{
				Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}, Runtime: rt,
			})

			in := validInput()
			in.Type = c.typ
			created, err := svc.Create(context.Background(), in)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if created.Stack != c.stack {
				t.Errorf("create → stack = %q, want %q", created.Stack, c.stack)
			}

			// The list is what the websites screen reads, so it has to carry the
			// stack too — not just the single-site read.
			got, err := svc.Get(context.Background(), created.UID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Stack != c.stack {
				t.Errorf("get → stack = %q, want %q", got.Stack, c.stack)
			}

			list, err := svc.List(context.Background(), 0, 50, 0)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(list) != 1 || list[0].Stack != c.stack {
				t.Errorf("list → stack = %+v, want %q", list, c.stack)
			}
		})
	}
}

// With no runtime module wired at all, a proxy site still reports something
// true rather than an empty string the UI would have to special-case.
func TestProxyStackWithoutARuntimeModule(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})

	in := validInput()
	in.Type = site.TypeProxy
	created, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Stack != "app" {
		t.Errorf("stack = %q, want app", created.Stack)
	}
}

// The stack the operator picked decides the vhost shape, so a client never has
// to know that Node and Python are both "proxy" underneath.
func TestCreateAcceptsAStack(t *testing.T) {
	for _, c := range []struct {
		stack string
		typ   site.Type
	}{
		{"static", site.TypeStatic},
		{"php", site.TypePHP},
		{"node", site.TypeProxy},
		{"python", site.TypeProxy},
	} {
		t.Run(c.stack, func(t *testing.T) {
			store, _ := newStore(t)
			svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})

			in := validInput()
			in.Type = ""
			in.Stack = c.stack
			created, err := svc.Create(context.Background(), in)
			if err != nil {
				t.Fatalf("create %q: %v", c.stack, err)
			}
			if created.Type != c.typ {
				t.Errorf("stack %q → type %q, want %q", c.stack, created.Type, c.typ)
			}
		})
	}
}

// WordPress is refused rather than quietly created as a PHP site.
//
// Accepting it would hand back a site the panel badges "WordPress" with no
// WordPress installed on it, and no way for the operator to tell the difference
// until they visited it. The error says what to do instead.
func TestCreateRefusesWordPressUntilTheModuleExists(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})

	in := validInput()
	in.Type = ""
	in.Stack = "wp"
	_, err := svc.Create(context.Background(), in)
	if err == nil {
		t.Fatal("a WordPress site was created; nothing can install WordPress yet")
	}
	if !strings.Contains(err.Error(), "WordPress") {
		t.Errorf("the error should say what was refused, got %v", err)
	}

	list, _ := svc.List(context.Background(), 0, 50, 0)
	if len(list) != 0 {
		t.Errorf("a refused create left %d site(s) behind: %+v", len(list), list)
	}
}

func TestCreateRefusesAnUnknownStack(t *testing.T) {
	store, _ := newStore(t)
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})

	in := validInput()
	in.Type = ""
	in.Stack = "ruby"
	if _, err := svc.Create(context.Background(), in); err == nil {
		t.Fatal("an unknown stack was accepted")
	}
}

// Deleting a site on a panel with no broker succeeds instead of reporting a
// failure for something that happened.
//
// The row is soft-deleted before de-provisioning starts, so an error raised
// afterwards tells the operator nothing was done about a site that has in fact
// gone — the worst of both answers. With no broker there is nothing to
// de-provision in the first place: no vhost was applied, no Linux user created,
// no slice allocated.
func TestDeleteWithoutABrokerSucceeds(t *testing.T) {
	store, _ := newStore(t)
	// Created with a broker, since creation genuinely needs one...
	svc := site.NewService(site.Deps{Repo: store, Broker: &mockGateway{}, Web: &fakeApplier{}})
	created, err := svc.Create(context.Background(), validInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// ...then deleted by a service that has none, which is the panel an
	// operator has when the broker is down or was never installed.
	brokerless := site.NewService(site.Deps{Repo: store})
	if err := brokerless.Delete(context.Background(), created.UID); err != nil {
		t.Fatalf("delete without a broker: %v", err)
	}

	list, err := brokerless.List(context.Background(), 0, 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("the site is still listed after a successful delete: %+v", list)
	}
}
