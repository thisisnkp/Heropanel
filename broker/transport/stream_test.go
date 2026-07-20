package transport

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/audit"
	"github.com/thisisnkp/heropanel/broker/exec"
	"github.com/thisisnkp/heropanel/broker/policy"
	"github.com/thisisnkp/heropanel/pkg/brokerwire"
)

// The terminal upgrade is the one place where a *refused* request and a granted
// one take different shapes on the wire: a grant switches the connection to
// StreamFrames forever, a refusal must stay a plain Response. Getting that
// backwards would hand the client a stream to a session that was never opened,
// and the refusal would never be seen. The e2e (run-terminal.sh) proves the
// happy path with a real PTY and a real user; these pin the refusals, which
// decide before any PTY is allocated and so run on any platform.

func testServer(t *testing.T, pol policy.Policy) *Server {
	t.Helper()
	chain := audit.NewChain(func(audit.Entry) error { return nil })
	b := broker.New(broker.DefaultRegistry(), pol, chain, &exec.FakeRunner{}, nil)
	return NewServer(b, "test-token", slog.New(slog.DiscardHandler))
}

// runTerminal drives handleTerminal over an in-memory connection and returns the
// first frame the server wrote back.
func runTerminal(t *testing.T, s *Server, input any) brokerwire.Response {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		s.handleTerminal(ctx, server, brokerwire.Request{
			ID: "req-1", Capability: "terminal.open", Input: raw,
		})
		_ = server.Close()
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp brokerwire.Response
	if err := brokerwire.ReadFrame(client, &resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp
}

func TestTerminalRefusalIsAPlainResponseNotAStreamUpgrade(t *testing.T) {
	cases := map[string]any{
		// Each of these is refused by broker.OpenTerminal before a PTY exists.
		"root user":        map[string]any{"username": "root", "root": "/srv/heropanel/sites/1"},
		"invalid username": map[string]any{"username": "hps1; rm -rf /", "root": "/srv/heropanel/sites/1"},
	}
	s := testServer(t, policy.Default())
	for name, in := range cases {
		resp := runTerminal(t, s, in)
		if resp.OK {
			t.Errorf("%s: OK = true, want a refusal", name)
		}
		if resp.Stream {
			t.Errorf("%s: the connection must not upgrade to a stream on refusal", name)
		}
		if resp.Error == nil {
			t.Errorf("%s: a refusal must carry an error the client can show", name)
			continue
		}
		if resp.Error.Message == "" {
			t.Errorf("%s: the error must have a message; got %+v", name, resp.Error)
		}
	}
}

func TestTerminalRefusedWhenPolicyDisablesIt(t *testing.T) {
	pol := policy.Default()
	pol.Enabled["terminal.open"] = false
	resp := runTerminal(t, testServer(t, pol), map[string]any{
		"username": "hps1", "root": "/srv/heropanel/sites/1",
	})
	if resp.OK || resp.Stream {
		t.Fatalf("a policy-disabled terminal must be refused without upgrading; got %+v", resp)
	}
	if resp.Error == nil || resp.Error.Kind != "forbidden" {
		t.Errorf("want a forbidden error, got %+v", resp.Error)
	}
}

func TestTerminalRejectsMalformedInputBeforeReachingTheBroker(t *testing.T) {
	server, client := net.Pipe()
	defer func() {
		_ = server.Close()
		_ = client.Close()
	}()
	s := testServer(t, policy.Default())

	go func() {
		s.handleTerminal(context.Background(), server, brokerwire.Request{
			ID: "req-1", Capability: "terminal.open", Input: json.RawMessage(`{"cols": "not a number"}`),
		})
		_ = server.Close()
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp brokerwire.Response
	if err := brokerwire.ReadFrame(client, &resp); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.OK || resp.Stream {
		t.Fatalf("malformed input must be refused without upgrading; got %+v", resp)
	}
	if resp.Error == nil || resp.Error.Code != "bad_input" {
		t.Errorf("want the bad_input code, got %+v", resp.Error)
	}
}

// The response ID has to echo the request's, or a client multiplexing requests
// cannot tell which one was refused.
func TestTerminalResponseEchoesRequestID(t *testing.T) {
	resp := runTerminal(t, testServer(t, policy.Default()), map[string]any{"username": "root"})
	if resp.ID != "req-1" {
		t.Errorf("response ID = %q, want the request's", resp.ID)
	}
}
