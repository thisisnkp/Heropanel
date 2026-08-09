// Package transport is np-broker's connection server. It authenticates each
// connection, then dispatches framed requests to the capability broker.
//
// There are two ways in, and they authenticate differently because they can:
//
//   - [Server.Serve] takes the local Unix socket, where SO_PEERCRED lets the
//     kernel attest the caller's uid.
//   - [Server.ServeRemote] takes a network listener, where nothing attests
//     anything, so a verified TLS client certificate has to.
//
// They are separate methods rather than one method that inspects the connection
// on purpose. A single entry point would have to decide "is this local?" per
// connection, and every wrong answer fails open — which is exactly what the
// original peer check did when handed a non-Unix listener: peerCredSupported
// went false, the uid check silently evaporated, and a network endpoint would
// have been protected by the shared token alone. Choosing the trust model at the
// listener makes that mistake unrepresentable.
//
// See ADR-0007, ADR-0008 and docs/05, docs/27.
package transport

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/thisisnkp/nexpanel/broker"
	"github.com/thisisnkp/nexpanel/broker/capability"
	"github.com/thisisnkp/nexpanel/pkg/brokerwire"
	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Server serves the broker over a stream listener.
type Server struct {
	broker *broker.Broker
	token  string
	log    *slog.Logger

	// AllowedUID, when >= 0 and the platform supports SO_PEERCRED, requires the
	// connecting peer's uid to match (the nexpanel user). -1 disables the check
	// (relying on the token and socket file mode).
	AllowedUID int

	// AllowedNodes lists the certificate common names permitted to call over a
	// remote listener. A valid certificate from the panel's CA is necessary and
	// not sufficient: the CA says "this is node X", the allowlist says whether
	// node X may drive root on this host. ServeRemote refuses to start when it is
	// empty, so an unconfigured remote endpoint is a startup error rather than an
	// open one.
	AllowedNodes []string
}

// NewServer constructs a Server. token must be non-empty.
func NewServer(b *broker.Broker, token string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{broker: b, token: token, log: log, AllowedUID: -1}
}

// Serve accepts connections on the local Unix socket until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	return s.serve(ctx, ln, s.acceptLocal)
}

// ServeRemote accepts connections on a network listener until ctx is cancelled.
// ln must be a TLS listener configured by [ServerTLS]; a connection that is not
// TLS, or whose client certificate did not verify against the configured CA, is
// closed before a single frame is read.
func (s *Server) ServeRemote(ctx context.Context, ln net.Listener) error {
	if len(s.AllowedNodes) == 0 {
		return errors.New("broker: refusing to serve remotely with an empty node allowlist")
	}
	return s.serve(ctx, ln, s.acceptRemote)
}

// accepted carries what an accept-time check established about a connection.
type accepted struct {
	node string // attested node identity; empty for local calls
}

func (s *Server) serve(ctx context.Context, ln net.Listener, check func(net.Conn) (accepted, error)) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return err
		}
		info, err := check(conn)
		if err != nil {
			s.log.Warn("broker: rejected peer", "err", err, "remote", conn.RemoteAddr().String())
			_ = conn.Close()
			continue
		}
		go s.ServeConn(withNode(ctx, info.node), conn)
	}
}

func (s *Server) acceptLocal(conn net.Conn) (accepted, error) {
	return accepted{}, s.authorizePeer(conn)
}

// acceptRemote completes the TLS handshake and authorizes the certificate.
//
// The handshake is forced here rather than left to the first Read so that an
// unauthorized peer is rejected at accept time, before any framing code — the
// parser the root broker most wants to keep away from strangers — sees its
// bytes.
func (s *Server) acceptRemote(conn net.Conn) (accepted, error) {
	hs, ok := conn.(interface {
		HandshakeContext(context.Context) error
	})
	if !ok {
		return accepted{}, errors.New("remote listener did not yield a TLS connection")
	}
	if err := hs.HandshakeContext(context.Background()); err != nil {
		return accepted{}, fmt.Errorf("tls handshake: %w", err)
	}
	node, err := peerNode(conn)
	if err != nil {
		return accepted{}, err
	}
	for _, allowed := range s.AllowedNodes {
		if allowed == node {
			return accepted{node: node}, nil
		}
	}
	return accepted{}, fmt.Errorf("node %q is not in the allowlist", node)
}

// authorizePeer enforces the OS-credential check (Linux) before the handshake.
func (s *Server) authorizePeer(conn net.Conn) error {
	if s.AllowedUID < 0 || !peerCredSupported {
		return nil
	}
	uid, _, ok := peerCred(conn)
	if !ok {
		return errors.New("could not read peer credentials")
	}
	if uid != s.AllowedUID {
		return errors.New("peer uid not permitted")
	}
	return nil
}

// ServeConn handles one authenticated connection: token handshake, then a loop
// of request/response frames. It is exported so tests can drive it over a pipe.
func (s *Server) ServeConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var hello brokerwire.Hello
	if err := brokerwire.ReadFrame(conn, &hello); err != nil {
		return
	}
	if subtle.ConstantTimeCompare([]byte(hello.Token), []byte(s.token)) != 1 {
		_ = brokerwire.WriteFrame(conn, brokerwire.HelloAck{OK: false, Error: "unauthorized"})
		s.log.Warn("broker: handshake rejected (bad token)")
		return
	}
	if err := brokerwire.WriteFrame(conn, brokerwire.HelloAck{OK: true}); err != nil {
		return
	}

	for {
		var req brokerwire.Request
		if err := brokerwire.ReadFrame(conn, &req); err != nil {
			return // EOF or read error → close
		}
		// An interactive terminal is a stream, not a result. It takes over the
		// connection for its lifetime and never returns to this loop.
		if req.Capability == broker.CapTerminalOpen {
			s.handleTerminal(ctx, conn, req)
			return
		}
		// A shell inside a container is the same shape: a PTY on a connection
		// that never returns to this loop.
		if req.Capability == broker.CapContainerExec {
			s.handleContainerExec(ctx, conn, req)
			return
		}
		// Following a container's logs is a one-way stream on a connection that
		// likewise never returns to this loop.
		if req.Capability == broker.CapContainerLogsFollow {
			s.handleContainerLogs(ctx, conn, req)
			return
		}
		resp := s.dispatch(ctx, req)
		if err := brokerwire.WriteFrame(conn, resp); err != nil {
			return
		}
	}
}

// nodeKey carries the attested node identity from accept time to dispatch.
//
// It travels in the context rather than on the wire type because that is the
// only way it can be trusted: brokerwire.Request is what the caller sent, and a
// caller that could name its own node would make the certificate check
// decorative.
type nodeKey struct{}

func withNode(ctx context.Context, node string) context.Context {
	if node == "" {
		return ctx
	}
	return context.WithValue(ctx, nodeKey{}, node)
}

// NodeFrom reports the attested node identity on ctx, if the call arrived over a
// remote listener. Empty means the local socket.
func NodeFrom(ctx context.Context) string {
	node, _ := ctx.Value(nodeKey{}).(string)
	return node
}

func (s *Server) dispatch(ctx context.Context, req brokerwire.Request) brokerwire.Response {
	out, err := s.broker.Invoke(ctx, broker.Request{
		Capability: req.Capability,
		Input:      req.Input,
		Actor: capability.Actor{
			UserID:        req.Actor.UserID,
			IP:            req.Actor.IP,
			CorrelationID: req.Actor.CorrelationID,
			Node:          NodeFrom(ctx),
		},
	})
	if err != nil {
		return brokerwire.Response{ID: req.ID, OK: false, Error: toWireError(err)}
	}
	return brokerwire.Response{ID: req.ID, OK: true, Data: out.Data}
}

func toWireError(err error) *brokerwire.WireError {
	if e, ok := errx.As(err); ok {
		return &brokerwire.WireError{Kind: string(e.Kind), Code: e.Code, Message: e.Message}
	}
	return &brokerwire.WireError{
		Kind:    string(errx.KindInternal),
		Code:    "internal_error",
		Message: "An unexpected error occurred.",
	}
}
