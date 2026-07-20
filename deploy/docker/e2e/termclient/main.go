// Command termclient is an end-to-end test client for HeroPanel's web terminal.
//
// It is not shipped with the product — it lives under deploy/docker/e2e because
// the terminal is the one surface that cannot be exercised with curl: it is a
// WebSocket carrying binary PTY traffic. This client speaks the exact wire shape
// the browser does (binary frames for terminal bytes, JSON text frames for
// control), types a script into the shell, and prints what comes back, so a
// shell script can assert on it.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	var base, cookie, uid, script, cwd string
	var timeout, step time.Duration
	flag.StringVar(&base, "base", "127.0.0.1:18443", "host:port of hpd")
	flag.StringVar(&cookie, "cookie", "", "session cookie, e.g. hp_session=…")
	flag.StringVar(&uid, "site", "", "site uid")
	flag.StringVar(&cwd, "cwd", "", "starting directory, relative to the site home")
	flag.StringVar(&script, "script", "id -un; pwd; exit\n", "text typed into the shell")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "overall deadline")
	flag.DurationVar(&step, "step", 400*time.Millisecond, "pause between typed lines")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	u := fmt.Sprintf("ws://%s/api/v1/sites/%s/terminal?cols=100&rows=30", base, uid)
	if cwd != "" {
		u += "&cwd=" + cwd
	}
	hdr := http.Header{}
	if cookie != "" {
		hdr.Set("Cookie", cookie)
	}

	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		fmt.Printf("DIAL_FAILED status=%d err=%v\n", status, err)
		os.Exit(2)
	}
	defer func() { _ = conn.CloseNow() }()
	fmt.Println("CONNECTED")

	// Give the login shell a moment to emit its prompt before typing, so the
	// script is not swallowed by shell startup.
	time.Sleep(500 * time.Millisecond)

	// Typing happens a line at a time, with a pause between lines, because that
	// is what a person does — and because some behaviour only exists in that
	// shape. Session recording redacts input typed while the terminal has echo
	// off, and the terminal only turns echo off *in response to* a previous line
	// (`stty -echo`, a sudo prompt). Sending the whole script in one write would
	// deliver the secret before the prompt that hides it had happened at all,
	// which no real session does.
	for _, line := range splitKeepNewline(script) {
		if err := conn.Write(ctx, websocket.MessageBinary, []byte(line)); err != nil {
			fmt.Printf("WRITE_FAILED err=%v\n", err)
			os.Exit(3)
		}
		time.Sleep(step)
	}

	var out strings.Builder
	for {
		typ, data, rerr := conn.Read(ctx)
		if rerr != nil {
			break // the server closed after the shell exited, or we hit the deadline
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
			continue
		}
		// A JSON control frame: exit or error.
		fmt.Printf("CONTROL %s\n", strings.TrimSpace(string(data)))
		if strings.Contains(string(data), `"exit"`) || strings.Contains(string(data), `"error"`) {
			break
		}
	}

	fmt.Println("---- TERMINAL OUTPUT ----")
	fmt.Println(out.String())
	fmt.Println("---- END ----")
}

// splitKeepNewline breaks a script into lines, keeping each line's terminator so
// the shell still sees a complete command.
func splitKeepNewline(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			if s != "" {
				out = append(out, s)
			}
			return out
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
}
