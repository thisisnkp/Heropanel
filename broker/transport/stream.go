package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/thisisnkp/heropanel/broker"
	"github.com/thisisnkp/heropanel/broker/capability"
	"github.com/thisisnkp/heropanel/pkg/brokerwire"
)

// Streaming (interactive terminal) support.
//
// A terminal cannot be a request/response capability, so the connection is
// *upgraded*: the client sends a normal Request for terminal.open, and if the
// broker accepts it the connection stops carrying Responses and starts carrying
// StreamFrames in both directions until either side closes. Everything that
// guards a normal request — peer-credential check, token handshake, policy,
// audit — has already happened by the time we get here.

// ptyReadChunk bounds one read from the terminal. It is far below the 1 MiB
// frame cap, so a burst of output (a `cat` of something large) is delivered as a
// series of frames rather than failing to encode.
const ptyReadChunk = 32 * 1024

// terminalInput is the client's terminal.open payload.
type terminalInput struct {
	Username string `json:"username"`
	Root     string `json:"root"`
	Cwd      string `json:"cwd"`
	Cols     uint16 `json:"cols"`
	Rows     uint16 `json:"rows"`
}

// handleTerminal upgrades conn to a PTY stream. It returns when the session
// ends; the caller closes the connection.
func (s *Server) handleTerminal(ctx context.Context, conn net.Conn, req brokerwire.Request) {
	var in terminalInput
	if len(req.Input) > 0 {
		if err := json.Unmarshal(req.Input, &in); err != nil {
			_ = brokerwire.WriteFrame(conn, brokerwire.Response{
				ID: req.ID, OK: false,
				Error: &brokerwire.WireError{Kind: "validation", Code: "bad_input", Message: "Invalid terminal input."},
			})
			return
		}
	}

	sess, err := s.broker.OpenTerminal(ctx, broker.TerminalRequest{
		Username: in.Username,
		Root:     in.Root,
		Cwd:      in.Cwd,
		Cols:     in.Cols,
		Rows:     in.Rows,
		Actor: capability.Actor{
			UserID:        req.Actor.UserID,
			IP:            req.Actor.IP,
			CorrelationID: req.Actor.CorrelationID,
		},
	})
	if err != nil {
		_ = brokerwire.WriteFrame(conn, brokerwire.Response{ID: req.ID, OK: false, Error: toWireError(err)})
		return
	}
	defer func() { _ = sess.Close() }()

	// From this frame on, the connection carries StreamFrames only.
	if err := brokerwire.WriteFrame(conn, brokerwire.Response{ID: req.ID, OK: true, Stream: true}); err != nil {
		return
	}

	// A single writer goroutine owns the socket: PTY output and the final exit
	// frame both go through it, so two goroutines can never interleave halves of
	// a frame on the wire.
	var wmu sync.Mutex
	write := func(f brokerwire.StreamFrame) error {
		wmu.Lock()
		defer wmu.Unlock()
		return brokerwire.WriteFrame(conn, f)
	}

	done := make(chan struct{})

	// Whether typed input is visible to the operator (see pty.InputVisible),
	// mirrored to the client whenever it changes so a recorder can redact what is
	// typed at a password prompt. The initial value is reported once, so the
	// client never has to assume.
	visible := sess.InputVisible()
	_ = write(brokerwire.StreamFrame{Kind: brokerwire.StreamEcho, Echo: &visible})
	reportVisibility := func() {
		if now := sess.InputVisible(); now != visible {
			visible = now
			state := now
			_ = write(brokerwire.StreamFrame{Kind: brokerwire.StreamEcho, Echo: &state})
		}
	}

	// PTY → client.
	go func() {
		defer close(done)
		buf := make([]byte, ptyReadChunk)
		for {
			n, rerr := sess.Read(buf)
			if n > 0 {
				// Checked *before* forwarding the output: a password prompt writes
				// "Password: " and turns echo off in the same breath, and the client
				// has to know the state has changed before the keystrokes arrive.
				reportVisibility()
				if werr := write(brokerwire.StreamFrame{Kind: brokerwire.StreamOut, Data: buf[:n]}); werr != nil {
					return
				}
			}
			if rerr != nil {
				// os.ErrClosed is how the pty package reports "the child exited"
				// (Linux surfaces EIO on the master); anything else is a genuine
				// read failure. Either way the session is over.
				code := sess.Wait()
				_ = write(brokerwire.StreamFrame{Kind: brokerwire.StreamExit, ExitCode: code})
				return
			}
		}
	}()

	// Client → PTY. Runs on this goroutine so the function returns (and the
	// deferred Close runs) as soon as the client hangs up.
	go func() {
		<-ctx.Done()
		_ = sess.Close() // unblocks the reader on shutdown
	}()

	for {
		var f brokerwire.StreamFrame
		if err := brokerwire.ReadFrame(conn, &f); err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug("terminal: client stream ended", "err", err)
			}
			return // client gone → deferred Close kills the process group
		}
		switch f.Kind {
		case brokerwire.StreamIn:
			// And again on the way in, so a state change that happened without any
			// intervening output still reaches the client before its keystrokes are
			// recorded.
			reportVisibility()
			if _, err := sess.Write(f.Data); err != nil {
				return
			}
		case brokerwire.StreamResize:
			_ = sess.Resize(f.Cols, f.Rows)
		default:
			// Unknown kinds are ignored rather than fatal: a newer client may
			// send a frame this broker does not know about yet.
		}
		select {
		case <-done:
			return // the shell exited while we were reading input
		default:
		}
	}
}
