package setup

import (
	"context"
	"testing"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		sel  Selection
		ok   bool
	}{
		{"ols+mariadb", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMariaDB}, true},
		{"ols+mysql", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMySQL}, true},
		{"nginx now allowed", Selection{Webserver: WebserverNginx, DBEngine: DBEngineMariaDB}, true},
		{"postgres now allowed", Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEnginePostgreSQL}, true},
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
		Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMySQL,
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
	_, err := svc.Complete(context.Background(), Selection{Webserver: WebserverOpenLiteSpeed, DBEngine: DBEngineMySQL})
	if err != nil {
		t.Fatalf("record-only complete should succeed: %v", err)
	}
	if store.state == nil || !store.state.Completed {
		t.Fatal("expected selection recorded even without a provisioner")
	}
}
