// Command wsprobe is a test tool: it logs in, opens the realtime hub, subscribes
// to a channel, and prints the first event it receives (or times out). It is how
// the e2e proves the "live, pushed, not polled" contract for the monitor
// dashboard — a plain curl can complete the WebSocket upgrade but cannot speak
// the JSON subscribe/event frames, so this exists.
//
// It is a harness tool, not a shipped binary, so it lives under deploy/docker/e2e
// alongside the other e2e helpers.
//
// Usage: wsprobe <base-url> <email> <password> <channel> [timeout-seconds]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: wsprobe <base> <email> <password> <channel> [timeout-seconds]")
		os.Exit(2)
	}
	base, email, password, channel := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	timeout := 15 * time.Second
	if len(os.Args) > 5 {
		if n, err := strconv.Atoi(os.Args[5]); err == nil {
			timeout = time.Duration(n) * time.Second
		}
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Log in for a session cookie (the same-origin upgrade authenticates by it).
	loginBody := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	resp, err := client.Post(base+"/api/v1/auth/login", "application/json", strings.NewReader(loginBody))
	if err != nil {
		fail("login request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail("login failed: HTTP %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Dial the hub. The cookie jar carries the session cookie onto the upgrade.
	conn, _, err := websocket.Dial(ctx, base+"/api/v1/ws", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		fail("ws dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	sub, _ := json.Marshal(map[string]any{"op": "subscribe", "channels": []string{channel}})
	if err := conn.Write(ctx, websocket.MessageText, sub); err != nil {
		fail("subscribe: %v", err)
	}

	// Wait for the first event on our channel (ignore the subscribe ack).
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			fail("no %s event within %s: %v", channel, timeout, err)
		}
		var env struct {
			Type    string          `json:"type"`
			Channel string          `json:"channel"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		if env.Type == "event" && env.Channel == channel {
			fmt.Printf("EVENT %s %s\n", channel, string(env.Data))
			return
		}
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "wsprobe: "+format+"\n", a...)
	os.Exit(1)
}
