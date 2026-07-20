package broker

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/thisisnkp/heropanel/pkg/brokerwire"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Streaming capabilities (today: the interactive terminal).
//
// A normal Invoke is one request, one response, connection closed. A stream
// keeps the connection open and exchanges StreamFrames in both directions for
// the life of the session, so it gets its own dedicated connection rather than
// sharing one with request/response traffic.

// Stream is an upgraded broker connection. It is not safe for concurrent Send
// from multiple goroutines, nor concurrent Recv; the usual shape is one
// goroutine pumping each direction.
type Stream interface {
	Send(f brokerwire.StreamFrame) error
	Recv() (brokerwire.StreamFrame, error)
	Close() error
}

// StreamGateway is the subset of the broker client that can open a stream. It
// is separate from Gateway so that only the terminal depends on it.
type StreamGateway interface {
	OpenStream(ctx context.Context, capability string, input any) (Stream, error)
}

// streamConn is the concrete Stream over the broker's Unix socket.
type streamConn struct {
	conn net.Conn
}

func (s *streamConn) Send(f brokerwire.StreamFrame) error {
	return brokerwire.WriteFrame(s.conn, f)
}

func (s *streamConn) Recv() (brokerwire.StreamFrame, error) {
	var f brokerwire.StreamFrame
	err := brokerwire.ReadFrame(s.conn, &f)
	return f, err
}

func (s *streamConn) Close() error { return s.conn.Close() }

// streamHandshakeTimeout bounds only the dial + handshake + accept exchange. It
// deliberately does *not* bound the session: an interactive terminal is idle for
// minutes at a time, and a read deadline would kill it mid-use.
const streamHandshakeTimeout = 10 * time.Second

// OpenStream dials the broker, authenticates, and upgrades the connection to a
// stream for the given capability. The caller owns the returned Stream and must
// Close it — closing is what tells the broker to tear the session down.
func (c *Client) OpenStream(ctx context.Context, capability string, input any) (Stream, error) {
	// Handshake under a short deadline, then clear it for the session itself.
	hctx, cancel := context.WithTimeout(ctx, streamHandshakeTimeout)
	defer cancel()

	conn, err := c.connectAndHandshake(hctx)
	if err != nil {
		return nil, err
	}
	closeOnErr := func(e error) (Stream, error) {
		_ = conn.Close()
		return nil, e
	}

	var raw json.RawMessage
	if input != nil {
		b, mErr := json.Marshal(input)
		if mErr != nil {
			return closeOnErr(errx.Wrap(mErr, errx.KindValidation, "bad_input", "Could not encode broker input."))
		}
		raw = b
	}

	req := brokerwire.Request{ID: idgen.NewULID(), Capability: capability, Input: raw}
	if err := brokerwire.WriteFrame(conn, req); err != nil {
		return closeOnErr(errx.Wrap(err, errx.KindUnavailable, "broker_write_failed", "Could not open the broker stream."))
	}
	var resp brokerwire.Response
	if err := brokerwire.ReadFrame(conn, &resp); err != nil {
		return closeOnErr(errx.Wrap(err, errx.KindUnavailable, "broker_read_failed", "Could not read the broker response."))
	}
	if !resp.OK {
		return closeOnErr(errFromWire(resp.Error))
	}
	if !resp.Stream {
		return closeOnErr(errx.New(errx.KindInternal, "not_a_stream",
			"The broker did not upgrade the connection to a stream."))
	}

	// The session may sit idle indefinitely; drop the handshake deadline.
	_ = conn.SetDeadline(time.Time{})
	return &streamConn{conn: conn}, nil
}

// ensure Client satisfies StreamGateway.
var _ StreamGateway = (*Client)(nil)
