// Package broker is hpd's client to the privileged hp-broker daemon. Services
// call Gateway.Invoke to request privileged operations; the client dials the
// broker's Unix socket, performs the token handshake, and exchanges one framed
// request/response per call (ADR-0007).
package broker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/thisisnkp/heropanel/pkg/brokerwire"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Gateway is the interface services depend on to run privileged operations.
type Gateway interface {
	Invoke(ctx context.Context, capability string, input any) (map[string]any, error)
	Health(ctx context.Context) error
}

// Client is the concrete Gateway backed by the broker Unix socket.
type Client struct {
	socket  string
	token   string
	log     *slog.Logger
	timeout time.Duration
	// dialer is overridable in tests (e.g. an in-memory pipe).
	dialer func(ctx context.Context) (net.Conn, error)
}

// NewClient constructs a Client for the broker at socket, authenticating with
// token.
func NewClient(socket, token string, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{socket: socket, token: token, log: log, timeout: 30 * time.Second}
	c.dialer = c.dialUnix
	return c
}

func (c *Client) dialUnix(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", c.socket)
}

// Invoke runs a privileged capability with a JSON-serializable input and returns
// its result data. Errors from the broker are returned as typed errx errors.
func (c *Client) Invoke(ctx context.Context, capability string, input any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	conn, err := c.connectAndHandshake(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	var raw json.RawMessage
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, errx.Wrap(err, errx.KindValidation, "bad_input", "Could not encode broker input.")
		}
		raw = b
	}

	req := brokerwire.Request{ID: idgen.NewULID(), Capability: capability, Input: raw}
	if err := brokerwire.WriteFrame(conn, req); err != nil {
		return nil, errx.Wrap(err, errx.KindUnavailable, "broker_write_failed", "Could not send request to the broker.")
	}
	var resp brokerwire.Response
	if err := brokerwire.ReadFrame(conn, &resp); err != nil {
		return nil, errx.Wrap(err, errx.KindUnavailable, "broker_read_failed", "Could not read the broker response.")
	}
	if !resp.OK {
		return nil, errFromWire(resp.Error)
	}
	return resp.Data, nil
}

// Health verifies the broker is reachable and accepts our token (dial +
// handshake, no capability invoked).
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := c.connectAndHandshake(ctx)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Client) connectAndHandshake(ctx context.Context) (net.Conn, error) {
	conn, err := c.dialer(ctx)
	if err != nil {
		return nil, errx.Wrap(err, errx.KindUnavailable, "broker_unavailable", "The broker is not reachable.")
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if err := brokerwire.WriteFrame(conn, brokerwire.Hello{Token: c.token}); err != nil {
		_ = conn.Close()
		return nil, errx.Wrap(err, errx.KindUnavailable, "broker_handshake_failed", "Broker handshake failed.")
	}
	var ack brokerwire.HelloAck
	if err := brokerwire.ReadFrame(conn, &ack); err != nil {
		_ = conn.Close()
		return nil, errx.Wrap(err, errx.KindUnavailable, "broker_handshake_failed", "Broker handshake failed.")
	}
	if !ack.OK {
		_ = conn.Close()
		return nil, errx.New(errx.KindUnauthorized, "broker_unauthorized", "The broker rejected our credentials.")
	}
	return conn, nil
}

func errFromWire(we *brokerwire.WireError) error {
	if we == nil {
		return errx.New(errx.KindInternal, "internal_error", "An unexpected error occurred.")
	}
	return errx.New(errx.Kind(we.Kind), we.Code, we.Message)
}

// ensure Client satisfies Gateway.
var _ Gateway = (*Client)(nil)
