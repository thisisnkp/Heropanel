package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/thisisnkp/heropanel/internal/audit"
	"github.com/thisisnkp/heropanel/internal/auth"
	brokerclient "github.com/thisisnkp/heropanel/internal/broker"
	"github.com/thisisnkp/heropanel/internal/terminal"
	"github.com/thisisnkp/heropanel/pkg/brokerwire"
)

// The web terminal's HTTP edge: a WebSocket that bridges the browser to a
// PTY the root broker is hosting as the site's Linux user.
//
// Wire shape. Terminal payload travels as **binary** frames in both directions:
// PTY output is arbitrary bytes and a 32 KiB read can land mid-UTF-8-sequence,
// so encoding it as a JSON string would corrupt the stream. Control messages
// (resize, exit) travel as JSON **text** frames, where they cannot be confused
// with payload. xterm.js buffers partial escape/UTF-8 sequences itself, so
// splitting output across frames is safe.

// terminalControl is a JSON text frame in either direction.
type terminalControl struct {
	Type     string `json:"type"`                // resize | exit | error
	Cols     uint16 `json:"cols,omitempty"`      // resize
	Rows     uint16 `json:"rows,omitempty"`      // resize
	ExitCode int    `json:"exit_code,omitempty"` // exit
	Message  string `json:"message,omitempty"`   // error
}

// terminalHandler upgrades to a WebSocket and runs an interactive session.
// Gated by "terminal.use".
func terminalHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := chi.URLParam(r, "uid")
		cwd := r.URL.Query().Get("cwd")
		cols := uint16(termDimension(r.URL.Query().Get("cols"), 0))
		rows := uint16(termDimension(r.URL.Query().Get("rows"), 0))

		// Handing someone a shell on a customer's site is the most powerful thing
		// this API does. It is a GET, so the edge would not audit it by default —
		// force a row, and record which Linux account the session ran as.
		audit.Force(r.Context())
		audit.AddDetail(r.Context(), "cwd", cwd)

		// Open the broker stream *before* upgrading. A failure here (no broker, an
		// unprovisioned site, policy refusal) then still becomes a normal JSON
		// error response instead of an opaque WebSocket close.
		stream, ref, err := d.Terminal.Open(r.Context(), uid, cwd, cols, rows)
		if err != nil {
			writeError(w, r, err)
			return
		}
		defer func() { _ = stream.Close() }()
		audit.AddDetail(r.Context(), "linux_user", ref.LinuxUser)

		// Start recording, if it is configured. A failure here is logged and the
		// session continues: the shell is what the operator asked for, and refusing
		// it because the audit artifact could not be opened would turn a full disk
		// into an outage. The audit row records whether a recording exists, so a
		// session without one is visible rather than indistinguishable.
		var rec *terminal.Session
		if d.Recordings.Enabled() {
			p, _ := auth.FromContext(r.Context())
			meta := terminal.SessionMeta{
				SiteID: ref.ID, SiteUID: ref.UID,
				SystemUser: ref.LinuxUser,
				ActorIP:    clientIP(r),
				Cols:       cols, Rows: rows,
			}
			if p != nil {
				meta.ActorUserID, meta.ActorEmail = p.UserID, p.Email
			}
			started, rErr := d.Recordings.Begin(r.Context(), meta)
			if rErr != nil {
				d.Logger.Error("terminal: could not start session recording",
					"err", rErr, "site", uid)
				audit.AddDetail(r.Context(), "recorded", false)
			} else {
				rec = started
				audit.AddDetail(r.Context(), "recorded", true)
				audit.AddDetail(r.Context(), "recording_uid", rec.UID())
			}
		} else {
			audit.AddDetail(r.Context(), "recorded", false)
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			if rec != nil {
				_ = rec.End(context.WithoutCancel(r.Context()))
			}
			return // Accept already responded
		}
		defer func() { _ = conn.CloseNow() }()
		// Terminal input is keystrokes and pastes, never bulk data.
		conn.SetReadLimit(1 << 20)

		// The session outlives the request's own context (which is cancelled when
		// the handler returns), so give the pumps their own, cancelled when either
		// direction ends.
		ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
		defer cancel()
		if rec != nil {
			defer func() { _ = rec.End(ctx) }()
		}

		go pumpBrokerToWS(ctx, cancel, conn, stream, rec)
		pumpWSToBroker(ctx, conn, stream, rec)
	}
}

// pumpBrokerToWS forwards PTY output and the final exit status to the browser,
// recording the output as it passes. rec may be nil (recording disabled).
func pumpBrokerToWS(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, stream brokerclient.Stream, rec *terminal.Session) {
	defer cancel()
	for {
		f, err := stream.Recv()
		if err != nil {
			return // broker stream closed → session over
		}
		switch f.Kind {
		case brokerwire.StreamEcho:
			// The PTY stopped (or resumed) echoing. Only the broker can see this,
			// and the recorder needs it to redact input typed at a password prompt.
			// It is not forwarded to the browser: xterm learns about echo from the
			// escape sequences in the output stream itself.
			if rec != nil && f.Echo != nil {
				rec.SetEcho(*f.Echo)
			}
		case brokerwire.StreamOut:
			if rec != nil {
				rec.Output(f.Data)
			}
			if err := conn.Write(ctx, websocket.MessageBinary, f.Data); err != nil {
				return
			}
		case brokerwire.StreamExit:
			writeControl(ctx, conn, terminalControl{Type: "exit", ExitCode: f.ExitCode})
			_ = conn.Close(websocket.StatusNormalClosure, "session ended")
			return
		case brokerwire.StreamError:
			writeControl(ctx, conn, terminalControl{Type: "error", Message: f.Error})
			_ = conn.Close(websocket.StatusInternalError, "session failed")
			return
		}
	}
}

// pumpWSToBroker forwards keystrokes and resizes to the PTY. It returns when the
// browser disconnects, which closes the stream and kills the shell.
func pumpWSToBroker(ctx context.Context, conn *websocket.Conn, stream brokerclient.Stream, rec *terminal.Session) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return // client gone or context cancelled
		}
		switch typ {
		case websocket.MessageBinary:
			// Recorded *before* forwarding, so the transcript reflects what was
			// typed even if the write to the PTY then fails. The recorder redacts
			// this itself whenever the terminal is not echoing.
			if rec != nil {
				rec.Input(data)
			}
			if err := stream.Send(brokerwire.StreamFrame{Kind: brokerwire.StreamIn, Data: data}); err != nil {
				return
			}
		case websocket.MessageText:
			var c terminalControl
			if err := json.Unmarshal(data, &c); err != nil {
				continue // ignore malformed control frames rather than dropping the session
			}
			if c.Type == "resize" {
				if rec != nil {
					rec.Resize(c.Cols, c.Rows)
				}
				if err := stream.Send(brokerwire.StreamFrame{
					Kind: brokerwire.StreamResize, Cols: c.Cols, Rows: c.Rows,
				}); err != nil {
					return
				}
			}
		}
	}
}

func writeControl(ctx context.Context, conn *websocket.Conn, c terminalControl) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, b)
}

// termDimension parses a window dimension, falling back to def when the value is
// absent or implausible (a terminal is never 0 or 100000 columns wide).
func termDimension(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 1000 {
		return def
	}
	return n
}
