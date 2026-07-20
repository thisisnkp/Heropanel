// Package brokerwire defines the on-the-wire protocol shared by hp-broker
// (server) and hpd (client): the request/response types and a minimal
// length-prefixed JSON framing. It depends only on the standard library so the
// root broker's parser surface stays tiny (ADR-0007).
package brokerwire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

const (
	// DefaultSocket is the standard broker socket path.
	DefaultSocket = "/run/heropanel/broker.sock"
	// MaxFrame caps a single frame's payload to bound memory use.
	MaxFrame = 1 << 20 // 1 MiB
)

// ErrFrameTooLarge is returned when a frame exceeds MaxFrame.
var ErrFrameTooLarge = errors.New("brokerwire: frame too large")

// Hello is the first frame the client sends to authenticate the connection.
type Hello struct {
	Token string `json:"token"`
}

// HelloAck is the server's handshake response.
type HelloAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Actor identifies who requested a privileged action (for correlation/audit).
type Actor struct {
	UserID        string `json:"user_id,omitempty"`
	IP            string `json:"ip,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

// Request is a single privileged-operation request.
type Request struct {
	ID         string          `json:"id"`
	Capability string          `json:"capability"`
	Input      json.RawMessage `json:"input,omitempty"`
	Actor      Actor           `json:"actor"`
}

// Response is the result of a Request. On failure, OK is false and Error is set.
// When Stream is true the connection has been upgraded: no further Responses
// follow, and both ends exchange StreamFrames until the connection closes.
type Response struct {
	ID     string         `json:"id"`
	OK     bool           `json:"ok"`
	Data   map[string]any `json:"data,omitempty"`
	Error  *WireError     `json:"error,omitempty"`
	Stream bool           `json:"stream,omitempty"`
}

// WireError is the transport form of a domain error.
type WireError struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── streaming (interactive PTY sessions) ─────────────────────────────────────
//
// Most capabilities are one-shot request/response. A terminal is not: it is a
// long-lived, bidirectional byte stream. Rather than bolt a second protocol (or
// a second socket) onto the broker, a connection *upgrades*: the client sends a
// normal Request for a streaming capability, the broker replies with a Response
// carrying Stream=true, and from then on both ends exchange StreamFrames on that
// same connection until it closes. The existing framing, size cap, peer-credential
// check, and token handshake all still apply — which is the point.

// Stream frame kinds.
const (
	StreamIn     = "in"     // client → broker: bytes to write to the PTY
	StreamOut    = "out"    // broker → client: bytes read from the PTY
	StreamResize = "resize" // client → broker: new window size
	StreamExit   = "exit"   // broker → client: the session ended
	StreamError  = "error"  // broker → client: the session failed
	// StreamEcho tells the client whether the PTY is currently echoing what is
	// typed. It is emitted only when the state changes.
	//
	// It exists for session recording. A password prompt (sudo, mysql -p, ssh)
	// works by turning the terminal's ECHO bit off, so the typed secret never
	// reaches the screen — and therefore never reaches the recorded *output*. But
	// a recording that also captures *input* would capture it, turning the panel
	// into a plaintext password store. Only the broker can see the termios state,
	// since only the broker holds the PTY, so it has to say so on the wire.
	StreamEcho = "echo"
)

// StreamFrame is one message in an upgraded (streaming) connection. Data is
// base64-encoded by encoding/json, so terminal bytes are binary-safe.
type StreamFrame struct {
	Kind     string `json:"kind"`
	Data     []byte `json:"data,omitempty"`
	Cols     uint16 `json:"cols,omitempty"`
	Rows     uint16 `json:"rows,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
	// Echo carries the PTY's echo state on a StreamEcho frame. It is a pointer so
	// that "echo is off" (false) is distinguishable from "this frame says nothing
	// about echo", which every other kind does.
	Echo *bool `json:"echo,omitempty"`
}

// WriteFrame marshals v to JSON and writes it with a 4-byte length prefix.
func WriteFrame(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(payload) > MaxFrame {
		return ErrFrameTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed JSON frame and unmarshals it into v. It
// returns io.EOF when the connection is closed cleanly between frames.
func ReadFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrame {
		return ErrFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}
