package ws_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/thisisnkp/heropanel/internal/auth"
	"github.com/thisisnkp/heropanel/internal/ws"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// setup starts a hub (with miniredis) behind an httptest server that injects a
// principal, and returns the ws URL and the redis client (for publishing).
func setup(t *testing.T, authz ws.Authorizer) (string, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	hub := ws.NewHub(rdb, authz, discard())
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	handler := hub.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithPrincipal(r.Context(), &auth.Principal{UserID: 1})
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))

	t.Cleanup(func() { cancel(); srv.Close(); _ = rdb.Close(); mr.Close() })
	return "ws" + strings.TrimPrefix(srv.URL, "http"), rdb
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(websocket.StatusNormalClosure, "") })
	return c
}

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSubscribeAndReceiveEvent(t *testing.T) {
	allowAll := ws.AuthorizerFunc(func(context.Context, *auth.Principal, string) bool { return true })
	url, rdb := setup(t, allowAll)
	c := dial(t, url)

	// Read all inbound messages on a background goroutine.
	msgs := make(chan map[string]any, 8)
	go func() {
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				close(msgs)
				return
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			msgs <- m
		}
	}()

	send(t, c, map[string]any{"op": "subscribe", "channels": []string{"job:abc"}})

	// First message is the subscribe acknowledgement.
	ack := <-msgs
	if ack["type"] != "subscribed" || ack["channel"] != "job:abc" {
		t.Fatalf("unexpected ack: %+v", ack)
	}

	// Publish repeatedly to absorb the Redis subscription-establishment race,
	// until the event is delivered.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = rdb.Publish(context.Background(), "job:abc", `{"progress":42,"status":"running"}`).Err()
		select {
		case evt, ok := <-msgs:
			if !ok {
				t.Fatal("connection closed before event")
			}
			if evt["channel"] != "job:abc" || evt["type"] != "event" {
				continue // a stray message; keep waiting
			}
			data, _ := evt["data"].(map[string]any)
			if data["progress"] != float64(42) {
				t.Fatalf("unexpected event data: %+v", evt)
			}
			return // success
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("did not receive the published event in time")
}

func TestSubscribeDeniedByAuthorizer(t *testing.T) {
	denyAll := ws.AuthorizerFunc(func(context.Context, *auth.Principal, string) bool { return false })
	url, _ := setup(t, denyAll)
	c := dial(t, url)

	send(t, c, map[string]any{"op": "subscribe", "channels": []string{"job:secret"}})

	_, data, err := c.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["type"] != "error" || m["code"] != "forbidden" {
		t.Fatalf("expected forbidden error, got %+v", m)
	}
}

func TestPingPong(t *testing.T) {
	allowAll := ws.AuthorizerFunc(func(context.Context, *auth.Principal, string) bool { return true })
	url, _ := setup(t, allowAll)
	c := dial(t, url)

	send(t, c, map[string]any{"op": "ping"})
	_, data, err := c.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["type"] != "pong" {
		t.Fatalf("expected pong, got %+v", m)
	}
}
