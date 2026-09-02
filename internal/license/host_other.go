//go:build !linux

package license

import "net"

// The panel ships for Linux; these exist so the tree builds and the state
// machine, the token verifier and the store can be exercised on a developer's
// machine. What they report is "no reading", which is exactly what the licence
// server is told when a component is unreadable — so a developer's box behaves
// like a container with a thin identity rather than like a broken install.

func rootDevice() (devID, bool) { return devID{}, false }

func deviceNumber(string) (devID, bool) { return devID{}, false }

// The MAC is still real off Linux: there is no /sys to ask which interface is
// hardware, so every non-loopback address is a candidate and the sort in
// primaryMAC picks one deterministically.
func physicalInterfaces() []iface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]iface, 0, len(all))
	for _, in := range all {
		if in.Flags&net.FlagLoopback == 0 && len(in.HardwareAddr) > 0 {
			out = append(out, iface{name: in.Name, mac: in.HardwareAddr.String()})
		}
	}
	return out
}
