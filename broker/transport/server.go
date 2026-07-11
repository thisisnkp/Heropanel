// Package transport is hp-broker's socket server. It authenticates each
// connection (peer credentials + shared token), then dispatches framed requests
// to the capability broker. See ADR-0007 and docs/05.
package transport

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/brokerwire"
	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Server serves the broker over a stream listener.
type Server struct {
	broker *broker.Broker
	token  string
	log    *slog.Logger

	// AllowedUID, when >= 0 and the platform supports SO_PEERCRED, requires the
	// connecting peer's uid to match (the heropanel user). -1 disables the check
	// (relying on the token and socket file mode).
	AllowedUID int
}

// NewServer constructs a Server. token must be non-empty.
func NewServer(b *broker.Broker, token string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{broker: b, token: token, log: log, AllowedUID: -1}
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
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
		if err := s.authorizePeer(conn); err != nil {
			s.log.Warn("broker: rejected peer", "err", err)
			_ = conn.Close()
			continue
		}
		go s.ServeConn(ctx, conn)
	}
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
		resp := s.dispatch(ctx, req)
		if err := brokerwire.WriteFrame(conn, resp); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(ctx context.Context, req brokerwire.Request) brokerwire.Response {
	out, err := s.broker.Invoke(ctx, broker.Request{
		Capability: req.Capability,
		Input:      req.Input,
		Actor: capability.Actor{
			UserID:        req.Actor.UserID,
			IP:            req.Actor.IP,
			CorrelationID: req.Actor.CorrelationID,
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
