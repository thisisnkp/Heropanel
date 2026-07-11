package brokerwire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/brokerwire"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := brokerwire.Request{
		ID:         "req_1",
		Capability: "service.restart",
		Input:      json.RawMessage(`{"service":"mariadb"}`),
		Actor:      brokerwire.Actor{CorrelationID: "corr-1"},
	}
	if err := brokerwire.WriteFrame(&buf, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out brokerwire.Request
	if err := brokerwire.ReadFrame(&buf, &out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.ID != in.ID || out.Capability != in.Capability || string(out.Input) != string(in.Input) {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestMultipleFramesSequential(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 3; i++ {
		if err := brokerwire.WriteFrame(&buf, brokerwire.HelloAck{OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		var ack brokerwire.HelloAck
		if err := brokerwire.ReadFrame(&buf, &ack); err != nil || !ack.OK {
			t.Fatalf("frame %d: ack=%+v err=%v", i, ack, err)
		}
	}
}

func TestReadFrameRejectsOversizeHeader(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], brokerwire.MaxFrame+1)
	buf.Write(hdr[:])
	var v map[string]any
	if err := brokerwire.ReadFrame(&buf, &v); err != brokerwire.ErrFrameTooLarge {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}
