// Package ws is hpd's realtime WebSocket hub. Authenticated clients subscribe to
// channels (e.g. "job:<uid>"); the hub bridges Redis Pub/Sub events (published by
// the job worker and other subsystems) out to subscribed browsers. Subscriptions
// are authorized per channel so a client only receives events for resources it
// may read (docs/01 §3.6, docs/04 §9).
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/thisisnkp/heropanel/internal/auth"
)

// pattern is the Redis Pub/Sub pattern the hub bridges. More families
// (site:*, metrics:*) are added here as they are introduced.
const redisPattern = "job:*"

// sendBuffer bounds each client's outbound queue; a client that falls behind is
// dropped rather than growing memory without bound.
const sendBuffer = 32

// Authorizer decides whether a principal may subscribe to a channel.
type Authorizer interface {
	Authorize(ctx context.Context, p *auth.Principal, channel string) bool
}

// AuthorizerFunc adapts a function to Authorizer.
type AuthorizerFunc func(ctx context.Context, p *auth.Principal, channel string) bool

func (f AuthorizerFunc) Authorize(ctx context.Context, p *auth.Principal, channel string) bool {
	return f(ctx, p, channel)
}

// Hub manages WebSocket clients and fans out channel events.
type Hub struct {
	rdb   *redis.Client // may be nil (no Redis bridge)
	authz Authorizer
	log   *slog.Logger

	mu       sync.RWMutex
	clients  map[*Client]struct{}
	channels map[string]map[*Client]struct{} // channel -> subscribers
}

// NewHub constructs a Hub. rdb may be nil.
func NewHub(rdb *redis.Client, authz Authorizer, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		rdb:      rdb,
		authz:    authz,
		log:      log,
		clients:  map[*Client]struct{}{},
		channels: map[string]map[*Client]struct{}{},
	}
}

// Run bridges Redis Pub/Sub to subscribed clients until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	if h.rdb == nil {
		<-ctx.Done()
		return
	}
	pubsub := h.rdb.PSubscribe(ctx, redisPattern)
	defer func() { _ = pubsub.Close() }()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			h.Publish(msg.Channel, []byte(msg.Payload))
		}
	}
}

// envelope is the server->client message shape.
type envelope struct {
	Channel string          `json:"channel"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data,omitempty"`
	Ts      string          `json:"ts"`
}

// Publish fans a payload out to every client subscribed to channel. It is also
// usable directly (e.g. for local, non-Redis events).
func (h *Hub) Publish(channel string, payload []byte) {
	h.mu.RLock()
	subs := h.channels[channel]
	targets := make([]*Client, 0, len(subs))
	for c := range subs {
		targets = append(targets, c)
	}
	h.mu.RUnlock()
	if len(targets) == 0 {
		return
	}

	env, _ := json.Marshal(envelope{
		Channel: channel,
		Type:    "event",
		Data:    json.RawMessage(payload),
		Ts:      time.Now().UTC().Format(time.RFC3339),
	})
	for _, c := range targets {
		c.trySend(env)
	}
}

// HasSubscribers reports whether any client is currently subscribed to a
// channel. The monitor's sampler calls it to gate its work: with nobody
// watching, there is nothing to compute and nothing to push.
func (h *Hub) HasSubscribers(channel string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[channel]) > 0
}

func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	for ch := range c.subs {
		if set := h.channels[ch]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.channels, ch)
			}
		}
	}
	h.mu.Unlock()
}

func (h *Hub) subscribe(c *Client, channel string) {
	h.mu.Lock()
	set := h.channels[channel]
	if set == nil {
		set = map[*Client]struct{}{}
		h.channels[channel] = set
	}
	set[c] = struct{}{}
	c.subs[channel] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unsubscribe(c *Client, channel string) {
	h.mu.Lock()
	if set := h.channels[channel]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.channels, channel)
		}
	}
	delete(c.subs, channel)
	h.mu.Unlock()
}

// Handler upgrades a request to a WebSocket connection. The caller must ensure
// the request is authenticated (a principal is in the context).
func (h *Hub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return // Accept already wrote the error
		}

		client := &Client{
			hub:       h,
			conn:      conn,
			principal: p,
			send:      make(chan []byte, sendBuffer),
			subs:      map[string]struct{}{},
		}
		h.addClient(client)
		defer func() {
			h.removeClient(client)
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go client.writePump(ctx)
		client.readPump(ctx) // blocks until the client disconnects
	}
}
