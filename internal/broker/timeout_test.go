package broker_test

import (
	"testing"
	"time"

	client "github.com/thisisnkp/heropanel/internal/broker"
)

// The client's timeout is only a backstop against a broker that never answers.
// The broker is what actually bounds each command it runs, so for anything
// legitimately slow the client must wait *longer* than the broker's own budget —
// otherwise it hangs up on work that is still running and the operation fails
// for no reason.
//
// A blanket 30s here silently made every real deploy (npm install + next build)
// and every non-trivial database export impossible; it went unnoticed because
// every test used a fake runner and every e2e built something tiny.
func TestLongRunningCapabilitiesOutlastTheBrokersOwnBudget(t *testing.T) {
	// Lower bounds derived from the broker-side timeouts in
	// broker/capabilities: git.deploy = clone 5m + composer 15m + build 15m;
	// db.export = mysqldump 60m + gzip 30m; db.import = gunzip 30m + load 60m.
	minimums := map[string]time.Duration{
		"git.deploy": 35 * time.Minute,
		"db.export":  90 * time.Minute,
		"db.import":  90 * time.Minute,
	}
	for capability, floor := range minimums {
		got := client.TimeoutFor(capability)
		if got < floor {
			t.Fatalf("%s: client waits %s but the broker may legitimately take %s — "+
				"the client would abort work that is still running",
				capability, got, floor)
		}
	}
}

// Everything else is a handful of exec calls and should fail fast rather than
// hang a request.
func TestOrdinaryCapabilitiesFailFast(t *testing.T) {
	for _, capability := range []string{
		"system_user.create", "webserver.apply", "db.create", "site.apply_slice",
		"app.unit_apply", "dns.write_zone", "cert.install",
	} {
		if got := client.TimeoutFor(capability); got != client.DefaultTimeout {
			t.Fatalf("%s: timeout = %s, want the %s default", capability, got, client.DefaultTimeout)
		}
	}
	// An unknown capability gets the default too, not zero (which would mean
	// "already expired").
	if got := client.TimeoutFor("something.new"); got != client.DefaultTimeout {
		t.Fatalf("unknown capability timeout = %s, want %s", got, client.DefaultTimeout)
	}
}
