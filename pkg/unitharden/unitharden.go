// Package unitharden renders the systemd sandboxing directives NexPanel writes
// into the units it manages.
//
// It exists because those directives were drifting. The panel writes four kinds
// of unit from three packages — npd and np-broker from the installer, app units
// and cron units from the broker's capabilities — and each had grown its own
// partial subset. A directive that is present in three units and missing from
// the fourth is not a style problem; it is the one unit an attacker gets to use.
//
// Each profile below is a considered set, not a maximal one. Several directives
// that look like obvious wins are deliberately absent, and the comments say
// which and why, because the next person's instinct will be to add them:
//
//   - ProcSubset=pid would hide /proc/meminfo, /proc/stat, /proc/loadavg and
//     /proc/uptime — exactly the four files npd reads to report host metrics.
//   - MemoryDenyWriteExecute breaks every JIT, so it can never go on a unit that
//     runs a customer's Node or Python application.
//   - ProtectKernelModules would stop nft autoloading nf_tables, which is how the
//     firewall comes up on a freshly booted host.
//   - ProtectSystem and ProtectHome cannot apply to the root broker at all: its
//     job is writing /etc/postfix, /etc/dovecot, /usr/local/lsws and the home
//     directories useradd creates.
//
// See docs/28-hardening.md for the audit these were derived from.
package unitharden

import "strings"

// Profile is how much a unit can be confined.
type Profile int

const (
	// Daemon is npd: an unprivileged Go service that speaks HTTP, reaches a
	// datastore and the broker socket, and reads four global files from /proc.
	// It needs no capabilities at all, and no JIT, so it takes the strictest
	// profile the panel has.
	Daemon Profile = iota

	// RootBroker is np-broker. Almost none of the filesystem sandboxing applies:
	// writing system configuration in /etc and creating users' home directories
	// is the job, so ProtectSystem, ProtectHome and PrivateTmp are all off the
	// table. What it takes instead is a *deny* list of capabilities — the ones no
	// privileged operation the broker performs has ever needed.
	//
	// The honest reading is that a root broker cannot be sandboxed into safety.
	// Its real containment is the capability allowlist, the policy checks and the
	// hash-chained audit log (ADR-0007, docs/05). These directives narrow the
	// blast radius of a compromise; they do not contain it.
	RootBroker

	// SiteWorkload is a customer's app unit or cron unit: unprivileged, running
	// as the site user, and possibly a JIT runtime. Everything except W^X.
	SiteWorkload
)

// Directives renders the profile's sandboxing block, one directive per line,
// newline-terminated. Callers append it to a unit's [Service] section.
func (p Profile) Directives() string {
	var b strings.Builder
	for _, line := range p.lines() {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (p Profile) lines() []string {
	switch p {
	case RootBroker:
		return []string{
			// A deny list, not an allow list. The broker execs useradd, runuser,
			// nft, systemctl, docker, tar and the package manager, so enumerating
			// what it *needs* would be a guess that breaks a host the first time
			// the guess is wrong. Enumerating what it demonstrably never needs is
			// a claim that can be checked by reading the capability set — and if
			// one of these turns out to be needed, the failure is narrow and
			// named rather than a panel that will not provision anything.
			//
			// What this buys: a broker compromise cannot reboot the box, load or
			// unload kernel modules, move the clock, silence the kernel audit
			// subsystem, do raw port or memory I/O, or rewrite MAC policy.
			"CapabilityBoundingSet=~CAP_SYS_BOOT CAP_SYS_MODULE CAP_SYS_TIME CAP_SYS_RAWIO " +
				"CAP_WAKE_ALARM CAP_AUDIT_CONTROL CAP_AUDIT_READ CAP_MAC_ADMIN CAP_MAC_OVERRIDE " +
				"CAP_BLOCK_SUSPEND CAP_LEASE",
			// nft and ip speak netlink; everything else is sockets and IP.
			// AF_PACKET is the notable absence: nothing the broker runs sniffs or
			// forges frames.
			"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK",
			"RestrictRealtime=true",
			"LockPersonality=true",
			"SystemCallArchitectures=native",
			// Deliberately absent for this profile: ProtectSystem, ProtectHome,
			// PrivateTmp, PrivateDevices, ProtectKernelModules, NoNewPrivileges,
			// RestrictSUIDSGID, RestrictNamespaces and any SystemCallFilter —
			// each one collides with something the broker exists to do.
		}
	case SiteWorkload:
		return append(common(), []string{
			// The site user needs no capability to serve HTTP on the high port
			// the panel assigned it.
			"CapabilityBoundingSet=",
			"AmbientCapabilities=",
			"NoNewPrivileges=true",
			"RestrictSUIDSGID=true",
			// Blocking user namespaces removes a broad class of local kernel
			// privilege escalation from a runtime that is, by definition,
			// executing code the panel's operator did not write.
			"RestrictNamespaces=true",
			"SystemCallFilter=@system-service",
			"SystemCallErrorNumber=EPERM",
			// No MemoryDenyWriteExecute: V8 and every other JIT need W^X, and an
			// app unit is exactly where one runs.
		}...)
	default: // Daemon
		return append(common(), []string{
			"CapabilityBoundingSet=",
			"AmbientCapabilities=",
			"NoNewPrivileges=true",
			"RestrictSUIDSGID=true",
			"RestrictNamespaces=true",
			"SystemCallFilter=@system-service",
			"SystemCallErrorNumber=EPERM",
			// Go does not generate code at run time, so W^X costs nothing here
			// and removes the classic "write a payload, jump to it" step.
			"MemoryDenyWriteExecute=true",
			// Hides other users' process trees. Note this is ProtectProc, not
			// ProcSubset: the global /proc files npd reads for host metrics stay
			// readable.
			"ProtectProc=invisible",
		}...)
	}
}

// common is the set both unprivileged profiles take.
func common() []string {
	return []string{
		"PrivateDevices=true",
		"PrivateMounts=true",
		"ProtectClock=true",
		"ProtectControlGroups=true",
		"ProtectHostname=true",
		"ProtectKernelLogs=true",
		"ProtectKernelModules=true",
		"ProtectKernelTunables=true",
		"RemoveIPC=true",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"RestrictRealtime=true",
		"LockPersonality=true",
		"SystemCallArchitectures=native",
	}
}
