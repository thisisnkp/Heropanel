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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Components are the four things this machine is identified by.
//
// Every one of them is **written to disk and read back**, which is the entire
// selection rule and the reason this list no longer contains the CPU or the
// MAC. A VPS resize gives the customer a different CPU model, a different core
// count and usually a new MAC; a live migration moves them onto different host
// hardware entirely. Those are ordinary things a customer pays their provider
// to do, and a licence that treated them as "different machine" would spend a
// seat and open a support ticket every time somebody clicked "upgrade".
//
//   - InstallID   — a UUID NexPanel writes once, at install time, and never
//     rewrites. The strongest of the four: nothing outside the
//     panel can change it, and it survives every hardware event.
//   - MachineID   — /etc/machine-id.
//   - ProductUUID — /sys/class/dmi/id/product_uuid, what the firmware or the
//     hypervisor says this box is. Survives an OS reinstall,
//     which nothing else here does.
//   - DiskUUID    — the root filesystem's UUID.
//
// The licence server treats **two of four matching** as the same machine, so
// two of these can change at once without costing the customer anything.
//
// The JSON names are the wire contract with the licence server; they are what
// it hashes each component under, so renaming a field here silently makes every
// existing installation look like a different machine.
type Components struct {
	InstallID   string `json:"install_id"`
	MachineID   string `json:"machine_id"`
	ProductUUID string `json:"product_uuid"`
	DiskUUID    string `json:"disk_uuid"`
}

// SoftSignals are hardware facts that are reported and never enforced on.
//
// They exist so an administrator reviewing a machine that tripped a
// clone-detection rule has something a person can read: "same install id,
// completely different CPU" is a sentence somebody can act on. Nothing scores
// them, nothing compares them, and a machine whose every soft signal changed
// overnight is still the same machine — that is precisely what a resize looks
// like.
type SoftSignals struct {
	CPU   string `json:"cpu"`
	Cores int    `json:"cores"`
	RAMMB int    `json:"ram_mb"`
	MAC   string `json:"mac"`
}

// Fingerprint is the identity sent on activation: the components, the soft
// signals, and the hash the licence binds a token to.
type Fingerprint struct {
	Hash       string      `json:"fingerprint"`
	Components Components  `json:"fp_components"`
	Signals    SoftSignals `json:"soft_signals"`
}

// Present reports how many of the four were readable. Fewer than two means the
// server's tolerance cannot apply to this machine, because two can never agree
// — worth knowing, and worth showing an operator, rather than discovering it
// the first time a disk is rebuilt.
func (c Components) Present() int {
	n := 0
	for _, v := range []string{c.InstallID, c.MachineID, c.ProductUUID, c.DiskUUID} {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

// Collect reads this machine's identity. It never fails.
//
// A component that cannot be read is sent empty rather than aborting: a
// container with no /etc/machine-id, a cloud instance whose DMI tables are not
// exposed, a host whose root is on overlayfs with no UUID — all of those are
// still licensable, they simply have less tolerance to spare. Refusing to
// activate because one file was unreadable would turn a cosmetic gap into an
// install that cannot be sold to.
//
// `dir` is where the install id lives, and is the panel's data directory —
// /var/lib/nexpanel in production. It is a parameter rather than a constant so
// a test can run without writing to a real system path.
func Collect(dir string) Fingerprint {
	c := Components{
		InstallID:   installID(dir),
		MachineID:   machineID(),
		ProductUUID: productUUID(),
		DiskUUID:    rootDiskUUID(),
	}
	return Fingerprint{Hash: hashComponents(c), Components: c, Signals: collectSignals()}
}

// hashComponents derives the fingerprint the token is bound to.
//
// The separator is a NUL because none of the four can contain one, so
// ("ab", "c") and ("a", "bc") cannot collide. The order is fixed and the field
// names are included, so a future fifth component appended to the struct
// changes nothing for machines that do not report it.
func hashComponents(c Components) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"install_id=" + c.InstallID,
		"machine_id=" + c.MachineID,
		"product_uuid=" + c.ProductUUID,
		"disk_uuid=" + c.DiskUUID,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// InstallIDFile is the name of the install id inside the data directory.
const InstallIDFile = "install-id"

// installID reads the panel's own identifier, creating it on first call.
//
// This is the component the whole scheme leans on, because it is the only one
// nothing outside NexPanel can change: a resize does not touch it, a migration
// does not touch it, an OS upgrade that keeps /var does not touch it.
//
// Written with O_EXCL, and a collision falls back to reading. Two processes
// racing here is not hypothetical — the daemon starting while an operator runs
// `npd license activate` does exactly that — and the failure mode of a plain
// "check then write" is two different ids for one machine, which is a support
// ticket nobody could diagnose.
//
// Mode 0600. Anyone who can read this file can pretend to be this machine to
// the licence server; anyone who can *write* it can make this machine claim to
// be another one. Both are root-only on a correctly installed panel, and the
// mode says so rather than relying on the directory.
func installID(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = "/var/lib/nexpanel"
	}
	path := filepath.Join(dir, InstallIDFile)

	if v, ok := readInstallID(path); ok {
		return v
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}

	id, err := newUUID()
	if err != nil {
		return ""
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// Lost the race, or cannot write here. Either way the file is the
		// authority, never this process's freshly minted value: returning the
		// new id after failing to store it would give the machine a different
		// identity on every boot.
		if v, ok := readInstallID(path); ok {
			return v
		}
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(id + "\n"); err != nil {
		return ""
	}
	// Flushed before it is used. An install id that reached the licence server
	// but not the disk would be gone after a power cut, and the machine would
	// come back looking like a different one.
	if err := f.Sync(); err != nil {
		return ""
	}
	return id
}

func readInstallID(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			// Unreadable for some other reason — a permissions change, a
			// filesystem error. Treated as "no reading", which costs one
			// component out of four rather than inventing a second identity.
			return "", true
		}
		return "", false
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", false
	}
	return v, true
}

// newUUID builds a random (version 4) UUID without pulling in a dependency.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("licence: no randomness for an install id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
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
			if v := usable(string(b)); v != "" {
				return v
			}
		}
	}
	return ""
}

// productUUID reads what the firmware or the hypervisor says this machine is.
//
// The one component that survives a full OS reinstall, which is why it earns a
// slot. It is root-readable only (mode 0400) — npd runs as root, and a
// non-root reader simply gets an empty component rather than an error.
//
// product_uuid is checked against the placeholder list like everything else,
// and that matters more here than anywhere: DMI tables are full of stock text,
// and whole fleets cut from one template report the same demo UUID. Sending one
// of those would be worse than sending nothing — the server would count it as
// agreement between two unrelated machines.
func productUUID() string {
	for _, p := range []string{
		"/sys/class/dmi/id/product_uuid",
		// Containers and some ARM boards expose the same value here instead.
		"/sys/devices/virtual/dmi/id/product_uuid",
	} {
		if b, err := os.ReadFile(p); err == nil {
			if v := usable(string(b)); v != "" {
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
//
// This is what `blkid` would report, read from the kernel directly rather than
// by shelling out to a binary that may not be installed.
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

// placeholders are values that are technically present and identify nothing.
//
// Filtered here as well as on the server, and deliberately not only there: a
// panel that sends "To Be Filled By O.E.M." as its product UUID is telling the
// server something false about itself, and the honest report is that the
// component could not be read.
var placeholders = map[string]struct{}{
	"": {}, "0": {}, "none": {}, "unknown": {}, "null": {}, "n/a": {}, "na": {},
	"not specified": {}, "not available": {}, "default string": {},
	"to be filled by o.e.m.": {}, "system serial number": {}, "system uuid": {},
	"uninitialized":                        {},
	"00000000-0000-0000-0000-000000000000": {},
	"ffffffff-ffff-ffff-ffff-ffffffffffff": {},
	// VMware's and VirtualBox's stock demo UUIDs, shipped identically on every
	// machine that never had a real one written.
	"03000200-0400-0500-0006-000700080009": {},
	"12345678-1234-5678-1234-567812345678": {},
}

// usable trims a component value and blanks it if it is a known placeholder.
func usable(s string) string {
	v := strings.TrimSpace(s)
	if _, bad := placeholders[strings.ToLower(v)]; bad {
		return ""
	}
	return v
}

// ── soft signals ────────────────────────────────────────────────────────────

// collectSignals reads the facts that are reported and never scored.
func collectSignals() SoftSignals {
	model, cores := cpuInfo()
	return SoftSignals{CPU: model, Cores: cores, RAMMB: totalRAMMB(), MAC: primaryMAC()}
}

// cpuInfo is the model name and the core count.
//
// Both were components under the previous design, and both are the clearest
// possible example of why that was wrong: the model changes when the provider
// migrates the VM to a newer host, and the count changes the moment the
// customer resizes. Neither event means the machine is a different machine.
func cpuInfo() (string, int) {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", 0
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
				model = collapseSpaces(value)
			}
		case "processor":
			cores++
		}
	}
	return model, cores
}

// totalRAMMB reads MemTotal, in megabytes. Zero when it cannot be read.
func totalRAMMB() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != "MemTotal" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0
		}
		kb, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// primaryMAC picks one hardware address, deterministically.
//
// The hard part is not reading a MAC, it is choosing which one. A hosting node
// is exactly the machine with the most interfaces: docker0 and a veth per
// container, bridges, WireGuard tunnels, bonds. Those come and go with the
// customer's own workload, so a value built on one of them changes whenever a
// container starts.
//
// So only interfaces the kernel says are backed by a real device count, and
// among those the lowest name wins. Deliberately *not* the default-route
// interface, which sounds more meaningful and is not: adding a second uplink or
// failing over changes the default route without changing the machine.
//
// It is a soft signal now rather than a component, which removes the sting from
// getting this wrong: a MAC that churns costs a line in an admin table, not a
// seat.
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
