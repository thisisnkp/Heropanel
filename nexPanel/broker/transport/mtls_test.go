package transport

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/thisisnkp/nexpanel/broker"
	"github.com/thisisnkp/nexpanel/broker/audit"
	"github.com/thisisnkp/nexpanel/broker/exec"
	"github.com/thisisnkp/nexpanel/broker/policy"
	npdbroker "github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/pkg/nodepki"
)

// These are the two-node prototype, minus the second machine: a real np-broker
// serving a real TLS listener on loopback, and npd's real client dialling it.
// Everything between them — handshake, certificate verification, the allowlist,
// the brokerwire framing and the audit chain — is the shipping code path.
//
// What loopback cannot prove is stated in docs/27: latency, MTU, a firewall in
// the middle, and a genuinely separate host's clock. What it does prove is the
// part that decides whether the endpoint is safe at all, which is who gets in.

const testToken = "test-token"

type testPKI struct {
	ca     *nodepki.CA
	server nodepki.Pair
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	ca, err := nodepki.NewCA("test ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	server, err := ca.Issue(nodepki.Options{
		CommonName: "np-broker",
		Server:     true,
		DNSNames:   []string{"np-broker"},
		IPs:        []net.IP{net.ParseIP("127.0.0.1")},
	})
	if err != nil {
		t.Fatalf("issue server cert: %v", err)
	}
	return testPKI{ca: ca, server: server}
}

// client issues a node certificate from this PKI.
func (p testPKI) client(t *testing.T, cn string, ttl time.Duration) nodepki.Pair {
	t.Helper()
	pair, err := p.ca.Issue(nodepki.Options{CommonName: cn, Client: true, TTL: ttl})
	if err != nil {
		t.Fatalf("issue client cert %q: %v", cn, err)
	}
	return pair
}

// auditingServer returns a broker server plus the audit entries it records, so a
// test can assert on what the chain actually attributed the call to.
func auditingServer(t *testing.T, nodes ...string) (*Server, *[]audit.Entry) {
	t.Helper()
	var entries []audit.Entry
	chain := audit.NewChain(func(e audit.Entry) error {
		entries = append(entries, e)
		return nil
	})
	b := broker.New(broker.DefaultRegistry(), policy.Default(), chain, &exec.FakeRunner{}, nil)
	srv := NewServer(b, testToken, slog.New(slog.DiscardHandler))
	srv.AllowedNodes = nodes
	return srv, &entries
}

// serveRemote starts srv on a loopback TLS listener and returns its address.
func serveRemote(t *testing.T, srv *Server, p testPKI) string {
	t.Helper()
	cfg, err := nodepki.ServerTLS(p.server.CertPEM, p.server.KeyPEM, p.ca.CertPEM)
	if err != nil {
		t.Fatalf("server tls: %v", err)
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ln := tls.NewListener(raw, cfg)
	go func() { _ = srv.ServeRemote(ctx, ln) }()
	return raw.Addr().String()
}

// callAs dials the broker as a node and invokes a capability.
func callAs(t *testing.T, addr string, p testPKI, pair nodepki.Pair) error {
	t.Helper()
	cfg, err := nodepki.ClientTLS(pair.CertPEM, pair.KeyPEM, p.ca.CertPEM, "np-broker")
	if err != nil {
		t.Fatalf("client tls: %v", err)
	}
	return invoke(t, addr, cfg)
}

func invoke(t *testing.T, addr string, cfg *tls.Config) error {
	t.Helper()
	c, err := npdbroker.NewTLSClient(addr, testToken, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("new tls client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = c.Invoke(ctx, "service.restart", map[string]string{"service": "mariadb"})
	return err
}

// A node in the allowlist, holding a certificate from the panel's CA, can drive
// a privileged capability across the network — and the audit chain records the
// identity the *transport* proved, not the one the caller claimed.
func TestRemoteNodeInvokesCapability(t *testing.T) {
	p := newTestPKI(t)
	srv, entries := auditingServer(t, "node-b")
	addr := serveRemote(t, srv, p)

	if err := callAs(t, addr, p, p.client(t, "node-b", 0)); err != nil {
		t.Fatalf("invoke over mTLS: %v", err)
	}
	if len(*entries) == 0 {
		t.Fatal("no audit entries recorded")
	}
	for _, e := range *entries {
		if !strings.Contains(e.Actor, "node:node-b") {
			t.Errorf("audit actor = %q, want it to carry the attested node", e.Actor)
		}
	}
	if err := audit.Verify(*entries); err != nil {
		t.Errorf("audit chain did not verify: %v", err)
	}
}

// A certificate the panel's own CA signed is still not permission to drive root:
// the allowlist is a separate decision from "is this a real node".
func TestRemoteRefusesNodeOutsideAllowlist(t *testing.T) {
	p := newTestPKI(t)
	srv, entries := auditingServer(t, "node-b")
	addr := serveRemote(t, srv, p)

	if err := callAs(t, addr, p, p.client(t, "node-c", 0)); err == nil {
		t.Fatal("a node outside the allowlist was allowed to call")
	}
	if len(*entries) != 0 {
		t.Errorf("rejected node reached the broker: %d audit entries", len(*entries))
	}
}

// A certificate from some other CA must not authenticate, however well-formed.
func TestRemoteRefusesForeignCA(t *testing.T) {
	p := newTestPKI(t)
	srv, entries := auditingServer(t, "node-b")
	addr := serveRemote(t, srv, p)

	other, err := nodepki.NewCA("other ca", 0)
	if err != nil {
		t.Fatalf("new ca: %v", err)
	}
	// Same common name as the allowlisted node — only the issuer differs, which
	// is the whole point: the name is a claim, the chain is the proof.
	rogue, err := other.Issue(nodepki.Options{CommonName: "node-b", Client: true})
	if err != nil {
		t.Fatalf("issue rogue cert: %v", err)
	}
	cfg, err := nodepki.ClientTLS(rogue.CertPEM, rogue.KeyPEM, p.ca.CertPEM, "np-broker")
	if err != nil {
		t.Fatalf("client tls: %v", err)
	}
	if err := invoke(t, addr, cfg); err == nil {
		t.Fatal("a certificate from an untrusted CA was accepted")
	}
	if len(*entries) != 0 {
		t.Errorf("foreign-CA peer reached the broker: %d audit entries", len(*entries))
	}
}

// Encryption without client authentication is the failure this endpoint exists
// to avoid, so a peer presenting no certificate at all must be turned away.
func TestRemoteRefusesMissingClientCertificate(t *testing.T) {
	p := newTestPKI(t)
	srv, entries := auditingServer(t, "node-b")
	addr := serveRemote(t, srv, p)

	pool, err := nodepki.CertPool(p.ca.CertPEM)
	if err != nil {
		t.Fatalf("cert pool: %v", err)
	}
	cfg := &tls.Config{RootCAs: pool, ServerName: "np-broker", MinVersion: tls.VersionTLS13}
	if err := invoke(t, addr, cfg); err == nil {
		t.Fatal("a peer with no client certificate was accepted")
	}
	if len(*entries) != 0 {
		t.Errorf("uncertified peer reached the broker: %d audit entries", len(*entries))
	}
}

// This package has no revocation, so expiry is the only thing that retires a
// node certificate. It has to actually be enforced.
func TestRemoteRefusesExpiredCertificate(t *testing.T) {
	p := newTestPKI(t)
	srv, entries := auditingServer(t, "node-b")
	addr := serveRemote(t, srv, p)

	if err := callAs(t, addr, p, p.client(t, "node-b", time.Nanosecond)); err == nil {
		t.Fatal("an expired certificate was accepted")
	}
	if len(*entries) != 0 {
		t.Errorf("expired peer reached the broker: %d audit entries", len(*entries))
	}
}

// The listener must not fall back to plaintext for a caller that simply does not
// speak TLS — the framing parser should never see those bytes.
func TestRemoteRefusesPlaintext(t *testing.T) {
	p := newTestPKI(t)
	srv, entries := auditingServer(t, "node-b")
	addr := serveRemote(t, srv, p)

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("not a tls client hello at all")); err != nil {
		t.Logf("write refused immediately: %v", err) // also an acceptable outcome
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("plaintext peer got a readable connection")
	}
	if len(*entries) != 0 {
		t.Errorf("plaintext peer reached the broker: %d audit entries", len(*entries))
	}
}

// An unconfigured remote endpoint must be a startup error, never an open one.
func TestServeRemoteRefusesEmptyAllowlist(t *testing.T) {
	srv, _ := auditingServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	err = srv.ServeRemote(context.Background(), ln)
	if err == nil {
		t.Fatal("ServeRemote accepted an empty allowlist")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("error = %v, want it to name the allowlist", err)
	}
}

// A local call still records only what npd said about itself: the node field is
// evidence the transport gathered, and there is none to gather on a socket.
func TestLocalCallHasNoNodeIdentity(t *testing.T) {
	if got := NodeFrom(context.Background()); got != "" {
		t.Errorf("NodeFrom on a bare context = %q, want empty", got)
	}
	ctx := withNode(context.Background(), "")
	if got := NodeFrom(ctx); got != "" {
		t.Errorf("NodeFrom after withNode(\"\") = %q, want empty", got)
	}
}
