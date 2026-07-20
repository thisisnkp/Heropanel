package terminal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The recorder captures keystrokes, which is the whole reason its redaction has
// to be right: a password prompt turns terminal echo off, and input recorded
// during that window would be a plaintext password sitting in the panel's own
// store. These pin that it never gets there, and that the file stays a valid
// asciicast.

// fakeClock advances by a fixed step on every read, so event timings are exact
// rather than approximately-right.
type fakeClock struct {
	t    time.Time
	step time.Duration
}

func (c *fakeClock) now() time.Time {
	t := c.t
	c.t = c.t.Add(c.step)
	return t
}

func newTestRecorder(t *testing.T) (*Recorder, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	clock := &fakeClock{t: time.Unix(1700000000, 0), step: time.Second}
	r, err := NewRecorder(&buf, 120, 30, clock.now)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	return r, &buf
}

// events parses everything after the header line.
func events(t *testing.T, buf *bytes.Buffer) [][]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("no output at all")
	}
	out := [][]any{}
	for _, l := range lines[1:] {
		var e []any
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("event %q is not valid JSON: %v", l, err)
		}
		out = append(out, e)
	}
	return out
}

func TestHeaderIsAValidAsciicastV2Header(t *testing.T) {
	_, buf := newTestRecorder(t)
	line, _, _ := strings.Cut(buf.String(), "\n")

	var h struct {
		Version   int   `json:"version"`
		Width     int   `json:"width"`
		Height    int   `json:"height"`
		Timestamp int64 `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		t.Fatalf("header is not JSON: %v", err)
	}
	if h.Version != 2 {
		t.Errorf("version = %d, want 2 — other tools read this field to decide how to parse", h.Version)
	}
	if h.Width != 120 || h.Height != 30 {
		t.Errorf("size = %dx%d, want 120x30", h.Width, h.Height)
	}
	if h.Timestamp == 0 {
		t.Error("timestamp must be set so a recording can be dated without its metadata")
	}
}

func TestOutputAndInputAreRecordedWithTimings(t *testing.T) {
	r, buf := newTestRecorder(t)
	r.Output([]byte("hps1@host:~$ "))
	r.Input([]byte("ls\r"))
	r.Output([]byte("index.php\r\n"))

	got := events(t, buf)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %v", len(got), got)
	}
	want := []struct{ kind, data string }{
		{"o", "hps1@host:~$ "},
		{"i", "ls\r"},
		{"o", "index.php\r\n"},
	}
	for i, w := range want {
		if got[i][1] != w.kind || got[i][2] != w.data {
			t.Errorf("event %d = %v, want [%q %q]", i, got[i], w.kind, w.data)
		}
	}
	// The clock steps one second per event, and the header consumed the first
	// reading, so offsets run 1, 2, 3.
	for i, e := range got {
		if e[0].(float64) != float64(i+1) {
			t.Errorf("event %d offset = %v, want %d", i, e[0], i+1)
		}
	}
}

// The invariant the whole design rests on.
func TestInputIsRedactedWhileEchoIsOff(t *testing.T) {
	r, buf := newTestRecorder(t)
	r.Output([]byte("[sudo] password for hps1: "))
	r.SetEcho(false)
	r.Input([]byte("h"))
	r.Input([]byte("u"))
	r.Input([]byte("n"))
	r.Input([]byte("t"))
	r.Input([]byte("er2"))
	r.Input([]byte("\r"))
	r.SetEcho(true)
	r.Output([]byte("\r\nroot@host:~# "))

	body := buf.String()
	if strings.Contains(body, "hunter2") || strings.Contains(body, `"i", "h"`) {
		t.Fatalf("the typed password reached the recording:\n%s", body)
	}
	if !strings.Contains(body, RedactedMarker) {
		t.Errorf("a redaction marker must be written so playback shows something happened:\n%s", body)
	}

	// Exactly one marker for the whole run: one per keystroke would leak the
	// password's length, which is most of what redaction is protecting.
	if n := strings.Count(body, RedactedMarker); n != 1 {
		t.Errorf("got %d redaction markers, want exactly 1 — the count reveals the secret's length", n)
	}
}

func TestInputIsRecordedAgainOnceEchoReturns(t *testing.T) {
	r, buf := newTestRecorder(t)
	r.SetEcho(false)
	r.Input([]byte("secret"))
	r.SetEcho(true)
	r.Input([]byte("whoami\r"))

	body := buf.String()
	if strings.Contains(body, "secret") {
		t.Fatal("input from the no-echo window leaked")
	}
	if !strings.Contains(body, "whoami") {
		t.Error("input after echo returned must be recorded normally")
	}
}

// Two prompts in a row each get their own marker; the run resets when echo
// comes back.
func TestEachNoEchoRunGetsItsOwnMarker(t *testing.T) {
	r, buf := newTestRecorder(t)
	for i := 0; i < 2; i++ {
		r.SetEcho(false)
		r.Input([]byte("pw"))
		r.SetEcho(true)
	}
	if n := strings.Count(buf.String(), RedactedMarker); n != 2 {
		t.Errorf("got %d markers for two prompts, want 2", n)
	}
}

// Output is *not* redacted: echo-off means the terminal is not printing the
// secret, so whatever the program does print is legitimate context.
func TestOutputIsNeverRedacted(t *testing.T) {
	r, buf := newTestRecorder(t)
	r.SetEcho(false)
	r.Output([]byte("Password: "))
	if !strings.Contains(buf.String(), "Password: ") {
		t.Error("output during a password prompt is what makes the recording readable; it must survive")
	}
}

// PTY reads land on arbitrary byte boundaries. Encoding half a rune would put a
// permanent U+FFFD in the recording for a byte that was perfectly valid.
func TestSplitMultiByteRuneIsReassembledAcrossChunks(t *testing.T) {
	r, buf := newTestRecorder(t)
	star := []byte("★") // three bytes
	r.Output(star[:2])
	r.Output(star[2:])
	r.Close()

	body := buf.String()
	if !strings.Contains(body, "★") {
		t.Errorf("the split rune was not reassembled:\n%s", body)
	}
	if strings.Contains(body, "�") {
		t.Errorf("a replacement character was written instead of the real rune:\n%s", body)
	}
}

func TestInvalidBytesDoNotStallTheRecording(t *testing.T) {
	r, buf := newTestRecorder(t)
	// 0xFF is not a valid UTF-8 lead byte anywhere. It must be passed through
	// rather than held back forever waiting for continuation bytes.
	r.Output([]byte{0xFF})
	r.Output([]byte("after"))
	if !strings.Contains(buf.String(), "after") {
		t.Error("a stray invalid byte blocked everything that followed it")
	}
}

func TestResizeIsRecorded(t *testing.T) {
	r, buf := newTestRecorder(t)
	r.Resize(132, 43)
	got := events(t, buf)
	if len(got) != 1 || got[0][1] != "r" || got[0][2] != "132x43" {
		t.Errorf("resize event = %v, want [_ \"r\" \"132x43\"]", got)
	}
}

func TestRecordingStopsAtTheSizeCapAndSaysSo(t *testing.T) {
	var buf bytes.Buffer
	clock := &fakeClock{t: time.Unix(1700000000, 0), step: time.Millisecond}
	r, err := NewRecorder(&buf, 80, 24, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	blob := bytes.Repeat([]byte("x"), 64*1024)
	for i := 0; i < 200; i++ { // ~12 MiB offered, well past the cap
		r.Output(blob)
	}

	if !r.Truncated() {
		t.Fatal("a recording past the cap must report itself truncated")
	}
	if int64(buf.Len()) > MaxRecordingBytes+128*1024 {
		t.Errorf("recording grew to %d bytes, past the %d cap", buf.Len(), MaxRecordingBytes)
	}
	if !strings.Contains(buf.String(), "recording truncated") {
		t.Error("playback must end with an explanation, not simply stop")
	}
}

// A recorder whose writer fails must not take the shell down with it: the
// operator's session is the primary function, the recording is the artifact.
type failingWriter struct{ n int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n > 1 { // let the header through, then fail
		return 0, errWrite
	}
	return len(p), nil
}

var errWrite = &writeError{}

type writeError struct{}

func (*writeError) Error() string { return "disk full" }

func TestWriteFailureIsSurvivableAndReported(t *testing.T) {
	r, err := NewRecorder(&failingWriter{}, 80, 24, (&fakeClock{t: time.Unix(1, 0), step: time.Second}).now)
	if err != nil {
		t.Fatal(err)
	}
	r.Output([]byte("hello")) // must not panic
	r.Output([]byte("world"))
	if !r.Truncated() {
		t.Error("a failed write must mark the recording incomplete rather than pretend it is whole")
	}
}
