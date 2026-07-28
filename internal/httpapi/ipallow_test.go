package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPAllowlist(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Empty allowlist is a no-op: everyone passes.
	h := ipAllowlist(nil)(next)
	if code := call(h, "203.0.113.9:1234"); code != http.StatusOK {
		t.Errorf("empty allowlist blocked a request: %d", code)
	}

	// A CIDR admits addresses inside it and rejects the rest.
	h = ipAllowlist([]string{"10.0.0.0/8", "203.0.113.5"})(next)
	cases := map[string]int{
		"10.1.2.3:5000":   http.StatusOK,        // inside the CIDR
		"203.0.113.5:443": http.StatusOK,        // the bare IP
		"203.0.113.6:443": http.StatusForbidden, // one off the bare IP
		"192.168.1.1:22":  http.StatusForbidden, // outside everything
	}
	for addr, want := range cases {
		if code := call(h, addr); code != want {
			t.Errorf("%s: got %d, want %d", addr, code, want)
		}
	}

	// A malformed allowlist entry must not silently widen access — it is dropped,
	// leaving the valid entries in force.
	h = ipAllowlist([]string{"not-a-cidr", "10.0.0.0/8"})(next)
	if code := call(h, "10.9.9.9:1"); code != http.StatusOK {
		t.Errorf("valid entry alongside a bad one stopped working: %d", code)
	}
	if code := call(h, "8.8.8.8:1"); code != http.StatusForbidden {
		t.Errorf("a bad entry widened access: %d", code)
	}
}

func call(h http.Handler, remoteAddr string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}
