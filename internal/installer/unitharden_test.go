package installer

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/pkg/unitharden"
)

// The profiles in pkg/unitharden are only worth anything if the units actually
// carry them. These tests are the wiring check: a renderer that quietly stops
// emitting its block would otherwise pass every other test in the tree, because
// nothing else reads these files until systemd does — on a host, in production.

func TestNpdUnitCarriesTheDaemonProfile(t *testing.T) {
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, newFakeRunner())
	unit := ex.renderNpdUnit()

	for _, line := range profileLines(t, unitharden.Daemon) {
		if !strings.Contains(unit, line) {
			t.Errorf("npd unit is missing %q", line)
		}
	}
	// The filesystem confinement is per-unit rather than part of the profile, so
	// it is asserted separately — losing it would leave npd able to write
	// anywhere its user can reach.
	for _, want := range []string{"ProtectSystem=strict", "ProtectHome=true", "PrivateTmp=true", "ReadWritePaths="} {
		if !strings.Contains(unit, want) {
			t.Errorf("npd unit is missing %q", want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(unit), "WantedBy=multi-user.target") {
		t.Error("npd unit does not end with its [Install] section")
	}
}

func TestBrokerUnitCarriesTheRootBrokerProfile(t *testing.T) {
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, newFakeRunner())
	unit := ex.renderBrokerUnit("1000")

	for _, line := range profileLines(t, unitharden.RootBroker) {
		if !strings.Contains(unit, line) {
			t.Errorf("broker unit is missing %q", line)
		}
	}
	// The broker performs privileged work for a process that cannot; flipping
	// this would break every capability that drops to a site user.
	if !strings.Contains(unit, "NoNewPrivileges=false") {
		t.Error("broker unit should keep NoNewPrivileges=false")
	}
	if !strings.Contains(unit, "User=root") {
		t.Error("broker unit should run as root")
	}
}

// Both units must remain parseable: a directive accidentally emitted after the
// [Install] header would be silently ignored by systemd.
func TestUnitsPlaceHardeningInTheServiceSection(t *testing.T) {
	ex := tempExecutor(t, Options{DB: "sqlite", Port: 9443}, newFakeRunner())
	for name, unit := range map[string]string{
		"npd":    ex.renderNpdUnit(),
		"broker": ex.renderBrokerUnit("1000"),
	} {
		service := strings.Index(unit, "[Service]")
		install := strings.Index(unit, "[Install]")
		if service < 0 || install < 0 || service > install {
			t.Fatalf("%s unit: malformed sections", name)
		}
		if strings.Contains(unit[install:], "Restrict") || strings.Contains(unit[install:], "Protect") {
			t.Errorf("%s unit: hardening directives appear after [Install], where systemd ignores them", name)
		}
	}
}

func profileLines(t *testing.T, p unitharden.Profile) []string {
	t.Helper()
	return strings.Split(strings.TrimRight(p.Directives(), "\n"), "\n")
}
