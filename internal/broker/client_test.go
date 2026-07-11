package broker_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	brokerd "github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/broker/transport"
	client "github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

const testToken = "test-broker-token"

// harness wires a real broker + transport server to a real client over an
// in-memory pipe, so the full handshake + framing + dispatch path is exercised
// without an OS socket. Returns the client and the fake runner for assertions.
func harness(t *testing.T, pol policy.Policy) (*client.Client, *exec.FakeRunner) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	fake := &exec.FakeRunner{Result: exec.Result{ExitCode: 0}}
	b := brokerd.New(brokerd.DefaultRegistry(), pol, audit.NewChain(nil), fake, log)
	srv := transport.NewServer(b, testToken, log)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	c := client.NewClient("unused", testToken, log)
	// Override the dialer: each call gets a fresh pipe whose server end is served
	// by the transport server (matching the dial-per-call client behavior).
	client.SetDialer(c, func(_ context.Context) (net.Conn, error) {
		serverConn, clientConn := net.Pipe()
		go srv.ServeConn(ctx, serverConn)
		return clientConn, nil
	})
	return c, fake
}

func TestClientServerInvokeSuccess(t *testing.T) {
	c, fake := harness(t, policy.Default())

	out, err := c.Invoke(context.Background(), "service.restart", map[string]string{"service": "mariadb"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if out["restarted"] != true {
		t.Fatalf("unexpected response: %+v", out)
	}
	last, ok := fake.Last()
	if !ok || last.Path != "/usr/bin/systemctl" {
		t.Fatalf("expected systemctl to run, got %+v", last)
	}
}

func TestClientServerForbiddenPropagates(t *testing.T) {
	c, _ := harness(t, policy.Default())

	_, err := c.Invoke(context.Background(), "service.restart", map[string]string{"service": "sshd"})
	if !errx.IsKind(err, errx.KindForbidden) {
		t.Fatalf("want forbidden across the wire, got %v", err)
	}
}

func TestClientServerUnknownCapability(t *testing.T) {
	pol := policy.Default()
	pol.Enabled["ghost.op"] = true // enabled but unregistered
	c, _ := harness(t, pol)

	_, err := c.Invoke(context.Background(), "ghost.op", nil)
	if !errx.IsKind(err, errx.KindNotFound) {
		t.Fatalf("want not_found across the wire, got %v", err)
	}
}

func TestClientBadTokenRejected(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := brokerd.New(brokerd.DefaultRegistry(), policy.Default(), audit.NewChain(nil), &exec.FakeRunner{}, log)
	srv := transport.NewServer(b, "the-real-token", log)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	c := client.NewClient("unused", "WRONG-token", log)
	client.SetDialer(c, func(_ context.Context) (net.Conn, error) {
		serverConn, clientConn := net.Pipe()
		go srv.ServeConn(ctx, serverConn)
		return clientConn, nil
	})

	if err := c.Health(context.Background()); !errx.IsKind(err, errx.KindUnauthorized) {
		t.Fatalf("want unauthorized for bad token, got %v", err)
	}
}
