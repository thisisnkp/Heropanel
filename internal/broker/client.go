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

// DefaultTimeout bounds a capability call that does not appear in
// capabilityTimeouts. Most privileged operations are a handful of exec calls and
// should fail fast rather than hang a request.
const DefaultTimeout = 30 * time.Second

// capabilityTimeouts is how long the client waits for the capabilities that are
// legitimately slow.
//
// This exists because the two ends have to agree. The broker bounds every
// command it runs (a clone gets 5 minutes, a build 15, a mysqldump 60) and is
// the authority on when an operation has really gone wrong. The client's timeout
// is only a backstop against a broker that never answers at all — so for these
// capabilities it must be *longer* than the broker's own budget, or the client
// hangs up on work that is still legitimately running and the operation fails
// for no reason. A blanket 30s here silently made every real deploy and every
// non-trivial database export impossible.
//
// Keep each entry above the sum of the broker-side timeouts for that capability.
var capabilityTimeouts = map[string]time.Duration{
	// clone 5m + composer 15m + build 15m + filesystem steps.
	"git.deploy": 40 * time.Minute,
	// mysqldump 60m + gzip 30m.
	"db.export": 95 * time.Minute,
	// gunzip 30m + load 60m.
	"db.import": 95 * time.Minute,
	// cp -a 10m + chown -R 5m. A clone copies an entire document root; on a
	// site with a real WordPress tree that is minutes, not seconds.
	"site.copy_tree": 20 * time.Minute,
}

// TimeoutFor returns how long the client will wait for a capability.
func TimeoutFor(capability string) time.Duration {
	if d, ok := capabilityTimeouts[capability]; ok {
		return d
	}
	return DefaultTimeout
}

// Client is the concrete Gateway backed by the broker Unix socket.
type Client struct {
	socket string
	token  string
	log    *slog.Logger
	// dialer is overridable in tests (e.g. an in-memory pipe).
	dialer func(ctx context.Context) (net.Conn, error)
}

// NewClient constructs a Client for the broker at socket, authenticating with
// token.
func NewClient(socket, token string, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{socket: socket, token: token, log: log}
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
	// A caller that set its own deadline knows better than this table; only
	// impose one when it did not.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, TimeoutFor(capability))
		defer cancel()
	}

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
