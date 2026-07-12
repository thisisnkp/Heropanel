//go:build ignore

// wsclient logs in, opens the realtime WebSocket, subscribes to the channel
// given as the first argument, and prints inbound messages. For local smoke
// tests. Run with: go run tools/wsclient/main.go job:<uid>
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: wsclient <channel>")
		os.Exit(2)
	}
	channel := os.Args[1]
	base := "http://127.0.0.1:18443"

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Post(base+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"a@h.io","password":"supersecret1"}`))
	if err != nil {
		fmt.Println("login error:", err)
		os.Exit(1)
	}
	_ = resp.Body.Close()

	u, _ := url.Parse(base)
	hdr := http.Header{}
	for _, c := range jar.Cookies(u) {
		hdr.Add("Cookie", c.Name+"="+c.Value)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:18443/api/v1/ws", &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		fmt.Println("ws dial error:", err)
		os.Exit(1)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	fmt.Println("connected; subscribing to", channel)
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"op":"subscribe","channels":["`+channel+`"]}`))

	for i := 0; i < 5; i++ {
		rctx, c := context.WithTimeout(ctx, 4*time.Second)
		_, data, err := conn.Read(rctx)
		c()
		if err != nil {
			break
		}
		fmt.Println("recv:", string(data))
	}
}
