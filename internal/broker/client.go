// Package broker is npd's client to the privileged np-broker daemon. Services
// call Gateway.Invoke to request privileged operations; the client dials the
// broker's Unix socket, performs the token handshake, and exchanges one framed
// request/response per call (ADR-0007).
package broker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/brokerwire"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
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
	// A rootkit/system audit walks the whole host: rkhunter's propupd + check
	// and lynis's audit are minutes, not seconds.
	"audit.scan": 15 * time.Minute,
	// AIDE hashes the watched tree; init/check are minutes on a real host.
	"fim.init":  10 * time.Minute,
	"fim.check": 10 * time.Minute,
	// A malware scan walks a whole site tree; clamscan and maldet are both
	// minutes on a real WordPress install, not seconds.
	"malware.scan": 30 * time.Minute,
	"maldet.scan":  30 * time.Minute,
	// maldet ships no distribution package: download, extract, then its own
	// install.sh, which compiles nothing but does a lot of filesystem work.
	"maldet.install": 20 * time.Minute,
	// Pulling a fresh signature pack from rfxn.com.
	"maldet.update": 10 * time.Minute,
	// First-run setup provisioning: apt/dnf update + install of a webserver, a
	// database engine, and optionally BIND/Postfix/Dovecot. On a fresh box that
	// is many minutes of downloads, so the client backstop must sit above the
	// broker's own per-install budget (5m update + 10m per component install).
	"system.provision": 30 * time.Minute,
}

// TimeoutFor returns how long the client will wait for a capability.
func TimeoutFor(capability string) time.Duration {
	if d, ok := capabilityTimeouts[capability]; ok {
		return d
	}
	return DefaultTimeout
}

// Client is the concrete Gateway backed by a broker connection — the local Unix
// socket by default, or a remote broker over mutual TLS.
type Client struct {
	// addr is the Unix socket path, or host:port when this client is remote.
	addr string
	// remote records that this client crosses a network. It is reported in logs
	// and health errors because "the broker is unreachable" means something very
	// different when the broker is a socket on this box and when it is a machine
	// somewhere else.
	remote bool
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
	c := &Client{addr: socket, token: token, log: log}
	c.dialer = c.dialUnix
	return c
}

// NewTLSClient constructs a Client for a broker on another host, authenticating
// with a client certificate as well as the shared token.
//
// The certificate is what the far end authorizes on; the token stays because it
// is a second, independent secret. A CA key that leaked would otherwise be
// enough on its own to mint an identity and drive root on every node in the
// installation.
func NewTLSClient(addr, token string, cfg *tls.Config, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg == nil {
		return nil, errx.New(errx.KindValidation, "broker_tls_missing",
			"A remote broker requires a TLS configuration.")
	}
	c := &Client{addr: addr, remote: true, token: token, log: log}
	c.dialer = func(ctx context.Context) (net.Conn, error) {
		d := tls.Dialer{Config: cfg}
		return d.DialContext(ctx, "tcp", addr)
	}
	return c, nil
}

func (c *Client) dialUnix(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", c.addr)
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
