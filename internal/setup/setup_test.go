package setup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/thisisnkp/nexpanel/internal/php"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		sel  Selection
		ok   bool
	}{
		{"ols", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB}, true},
		{"litespeed enterprise", Selection{Webserver: WebserverLiteSpeed, DBEngine: DBEngineMariaDB}, true},
		// "mysql" names the same server MariaDB speaks for, so it is accepted and
		// rewritten rather than refused; an omitted engine means the only one.
		{"mysql is an alias", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: "mysql"}, true},
		{"engine may be omitted", Selection{Webserver: WebserverOpenLiteSpeed}, true},
		// Retired engines are refused on input. They are real, different servers
		// the panel can no longer configure — saying yes would be a lie.
		{"nginx retired", Selection{Webserver: "nginx", DBEngine: DBEngineMariaDB}, false},
		{"apache retired", Selection{Webserver: "apache", DBEngine: DBEngineMariaDB}, false},
		{"postgres retired", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: "postgresql"}, false},
		{"unknown webserver", Selection{Webserver: "iis", DBEngine: DBEngineMariaDB}, false},
		{"unknown engine", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: "oracle"}, false},
		{"empty", Selection{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.sel.Validate()
			if c.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("expected invalid, got nil")
			}
		})
	}
}

// hasStep reports whether the plan contains a step matching kind+target with the
// given enable flag.
func hasStep(p Plan, kind StepKind, target string, enable bool) bool {
	for _, s := range p.Steps {
		if s.Kind == kind && s.Target == target && s.Enable == enable {
			return true
		}
	}
	return false
}

func TestBuildPlan_WebserverAndDB(t *testing.T) {
	p := BuildPlan(Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB})
	if !hasStep(p, StepPackage, "openlitespeed", true) {
		t.Error("expected openlitespeed package step")
	}
	if !hasStep(p, StepService, "lsws", true) {
		t.Error("expected lsws service step")
	}
	if !hasStep(p, StepPackage, "mariadb-server", true) {
		t.Error("expected mariadb-server package step")
	}
	if !hasStep(p, StepService, "mariadb", true) {
		t.Error("expected mariadb service step")
	}
}

func TestBuildPlan_DNSAndMailOn(t *testing.T) {
	p := BuildPlan(Selection{
		Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB,
		ManageDNS: true, CreateMail: true,
	})
	if !hasStep(p, StepPackage, "bind9", true) {
		t.Error("expected bind9 package when DNS on")
	}
	if !hasStep(p, StepService, "named", true) {
		t.Error("expected named service when DNS on")
	}
	if !hasStep(p, StepModule, "dns", true) {
		t.Error("expected dns module enable step")
	}
	if !hasStep(p, StepPackage, "postfix", true) || !hasStep(p, StepPackage, "dovecot-core", true) {
		t.Error("expected postfix + dovecot packages when mail on")
	}
	if !hasStep(p, StepModule, "mail", true) {
		t.Error("expected mail module enable step")
	}
}

func TestBuildPlan_DNSAndMailOff(t *testing.T) {
	p := BuildPlan(Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB})
	// No BIND/Postfix work when both are off...
	if hasStep(p, StepPackage, "bind9", true) {
		t.Error("did not expect bind9 when DNS off")
	}
	if hasStep(p, StepPackage, "postfix", true) {
		t.Error("did not expect postfix when mail off")
	}
	// ...but the module intent is still recorded as disabled, so a re-run turns
	// them off explicitly.
	if !hasStep(p, StepModule, "dns", false) {
		t.Error("expected dns module disable step")
	}
	if !hasStep(p, StepModule, "mail", false) {
		t.Error("expected mail module disable step")
	}
}

// ── service ──────────────────────────────────────────────────────────────────

type memStore struct {
	state *State
}

func (m *memStore) Get(context.Context) (*State, error) {
	if m.state == nil {
		return &State{}, nil
	}
	cp := *m.state
	return &cp, nil
}

func (m *memStore) Save(_ context.Context, sel Selection, completedAt *time.Time) error {
	m.state = &State{Selection: sel, Completed: completedAt != nil, CompletedAt: completedAt}
	return nil
}

type recordingProv struct {
	plans []Plan
	err   error
}

func (r *recordingProv) Provision(_ context.Context, p Plan) error {
	r.plans = append(r.plans, p)
	return r.err
}

func TestService_CompleteFlow(t *testing.T) {
	store := &memStore{}
	prov := &recordingProv{}
	svc := NewService(store, prov, nil)

	st, err := svc.Status(context.Background())
	if err != nil || st.Completed {
		t.Fatalf("fresh install should be incomplete: %+v err=%v", st, err)
	}

	sel := Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB, ManageDNS: true}
	done, err := svc.Complete(context.Background(), sel)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !done.Completed || done.CompletedAt == nil {
		t.Fatal("expected completed state with timestamp")
	}
	if len(prov.plans) != 1 {
		t.Fatalf("expected provisioner called once, got %d", len(prov.plans))
	}
	after, _ := svc.Status(context.Background())
	if !after.Completed || after.Webserver != WebserverOpenLiteSpeed {
		t.Fatalf("state not persisted: %+v", after)
	}
}

func TestService_CompleteRejectsUnknown(t *testing.T) {
	svc := NewService(&memStore{}, &recordingProv{}, nil)
	_, err := svc.Complete(context.Background(), Selection{Webserver: "iis", DBEngine: DBEngineMariaDB})
	if err == nil {
		t.Fatal("expected validation error for unknown webserver")
	}
	if e, ok := err.(*errx.Error); !ok || e.Kind != errx.KindValidation {
		t.Fatalf("expected validation errx, got %v", err)
	}
}

func TestService_RecordOnlyWithoutProvisioner(t *testing.T) {
	store := &memStore{}
	svc := NewService(store, nil, nil) // no provisioner → record-only
	_, err := svc.Complete(context.Background(), Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB})
	if err != nil {
		t.Fatalf("record-only complete should succeed: %v", err)
	}
	if store.state == nil || !store.state.Completed {
		t.Fatal("expected selection recorded even without a provisioner")
	}
}

// Every baseline entry the wizard ticks must be something an install actually
// puts on the host.
//
// The wizard's list is the panel's promise about what a NexPanel server is, and
// it is made before anything has been installed — so nothing checks it later. An
// entry that is Supported but provisioned by nothing would be a tick beside a
// component that never arrives, and the operator would discover it from a site
// that will not start.
//
// lscache is the one exemption, and it is exempt because there is nothing to
// install: page caching is compiled into OpenLiteSpeed and LiteSpeed Enterprise.
func TestBaselineTicksOnlyWhatIsProvisioned(t *testing.T) {
	provisioned := map[string]bool{"lscache": true}
	for _, c := range baselineComponents {
		provisioned[c] = true
	}
	// maldet is installed by its own broker capability during setup rather than
	// through baselineComponents, because it has no distribution package; see
	// MaldetInstallCapability in broker.go.
	provisioned["maldet"] = true

	var planned []string
	for _, o := range Baseline() {
		if o.Supported && !provisioned[o.ID] {
			t.Errorf("baseline %q is ticked but nothing installs it", o.ID)
		}
		if !o.Supported {
			planned = append(planned, o.ID)
		}
		if o.Label == "" {
			t.Errorf("baseline %q has no label", o.ID)
		}
	}

	// The runtimes are listed as planned on purpose: the default stack runs PHP
	// and Node, and no provisioning is written for either yet. If that changes,
	// this is the test that says to flip the flag rather than leaving the wizard
	// permanently apologising for something it now does.
	if len(planned) != 2 {
		t.Errorf("planned baseline entries = %v, want exactly php and node", planned)
	}
}

// The PHP the wizard names has to be the version a site is actually given.
func TestBaselineNamesThePanelPHPDefault(t *testing.T) {
	for _, o := range Baseline() {
		if o.ID == "php" {
			if !strings.Contains(o.Label, php.DefaultVersion) {
				t.Errorf("baseline php label = %q, want it to name %s", o.Label, php.DefaultVersion)
			}
			return
		}
	}
	t.Error("the baseline does not mention PHP at all")
}
