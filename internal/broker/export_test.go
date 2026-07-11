package broker

import (
	"context"
	"net"
)

// SetDialer overrides the connection dialer for tests (e.g. an in-memory pipe).
func SetDialer(c *Client, d func(ctx context.Context) (net.Conn, error)) {
	c.dialer = d
}
