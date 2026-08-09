package unitharden_test

import (
	"strings"
	"testing"

	"github.com/thisisnkp/nexpanel/pkg/unitharden"
)

// These tests encode the reasoning behind the profiles, not just their current
// text. Each absence below is a directive that looks like an obvious win and
// would break something real, so the next person who adds it should be stopped
// by a failing test with a reason in it rather than by a support ticket.

func directives(t *testing.T, p unitharden.Profile) string {
	t.Helper()
	out := p.Directives()
	if out == "" {
		t.Fatal("profile rendered nothing")
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("profile block must end in a newline so callers can append a section")
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if !strings.Contains(line, "=") {
			t.Errorf("line %q is not a systemd directive", line)
		}
	}
	return out
}

// ProcSubset=pid hides /proc/meminfo, /proc/stat, /proc/loadavg and
// /proc/uptime — the four files npd reads to report host metrics. ProtectProc,
// which hides other users' process trees but leaves those readable, is the one
// that belongs here.
func TestDaemonDoesNotHideTheProcFilesNpdReads(t *testing.T) {
	out := directives(t, unitharden.Daemon)
	if strings.Contains(out, "ProcSubset") {
		t.Error("Daemon sets ProcSubset, which would break host metrics collection")
	}
	if !strings.Contains(out, "ProtectProc=invisible") {
		t.Error("Daemon should still hide other users' process trees")
	}
}

// A JIT needs writable-then-executable memory. An app unit is precisely where a
// customer's Node or Python runtime executes, so W^X can never apply there.
func TestSiteWorkloadAllowsJIT(t *testing.T) {
	if strings.Contains(directives(t, unitharden.SiteWorkload), "MemoryDenyWriteExecute") {
		t.Error("SiteWorkload sets MemoryDenyWriteExecute, which breaks every JIT runtime")
	}
}

// Go generates no code at run time, so npd loses nothing by refusing W+X pages.
func TestDaemonDeniesWriteExecute(t *testing.T) {
	if !strings.Contains(directives(t, unitharden.Daemon), "MemoryDenyWriteExecute=true") {
		t.Error("Daemon should deny write+execute memory")
	}
}

// Both unprivileged profiles run processes that need no capability whatsoever.
// An empty bounding set is the strongest statement available, and it is correct
// for them.
func TestUnprivilegedProfilesDropAllCapabilities(t *testing.T) {
	for name, p := range map[string]unitharden.Profile{
		"Daemon":       unitharden.Daemon,
		"SiteWorkload": unitharden.SiteWorkload,
	} {
		out := directives(t, p)
		if !strings.Contains(out, "CapabilityBoundingSet=\n") {
			t.Errorf("%s: want an empty CapabilityBoundingSet", name)
		}
		if !strings.Contains(out, "NoNewPrivileges=true") {
			t.Errorf("%s: want NoNewPrivileges=true", name)
		}
		if !strings.Contains(out, "RestrictNamespaces=true") {
			t.Errorf("%s: want RestrictNamespaces=true", name)
		}
	}
}

// The root broker's containment is its capability allowlist and audit chain, not
// systemd. These four directives each collide with something it exists to do —
// writing /etc/postfix, creating home directories, letting nft autoload
// nf_tables — so their absence is the design, and a test should say so.
func TestRootBrokerOmitsWhatWouldBreakIt(t *testing.T) {
	out := directives(t, unitharden.RootBroker)
	for _, forbidden := range []string{
		"ProtectSystem",
		"ProtectHome",
		"ProtectKernelModules",
		"NoNewPrivileges=true",
		"SystemCallFilter",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("RootBroker sets %s, which collides with a privileged operation it must perform", forbidden)
		}
	}
}

// The broker's set must stay a deny list. An allow list here would be a guess at
// what root needs, and the first wrong guess is a host that cannot provision.
func TestRootBrokerCapabilitiesAreADenyList(t *testing.T) {
	out := directives(t, unitharden.RootBroker)
	if !strings.Contains(out, "CapabilityBoundingSet=~CAP_") {
		t.Fatal("RootBroker should express capabilities as a deny list (~CAP_...)")
	}
	// The ones a compromised broker must not reach, whatever else it can do.
	for _, want := range []string{
		"CAP_SYS_BOOT",      // reboot the host
		"CAP_SYS_MODULE",    // load a rootkit
		"CAP_SYS_TIME",      // move the clock under the audit log
		"CAP_SYS_RAWIO",     // raw port and memory access
		"CAP_AUDIT_CONTROL", // silence the kernel audit subsystem
		"CAP_MAC_ADMIN",     // rewrite MAC policy
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RootBroker deny list is missing %s", want)
		}
	}
}

// AF_PACKET is the family that sniffs and forges frames; nothing the panel runs
// needs it, on any profile.
func TestNoProfileAllowsPacketSockets(t *testing.T) {
	for name, p := range map[string]unitharden.Profile{
		"Daemon":       unitharden.Daemon,
		"RootBroker":   unitharden.RootBroker,
		"SiteWorkload": unitharden.SiteWorkload,
	} {
		out := directives(t, p)
		if !strings.Contains(out, "RestrictAddressFamilies=") {
			t.Errorf("%s: want RestrictAddressFamilies", name)
		}
		if strings.Contains(out, "AF_PACKET") {
			t.Errorf("%s: allows AF_PACKET", name)
		}
	}
}

// nft and ip speak netlink, so the broker needs it — and the unprivileged
// profiles do not, so they must not have it.
func TestOnlyTheBrokerGetsNetlink(t *testing.T) {
	if !strings.Contains(directives(t, unitharden.RootBroker), "AF_NETLINK") {
		t.Error("RootBroker needs AF_NETLINK for nft and ip")
	}
	for name, p := range map[string]unitharden.Profile{
		"Daemon":       unitharden.Daemon,
		"SiteWorkload": unitharden.SiteWorkload,
	} {
		if strings.Contains(directives(t, p), "AF_NETLINK") {
			t.Errorf("%s: does not need AF_NETLINK", name)
		}
	}
}

// Every profile takes the directives that cost nothing anywhere.
func TestEveryProfileTakesTheFreeWins(t *testing.T) {
	for name, p := range map[string]unitharden.Profile{
		"Daemon":       unitharden.Daemon,
		"RootBroker":   unitharden.RootBroker,
		"SiteWorkload": unitharden.SiteWorkload,
	} {
		out := directives(t, p)
		for _, want := range []string{
			"LockPersonality=true",
			"RestrictRealtime=true",
			"SystemCallArchitectures=native",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %s", name, want)
			}
		}
	}
}
