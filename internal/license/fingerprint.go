// Package license is the panel's licence client: it collects this machine's
// identity, activates against the licence server, keeps a signed lease fresh,
// and answers "may this feature run".
//
// Three rules shape every file in here, and they are worth stating before any
// of the code:
//
//   - **Nothing a customer runs is ever stopped by licence state.** Not the web
//     server, not php-fpm, not MySQL, not mail, not cron, not one existing site.
//     No data is deleted, disabled or reconfigured. What degrades is the
//     panel's own control plane — the buttons that create new things. A hosting
//     panel that takes a customer's websites down over an unpaid invoice has
//     done far more damage than the invoice was worth.
//   - **The network failing is not the licence failing.** Every call here falls
//     through to the token already on disk. This server having a bad afternoon
//     must not be a bad afternoon for every install in the fleet.
//   - **The panel decides, from a signature.** The server's answer is only as
//     trustworthy as the connection that carried it, and the machine's owner
//     controls that machine's DNS and CA store. So nothing here believes an
//     HTTP response; it believes an ed25519 signature checked against a key
//     compiled into this binary.
package license

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Components are the four things this machine is identified by.
//
// Four rather than one because any single identifier changes for innocent
// reasons: a NIC is replaced, a disk is resized and re-created, a VM template
// leaves every clone with the same machine-id until systemd regenerates it, a
// hypervisor migration rewrites the lot. The licence server treats **three of
// four matching** as the same machine, so one of these can change without
// costing the customer an activation slot.
//
// The JSON names are the wire contract with the licence server; they are what
// it hashes each component under, so renaming a field here silently makes every
// existing installation look like a different machine.
type Components struct {
	MachineID string `json:"machine_id"`
	DiskUUID  string `json:"disk_uuid"`
	MAC       string `json:"mac"`
	CPU       string `json:"cpu"`
}

// Fingerprint is the identity sent on activation: the components, and the hash
// the licence binds a token to.
type Fingerprint struct {
	Hash       string     `json:"fingerprint"`
	Components Components `json:"fp_components"`
}

// Present reports how many of the four were readable. Fewer than three means
// the server's tolerance cannot apply to this machine, because three can never
// agree — worth knowing, and worth showing an operator, rather than discovering
// it the first time a disk is resized.
func (c Components) Present() int {
	n := 0
	for _, v := range []string{c.MachineID, c.DiskUUID, c.MAC, c.CPU} {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

// Collect reads this machine's identity. It never fails.
//
// A component that cannot be read is sent empty rather than aborting: a
// container with no /etc/machine-id, a host whose root is on overlayfs with no
// UUID, a machine whose only interface is a bridge — all of those are still
// licensable, they simply have less tolerance to spare. Refusing to activate
// because one file was unreadable would turn a cosmetic gap into an install
// that cannot be sold to.
func Collect() Fingerprint {
	c := Components{
		MachineID: machineID(),
		DiskUUID:  rootDiskUUID(),
		MAC:       primaryMAC(),
		CPU:       cpuModel(),
	}
	return Fingerprint{Hash: hashComponents(c), Components: c}
}

// hashComponents derives the fingerprint the token is bound to.
//
// The separator is a NUL because none of the four can contain one, so
// ("ab", "c") and ("a", "bc") cannot collide. The order is fixed and the field
// names are included, so a future fifth component appended to the struct
// changes nothing for machines that do not report it.
func hashComponents(c Components) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"machine_id=" + c.MachineID,
		"disk_uuid=" + c.DiskUUID,
		"mac=" + c.MAC,
		"cpu=" + c.CPU,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// machineID reads systemd's host identifier.
//
// /var/lib/dbus/machine-id is the fallback because on older and minimal images
// it is the real file and /etc/machine-id is a symlink to it — or the other way
// round. Reading whichever exists costs one syscall and covers both.
func machineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			// An *empty* /etc/machine-id is a real state, not an absent file:
			// systemd writes it that way in an image that is meant to generate
			// its own on first boot. Treating it as a value would give every
			// clone of that image the same component.
			if v := strings.TrimSpace(string(b)); v != "" && v != "uninitialized" {
				return v
			}
		}
	}
	return ""
}

// rootDiskUUID finds the filesystem UUID of whatever "/" is mounted from.
//
// It matches by **device number**, not by path. /dev/disk/by-uuid holds
// symlinks, and the same block device is reachable as /dev/vda1, /dev/sda1,
// /dev/mapper/... and /dev/disk/by-id/... depending on how it was mounted;
// comparing strings would miss the match on most real systems. The kernel's
// major:minor is the same whichever name you arrive by.
func rootDiskUUID() string {
	dev, ok := rootDevice()
	if !ok {
		return ""
	}
	entries, err := os.ReadDir("/dev/disk/by-uuid")
	if err != nil {
		return ""
	}
	// Sorted, so a device that somehow has two UUID symlinks resolves the same
	// way on every boot rather than depending on directory order.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if d, ok := deviceNumber(filepath.Join("/dev/disk/by-uuid", name)); ok && d == dev {
			return name
		}
	}
	return ""
}

// cpuModel is the model name and core count, e.g. "AMD EPYC 7302P (16 cores)".
//
// Both halves matter. The model alone is shared by every VM on a host, so it
// would agree between unrelated customers; the count alone changes when a VM is
// resized, which is a legitimate thing to do. Together they are specific enough
// to be evidence and stable enough not to churn — and if the customer resizes,
// this is exactly the one component the 3-of-4 rule is there to absorb.
func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	model, cores := "", 0
	for _, line := range strings.Split(string(b), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "model name", "Model", "cpu model": // x86, some ARM kernels, mips
			if model == "" {
				model = value
			}
		case "processor":
			cores++
		}
	}
	if model == "" && cores == 0 {
		return ""
	}
	if model == "" {
		return fmt.Sprintf("(%d cores)", cores)
	}
	return fmt.Sprintf("%s (%d cores)", collapseSpaces(model), cores)
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// primaryMAC picks one hardware address, deterministically.
//
// The hard part is not reading a MAC, it is choosing which one. A hosting node
// is exactly the machine with the most interfaces: docker0 and a veth per
// container, bridges, WireGuard tunnels, bonds. Those come and go with the
// customer's own workload, so a fingerprint built on one of them would change
// whenever a container started.
//
// So only interfaces the kernel says are backed by a real device count, and
// among those the lowest name wins. Deliberately *not* the default-route
// interface, which sounds more meaningful and is not: adding a second uplink or
// failing over changes the default route without changing the machine.
func primaryMAC() string {
	candidates := physicalInterfaces()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].name < candidates[j].name })
	for _, c := range candidates {
		if mac := normaliseMAC(c.mac); mac != "" {
			return mac
		}
	}
	return ""
}

// normaliseMAC lowercases and drops the all-zero address, which some virtual
// interfaces report and which is not an identity.
func normaliseMAC(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if strings.Trim(s, "0:-.") == "" {
		return ""
	}
	return s
}

// devID is a kernel device number, kept as its two halves rather than a packed
// integer so the encoding never has to be guessed at.
type devID struct{ major, minor uint32 }

// iface is one network interface, reduced to what the fingerprint needs.
type iface struct{ name, mac string }
