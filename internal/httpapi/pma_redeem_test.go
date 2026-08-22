package httpapi

import (
	"net/http"
	"testing"
)

// The redeem endpoint has no session and no permission check. Loopback is what
// stands in for both, so this is the test that matters most about it.
//
// The header cases are the point. Anything in front of the panel — a reverse
// proxy, a load balancer — sets X-Forwarded-For, and so can anyone who can
// reach npd directly. Honouring it here would turn the one unauthenticated
// route in the panel into an internet-facing way to mint database credentials.
func TestLoopbackRequest(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    bool
	}{
		{"ipv4 loopback", "127.0.0.1:54321", nil, true},
		{"ipv4 loopback range", "127.0.0.53:54321", nil, true},
		{"ipv6 loopback", "[::1]:54321", nil, true},
		{"no port", "127.0.0.1", nil, true},

		{"lan address", "192.168.1.10:54321", nil, false},
		{"public address", "203.0.113.7:54321", nil, false},
		{"ipv6 public", "[2001:db8::1]:54321", nil, false},
		{"unparseable", "not-an-address", nil, false},
		{"empty", "", nil, false},

		{
			name:    "a spoofed forwarded header does not make a remote caller local",
			remote:  "203.0.113.7:54321",
			headers: map[string]string{"X-Forwarded-For": "127.0.0.1"},
			want:    false,
		},
		{
			name:    "nor does X-Real-IP",
			remote:  "203.0.113.7:54321",
			headers: map[string]string{"X-Real-IP": "127.0.0.1"},
			want:    false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodPost, "/api/v1/databases/sso/redeem", nil)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			r.RemoteAddr = c.remote
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}
			if got := isLoopbackRequest(r); got != c.want {
				t.Errorf("isLoopbackRequest(%q) = %v, want %v", c.remote, got, c.want)
			}
		})
	}
}
