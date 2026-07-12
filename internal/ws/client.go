package ws

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"

	"github.com/thisisnkp/heropanel/internal/auth"
)

// pingInterval keeps idle connections alive and detects dead peers.
const pingInterval = 30 * time.Second

// Client is one WebSocket connection.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	principal *auth.Principal
	send      chan []byte
	subs      map[string]struct{}
}

// clientMsg is the client->server message shape.
type clientMsg struct {
	Op       string   `json:"op"`       // subscribe | unsubscribe | ping
	Channels []string `json:"channels"` // for subscribe/unsubscribe
}

// readPump processes inbound client messages until the connection closes.
func (c *Client) readPump(ctx context.Context) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var msg clientMsg
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Op {
		case "subscribe":
			for _, ch := range msg.Channels {
				c.handleSubscribe(ctx, ch)
			}
		case "unsubscribe":
			for _, ch := range msg.Channels {
				c.hub.unsubscribe(c, ch)
				c.trySend(control("unsubscribed", ch, ""))
			}
		case "ping":
			c.trySend(control("pong", "", ""))
		}
	}
}

func (c *Client) handleSubscribe(ctx context.Context, channel string) {
	if !c.hub.authz.Authorize(ctx, c.principal, channel) {
		c.trySend(control("error", channel, "forbidden"))
		return
	}
	c.hub.subscribe(c, channel)
	c.trySend(control("subscribed", channel, ""))
}

// writePump writes queued messages and periodic pings until ctx is cancelled.
func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// trySend enqueues a message without blocking; a client that cannot keep up is
// closed (its read pump then unwinds and cleans up).
func (c *Client) trySend(b []byte) {
	select {
	case c.send <- b:
	default:
		_ = c.conn.Close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

// control builds a small server->client control message.
func control(typ, channel, code string) []byte {
	m := map[string]string{"type": typ}
	if channel != "" {
		m["channel"] = channel
	}
	if code != "" {
		m["code"] = code
	}
	b, _ := json.Marshal(m)
	return b
}
