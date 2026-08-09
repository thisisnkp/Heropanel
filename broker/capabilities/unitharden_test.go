package capabilities

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/pkg/unitharden"
)

// A site's app unit and its cron unit run the same thing — code the operator did
// not write, as the site user. They drifted apart once already, which is why the
// directives moved into a shared profile; these tests keep them together.

func TestAppUnitCarriesTheSiteWorkloadProfile(t *testing.T) {
	unit := renderAppUnit(appUnitApplyInput{
		Vhost: "nps1", Username: "nps1", Home: "/srv/nexpanel/sites/1",
		Command: "node server.js", Port: 3000,
	}, "/srv/nexpanel/sites/1", "/srv/nexpanel/sites/1/.nexpanel-run")

	assertProfile(t, "app", unit, unitharden.SiteWorkload)
	if !strings.Contains(unit, "ReadWritePaths=/srv/nexpanel/sites/1") {
		t.Error("app unit lost its per-site ReadWritePaths")
	}
}

func TestCronUnitCarriesTheSiteWorkloadProfile(t *testing.T) {
	unit := renderCronService(cronApplyInput{
		UID: "01J0000000000000000000000A", Vhost: "nps1", Username: "nps1",
		Home: "/srv/nexpanel/sites/1", Command: "php artisan schedule:run", Schedule: "daily",
	}, "/srv/nexpanel/sites/1", "/srv/nexpanel/sites/1/.np-cron-x")

	assertProfile(t, "cron", unit, unitharden.SiteWorkload)
}

// The two units must stay identical in posture. Comparing the rendered sets
// catches a directive added to one and forgotten on the other, which is exactly
// how the scheduler would become the soft way in.
func TestAppAndCronUnitsHaveTheSamePosture(t *testing.T) {
	app := renderAppUnit(appUnitApplyInput{
		Vhost: "nps1", Username: "nps1", Home: "/srv/nexpanel/sites/1",
		Command: "node server.js", Port: 3000,
	}, "/srv/nexpanel/sites/1", "/srv/nexpanel/sites/1/.nexpanel-run")
	cron := renderCronService(cronApplyInput{
		UID: "01J0000000000000000000000A", Vhost: "nps1", Username: "nps1",
		Home: "/srv/nexpanel/sites/1", Command: "true", Schedule: "daily",
	}, "/srv/nexpanel/sites/1", "/srv/nexpanel/sites/1/.np-cron-x")

	for _, want := range []string{
		"User=nps1", "Group=nps1", "PrivateTmp=true", "ProtectSystem=strict",
		"ProtectHome=true", "UMask=0027", "Slice=",
	} {
		if !strings.Contains(app, want) {
			t.Errorf("app unit missing %q", want)
		}
		if !strings.Contains(cron, want) {
			t.Errorf("cron unit missing %q", want)
		}
	}
}

func assertProfile(t *testing.T, name, unit string, p unitharden.Profile) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(p.Directives(), "\n"), "\n") {
		if !strings.Contains(unit, line) {
			t.Errorf("%s unit is missing %q", name, line)
		}
	}
}
