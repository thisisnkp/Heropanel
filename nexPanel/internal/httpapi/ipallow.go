package httpapi

import (
	"net"
	"net/http"
)

// ipAllowlist restricts access to a set of CIDRs (or bare IPs). It sits early
// in the chain — before auth — so a request from a disallowed address never
// reaches a handler or an audit entry it should not. An empty list disables
// the check (the default); a misconfigured one can lock the operator out,
// which is why it is opt-in.
//
// Note: this guards the panel at the application layer, complementing (not
// replacing) the host firewall — defence in depth, and it works even where the
// operator cannot change nftables (a managed load balancer in front).
func ipAllowlist(cidrs []string) mw {
	nets, bare := parseAllowlist(cidrs)
	return func(next http.Handler) http.Handler {
		if len(nets) == 0 && len(bare) == 0 {
			return next // disabled
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := net.ParseIP(clientIP(r))
			if ip == nil || !allowed(ip, nets, bare) {
				writeAPIError(w, r, http.StatusForbidden, "ip_not_allowed",
					"Access from your network is not permitted.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parseAllowlist splits the configured entries into networks and bare IPs,
// dropping anything unparseable (a typo must not silently widen access).
func parseAllowlist(cidrs []string) ([]*net.IPNet, []net.IP) {
	var nets []*net.IPNet
	var bare []net.IP
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
			continue
		}
		if ip := net.ParseIP(c); ip != nil {
			bare = append(bare, ip)
		}
	}
	return nets, bare
}

func allowed(ip net.IP, nets []*net.IPNet, bare []net.IP) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	for _, b := range bare {
		if b.Equal(ip) {
			return true
		}
	}
	return false
}
