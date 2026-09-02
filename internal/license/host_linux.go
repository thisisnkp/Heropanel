//go:build linux

package license

import (
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// rootDevice returns the kernel device number backing "/".
//
// Read from /proc/self/mountinfo rather than stat("/"), because on a host with
// overlayfs or a bind-mounted root the two disagree, and mountinfo is the one
// that names the block device an operator would recognise. Field 3 of every
// line is "major:minor" and field 5 is the mount point.
func rootDevice() (devID, bool) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return devID{}, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[4] != "/" {
			continue
		}
		majStr, minStr, ok := strings.Cut(f[2], ":")
		if !ok {
			continue
		}
		maj, err1 := strconv.ParseUint(majStr, 10, 32)
		min, err2 := strconv.ParseUint(minStr, 10, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		// A major of 0 is an anonymous device — tmpfs, overlay, a container's
		// own root. Real, but not a disk anyone can look up a UUID for, so it
		// is reported as "no reading" rather than as a number that will never
		// match anything in /dev/disk/by-uuid.
		if maj == 0 {
			return devID{}, false
		}
		return devID{major: uint32(maj), minor: uint32(min)}, true
	}
	return devID{}, false
}

// deviceNumber resolves a path to the device it refers to.
//
// unix.Major/unix.Minor rather than shifting the raw dev_t by hand: Linux's
// encoding is not the obvious one — the major is split across two ranges of
// bits to make room for a 20-bit minor — and a hand-rolled shift silently works
// for /dev/sda1 and breaks on a device-mapper volume with a high minor, which
// is precisely the setup an encrypted or LVM root has.
func deviceNumber(path string) (devID, bool) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return devID{}, false
	}
	return devID{major: unix.Major(uint64(st.Rdev)), minor: unix.Minor(uint64(st.Rdev))}, true
}

// physicalInterfaces lists interfaces the kernel says are backed by a real
// device.
//
// The test is the presence of /sys/class/net/<name>/device, which is a symlink
// into the device tree. That is the kernel's own answer to "is this hardware",
// and it is far better than a list of name prefixes: docker0, veth*, br-*,
// wg*, tailscale0 and every future virtual interface fail it without anyone
// having to remember to add them, while a NIC named something unexpected still
// passes.
func physicalInterfaces() []iface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]iface, 0, len(all))
	for _, in := range all {
		if in.Flags&net.FlagLoopback != 0 || len(in.HardwareAddr) == 0 {
			continue
		}
		if _, err := os.Stat("/sys/class/net/" + in.Name + "/device"); err != nil {
			continue
		}
		out = append(out, iface{name: in.Name, mac: in.HardwareAddr.String()})
	}
	// A machine whose only interfaces are virtual — a container, or a VM whose
	// NIC driver exposes no device link — still has an identity worth reporting.
	// Falling back to any non-loopback address beats an empty component, and
	// the 3-of-4 rule absorbs it if it later churns.
	if len(out) == 0 {
		out = anyInterfaces(all)
	}
	return out
}

func anyInterfaces(all []net.Interface) []iface {
	out := make([]iface, 0, len(all))
	for _, in := range all {
		if in.Flags&net.FlagLoopback == 0 && len(in.HardwareAddr) > 0 {
			out = append(out, iface{name: in.Name, mac: in.HardwareAddr.String()})
		}
	}
	return out
}
