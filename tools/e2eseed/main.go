//go:build ignore

// e2eseed writes site rows straight into a panel datastore, so the browser
// suite has something to browse.
//
// Why this exists rather than the suite calling POST /sites: creating a site
// provisions a dedicated Linux user, a directory tree, an FPM pool and a vhost,
// all through the root broker. The browser suite deliberately runs npd with no
// broker (see web/playwright.config.ts) because those effects need a real Linux
// host, and they are covered by deploy/docker/e2e against actual software. So
// npd correctly refuses to create a site, and the screens under /sites/{uid}
// have nothing to render.
//
// The rows are inserted through the real repository, not hand-written SQL, so
// this cannot drift from the schema: a migration that changes the sites table
// breaks the build here rather than producing rows npd cannot read.
//
// What it deliberately does NOT do is fake provisioning state. Each seeded site
// is "active" with a plausible document root and system user because that is
// what the UI reads — but nothing on the host exists, and any test that asserts
// on a real file, a real process or a real vhost belongs in the container suite.
//
// Run with: go run tools/e2eseed/main.go <sqlite-path>
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"

	"github.com/thisisnkp/nexpanel/internal/config"
	"github.com/thisisnkp/nexpanel/internal/repository"
	"github.com/thisisnkp/nexpanel/internal/runtime"
	"github.com/thisisnkp/nexpanel/internal/site"
)

// seed is one site to create, in the shape the suite's assertions expect: a
// WordPress-ish PHP site, two app sites on different runtimes, and a static
// one — so the stack badge, the per-stack navigation and the runtime screens
// each have a subject.
type seed struct {
	// uid is fixed rather than generated, so the browser specs can address a
	// site by a name a reader recognizes. Site routes take the uid, and a
	// generated ULID would force every spec to look one up before it could
	// navigate — turning a one-line goto into a fixture.
	uid     string
	name    string
	domain  string
	typ     site.Type
	deploy  site.DeployMode
	runtime string // node | python, for proxy sites
}

var seeds = []seed{
	{uid: "e2e-php", name: "novaretail.in", domain: "novaretail.in", typ: site.TypePHP, deploy: site.DeployBaremetal},
	{uid: "e2e-node", name: "api.novaretail.in", domain: "api.novaretail.in", typ: site.TypeProxy, deploy: site.DeployGit, runtime: "node"},
	{uid: "e2e-php2", name: "billing-portal.co", domain: "billing-portal.co", typ: site.TypePHP, deploy: site.DeployBaremetal},
	{uid: "e2e-static", name: "brightlabs.dev", domain: "brightlabs.dev", typ: site.TypeStatic, deploy: site.DeployBaremetal},
	{uid: "e2e-python", name: "queue.novaretail.in", domain: "queue.novaretail.in", typ: site.TypeProxy, deploy: site.DeployGit, runtime: "python"},
	// Exists to be destroyed. Deleting a site is real server state and the suite
	// runs sequentially against one datastore, so a delete test aimed at any of
	// the sites above would pull the ground out from under every spec that runs
	// after it.
	{uid: "e2e-doomed", name: "delete-me.example", domain: "delete-me.example", typ: site.TypeStatic, deploy: site.DeployBaremetal},
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: e2eseed <sqlite-path>")
		os.Exit(2)
	}
	ctx := context.Background()

	db, err := repository.Open(config.Database{Driver: "sqlite", DSN: os.Args[1]})
	if err != nil {
		fail("open datastore", err)
	}
	defer func() { _ = db.Close() }()

	// The suite's setup project bootstraps the administrator through the UI,
	// which also runs the migrations — but this runs first, so it must not
	// depend on that having happened.
	if _, err := repository.Migrate(ctx, db); err != nil {
		fail("migrate", err)
	}

	// Sites are owned by the first administrator. The suite bootstraps them
	// after this runs, so owner 1 is the account that will exist.
	const ownerID = 1

	sites := repository.NewSiteStore(db)
	runtimes := repository.NewRuntimeStore(db)

	for _, s := range seeds {
		rec := &site.Record{
			UID:           s.uid,
			OwnerID:       ownerID,
			Name:          s.name,
			PrimaryDomain: s.domain,
			Type:          string(s.typ),
			DeployMode:    string(s.deploy),
			Status:        string(site.StatusActive),
			Webserver:     "openlitespeed",
		}
		if err := sites.Insert(ctx, rec); err != nil {
			fail("insert "+s.domain, err)
		}

		id := strconv.FormatInt(rec.ID, 10)
		if err := sites.Provision(ctx, site.ProvisionData{
			SiteID:        rec.ID,
			DocumentRoot:  "/srv/nexpanel/sites/" + id + "/public",
			LinuxUser:     "nps" + id,
			LinuxUID:      20000 + int(rec.ID),
			HomeDir:       "/srv/nexpanel/sites/" + id,
			Shell:         "/usr/sbin/nologin",
			PrimaryDomain: s.domain,
		}); err != nil {
			fail("provision "+s.domain, err)
		}

		// A proxy site with no runtime record reports stack "app". The suite
		// needs Node and Python to be distinguishable, which is the whole point
		// of the stack field, so the record has to exist.
		if s.runtime != "" {
			if err := runtimes.Upsert(ctx, &runtime.Record{
				SiteID:  rec.ID,
				Runtime: s.runtime,
				Command: "server",
				Port:    3000 + int(rec.ID),
			}); err != nil {
				fail("runtime "+s.domain, err)
			}
		}

		fmt.Printf("seeded %s (%s)\n", s.domain, rec.UID)
	}
}

func fail(what string, err error) {
	if err == sql.ErrNoRows {
		err = fmt.Errorf("no rows")
	}
	fmt.Fprintf(os.Stderr, "e2eseed: %s: %v\n", what, err)
	os.Exit(1)
}
