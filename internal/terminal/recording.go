package terminal

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"
)

// Session recording, in the asciicast v2 format (the one asciinema uses).
//
// The format is a JSON header line followed by one JSON array per event:
//
//	{"version":2,"width":120,"height":30,"timestamp":1700000000}
//	[0.482, "o", "hps1@host:~$ "]
//	[1.913, "i", "ls\r"]
//	[2.004, "r", "132x40"]
//
// It is a real, documented format rather than something invented here, so a
// recording remains readable by tools outside this panel — which matters for an
// audit artifact that may outlive the software that wrote it. The panel's own
// player replays it into xterm.js, so nothing new is shipped to the browser.

// asciicastVersion is the format revision written in the header.
const asciicastVersion = 2

// RedactedMarker is what stands in for input typed while the terminal was not
// echoing. It is deliberately a fixed string.
const RedactedMarker = "[redacted]"

// MaxRecordingBytes caps one recording. A session that cats a large file can
// produce output without limit, and a terminal must never be a way to fill the
// panel's disk. Past the cap the recording stops growing and is marked
// truncated, which the UI shows — a recording that quietly stops is worse than
// one that says where it stopped.
const MaxRecordingBytes int64 = 8 << 20 // 8 MiB

// Recorder writes an asciicast stream. Every method is safe to call from the
// two goroutines that pump a terminal session in opposite directions.
type Recorder struct {
	w     io.Writer
	start time.Time
	now   func() time.Time

	mu        sync.Mutex
	written   int64
	truncated bool
	closed    bool

	// echo mirrors the PTY's echo state, and redacting records whether the
	// current no-echo run has already been marked.
	echo      bool
	redacting bool

	// pending holds a trailing incomplete UTF-8 sequence from the previous
	// output chunk. PTY reads land on arbitrary byte boundaries, and encoding a
	// half rune as JSON would replace it with U+FFFD — a permanent hole in the
	// recording for a byte that was perfectly fine.
	pending []byte
}

// NewRecorder writes the asciicast header and returns a recorder ready for
// events. now may be nil (time.Now); it is injectable so tests can assert
// timings rather than tolerate them.
func NewRecorder(w io.Writer, cols, rows uint16, now func() time.Time) (*Recorder, error) {
	if now == nil {
		now = time.Now
	}
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	start := now()
	header, err := json.Marshal(map[string]any{
		"version":   asciicastVersion,
		"width":     cols,
		"height":    rows,
		"timestamp": start.Unix(),
	})
	if err != nil {
		return nil, err
	}
	r := &Recorder{w: w, start: start, now: now, echo: true}
	if _, err := w.Write(append(header, '\n')); err != nil {
		return nil, err
	}
	r.written = int64(len(header) + 1)
	return r, nil
}

// SetEcho records whether typed input is currently visible to the operator.
//
// The broker computes this (see pty.InputVisible); it is deliberately not just
// "is ECHO on", because an interactive shell runs with ECHO off nearly all the
// time — readline does its own echoing. Only a genuine password prompt hides
// input, and only then is input redacted.
func (r *Recorder) SetEcho(visible bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.echo == visible {
		return
	}
	r.echo = visible
	if visible {
		r.redacting = false
	}
}

// Output records bytes the PTY produced.
func (r *Recorder) Output(b []byte) {
	if len(b) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-attach whatever was held back last time, then hold back any new
	// incomplete tail.
	chunk := b
	if len(r.pending) > 0 {
		chunk = append(append([]byte{}, r.pending...), b...)
		r.pending = nil
	}
	complete, tail := splitCompleteUTF8(chunk)
	r.pending = append([]byte{}, tail...)
	if len(complete) == 0 {
		return
	}
	r.event("o", string(complete))
}

// Input records bytes the operator typed.
//
// While the terminal is not echoing, the actual keystrokes are dropped and a
// single marker is written for the whole run. One marker per keystroke would
// leak the length of the password, which is most of the value of not recording
// it in the first place.
func (r *Recorder) Input(b []byte) {
	if len(b) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.echo {
		if r.redacting {
			return
		}
		r.redacting = true
		r.event("i", RedactedMarker)
		return
	}
	r.event("i", string(b))
}

// Resize records a window-size change, so playback reflows the way the session
// did rather than replaying a wide session into a narrow terminal.
func (r *Recorder) Resize(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.event("r", fmt.Sprintf("%dx%d", cols, rows))
}

// event writes one asciicast line. The caller holds the lock.
func (r *Recorder) event(kind, data string) {
	if r.closed || r.truncated {
		return
	}
	line, err := json.Marshal([]any{r.now().Sub(r.start).Seconds(), kind, data})
	if err != nil {
		return
	}
	if r.written+int64(len(line))+1 > MaxRecordingBytes {
		r.truncated = true
		// Leave a visible last event, so playback ends with an explanation rather
		// than simply stopping.
		if note, mErr := json.Marshal([]any{
			r.now().Sub(r.start).Seconds(), "o",
			"\r\n[recording truncated: size limit reached]\r\n",
		}); mErr == nil {
			if n, wErr := r.w.Write(append(note, '\n')); wErr == nil {
				r.written += int64(n)
			}
		}
		return
	}
	n, err := r.w.Write(append(line, '\n'))
	if err != nil {
		// A recorder that cannot write must not take the session down with it: the
		// operator's shell is the primary function, the recording is the artifact.
		r.truncated = true
		return
	}
	r.written += int64(n)
}

// Close finalises the recording. Any incomplete UTF-8 tail is dropped rather
// than written as a replacement character.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.pending = nil
	return nil
}

// Written is the recording's size in bytes, and Truncated whether it hit the cap
// (or a write error). Both are stored with the recording's metadata.
func (r *Recorder) Written() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.written
}

func (r *Recorder) Truncated() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.truncated
}

// splitCompleteUTF8 splits b at the last rune boundary, returning the part that
// is safe to encode and the incomplete tail to carry into the next chunk. Bytes
// that are simply invalid (not truncated) are passed through rather than held,
// so a corrupt stream cannot stall the recording forever.
func splitCompleteUTF8(b []byte) (complete, tail []byte) {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		c := b[i]
		if c&0xC0 == 0x80 {
			continue // continuation byte; keep scanning back for its lead byte
		}
		var need int
		switch {
		case c&0x80 == 0:
			need = 1
		case c&0xE0 == 0xC0:
			need = 2
		case c&0xF0 == 0xE0:
			need = 3
		case c&0xF8 == 0xF0:
			need = 4
		default:
			return b, nil // invalid lead byte: not a truncation, do not hold it
		}
		if len(b)-i < need {
			return b[:i], b[i:]
		}
		return b, nil
	}
	return b, nil
}
