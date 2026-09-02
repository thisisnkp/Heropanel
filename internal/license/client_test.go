package license

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer stands in for the licence server, answering in its envelope.
func fakeServer(t *testing.T, h func(path string, body map[string]any) (int, any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		status, data := h(r.URL.Path, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if e, ok := data.(*ServerError); ok {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": e.Code, "message": e.Message},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func newTestService(t *testing.T, url string) (*Service, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		ring:    Keyring{"lk1": pub},
		client:  NewClient(url),
		version: "1.4.0",
		log:     testLogger(),
		now:     func() time.Time { return epoch },
		refresh: make(chan struct{}, 1),
	}
	store, err := NewStore(t.TempDir(), svc.ring)
	if err != nil {
		t.Fatal(err)
	}
	svc.store = store
	return svc, priv
}

// The heartbeat's whole authentication, checked against the contract rather
// than against our own encoder: HMAC-SHA256(secret, "lid|fingerprint|nonce|ts").
func TestHeartbeatIsSignedWithTheInstallSecret(t *testing.T) {
	const secret = "nxi_test-secret"
	var seen atomic.Value

	srv := fakeServer(t, func(_ string, body map[string]any) (int, any) {
		seen.Store(body)
		return 200, map[string]any{"token": "", "commands": []string{}}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	c.Now = func() time.Time { return epoch }
	if _, err := c.Heartbeat(context.Background(), "lic_x", "sha256:fp", secret, "h", "os", "1.0"); err != nil {
		t.Fatal(err)
	}

	body, _ := seen.Load().(map[string]any)
	if body == nil {
		t.Fatal("the server saw no request")
	}
	nonce, _ := body["nonce"].(string)
	ts := strconv.FormatInt(epoch.Unix(), 10)
	if nonce == "" {
		t.Fatal("no nonce was sent")
	}
	if got := body["ts"]; got != float64(epoch.Unix()) {
		t.Fatalf("ts = %v, want %d", got, epoch.Unix())
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("lic_x|sha256:fp|" + nonce + "|" + ts))
	if want, got := hex.EncodeToString(mac.Sum(nil)), body["hmac"]; got != want {
		t.Fatalf("hmac = %v, want %s", got, want)
	}
	// The licence key must not travel on a heartbeat. That is the whole reason
	// the install secret exists.
	if _, present := body["key"]; present {
		t.Fatal("the heartbeat carried a licence key")
	}
}

// Every attempt gets a fresh nonce, or the server's replay protection would
// refuse the panel's own retries.
func TestEachHeartbeatUsesAFreshNonce(t *testing.T) {
	var nonces []string
	srv := fakeServer(t, func(_ string, body map[string]any) (int, any) {
		n, _ := body["nonce"].(string)
		nonces = append(nonces, n)
		return 200, map[string]any{"token": "", "commands": []string{}}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	for i := 0; i < 5; i++ {
		if _, err := c.Heartbeat(context.Background(), "lic_x", "fp", "s", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, n := range nonces {
		if seen[n] {
			t.Fatalf("nonce %q was reused", n)
		}
		seen[n] = true
	}
}

// A refusal the server made on purpose is not retried: a revoked licence does
// not become valid because we asked three times, and the operator watching a
// CLI deserves the answer now.
func TestDeliberateRefusalsAreNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := fakeServer(t, func(string, map[string]any) (int, any) {
		calls.Add(1)
		return 403, &ServerError{Code: CodeKeyRevoked, Message: "This licence has been revoked."}
	})
	defer srv.Close()

	_, err := NewClient(srv.URL).Activate(context.Background(), "NXP-X", Fingerprint{}, "", "", "")
	var se *ServerError
	if !errors.As(err, &se) || se.Code != CodeKeyRevoked {
		t.Fatalf("err = %v, want a KEY_REVOKED ServerError", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("the server was called %d times for a terminal refusal", got)
	}
}

// A 5xx is transient, so it is retried — but bounded.
func TestTransientFailuresAreRetried(t *testing.T) {
	var calls atomic.Int32
	srv := fakeServer(t, func(string, map[string]any) (int, any) {
		if calls.Add(1) < 3 {
			return 503, &ServerError{Code: "unavailable", Message: "try later"}
		}
		return 200, map[string]any{"token": "", "commands": []string{}}
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.Heartbeat(context.Background(), "lic_x", "fp", "s", "", "", ""); err != nil {
		t.Fatalf("the third attempt should have succeeded: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

// The rule the whole product rests on: the licence server being unreachable
// must not change what this panel allows.
func TestNetworkFailureIsNotALicenceFailure(t *testing.T) {
	svc, priv := newTestService(t, "http://127.0.0.1:1") // nothing listens there
	claims := validClaims(epoch)
	if err := svc.store.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}

	if err := svc.beat(context.Background()); err == nil {
		t.Fatal("a heartbeat to nowhere reported success")
	}

	// The state is still whatever the stored lease says.
	if got := svc.Status().State; got != StateActive {
		t.Fatalf("state = %q after an unreachable server, want active", got)
	}
	if err := svc.CanCreateSite(func() int { return 0 }); err != nil {
		t.Fatalf("creating a site was refused because the network was down: %v", err)
	}
}

// A token the server returns for a *different* machine is refused rather than
// stored. Otherwise a compromised or confused server could quietly move an
// install onto somebody else's licence.
func TestATokenForAnotherMachineIsRefused(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	claims := validClaims(epoch)
	claims.FP = "sha256:someone-else"

	srv := fakeServer(t, func(string, map[string]any) (int, any) {
		return 200, map[string]any{
			"token":          mintWith(priv, claims),
			"install_secret": "nxi_s",
		}
	})
	defer srv.Close()

	svc, _ := newTestService(t, srv.URL)
	svc.ring = Keyring{"lk1": pub}
	store, _ := NewStore(t.TempDir(), svc.ring)
	svc.store = store

	if _, err := svc.Activate(context.Background(), "NXP-KEY"); err == nil {
		t.Fatal("a token bound to another machine was accepted")
	}
	if svc.store.Snapshot(epoch).Activated {
		t.Fatal("the rejected activation was written to disk anyway")
	}
}

// A revoke command locks the panel and survives the process.
func TestRevokeCommandIsRecorded(t *testing.T) {
	srv := fakeServer(t, func(string, map[string]any) (int, any) {
		return 200, map[string]any{"token": "", "commands": []string{CmdRevoke}}
	})
	defer srv.Close()

	svc, priv := newTestService(t, srv.URL)
	claims := validClaims(epoch.Add(30 * 24 * time.Hour)) // a current licence
	if err := svc.store.SaveActivation(mint(t, priv, "lk1", claims), claims.LID, claims.FP, "nxi_s", claims, epoch); err != nil {
		t.Fatal(err)
	}

	if err := svc.beat(context.Background()); err != nil {
		t.Fatalf("a revoke response is an answer, not an error: %v", err)
	}
	st := svc.Status()
	if st.State != StateLocked || !st.Revoked {
		t.Fatalf("status = %+v, want locked and revoked", st)
	}
	// And creation is refused with a message that does not frighten anyone
	// about their websites.
	err := svc.CanCreateSite(func() int { return 0 })
	if err == nil {
		t.Fatal("a revoked licence still allowed a new site")
	}
	if !contains(err.Error(), "keep running") {
		t.Fatalf("the refusal does not reassure about services: %v", err)
	}
}

// backoff spreads retries. Without the spread, a fleet that all failed against
// one outage retries in the same instant and knocks the server over again.
func TestBackoffIsJittered(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 40; i++ {
		// Full jitter around a 2s base for the second attempt: [base/2, 1.5*base).
		d := backoff(2)
		if d < time.Second || d >= 3*time.Second {
			t.Fatalf("backoff(2) = %s, outside [1s, 3s)", d)
		}
		seen[d] = true
	}
	if len(seen) < 5 {
		t.Fatalf("only %d distinct delays in 40 draws — the jitter is not jittering", len(seen))
	}
}

func mintWith(priv ed25519.PrivateKey, claims Claims) string {
	header, _ := json.Marshal(map[string]string{"alg": Alg, "typ": "JWT", "kid": "lk1"})
	payload, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signing + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(signing)))
}
