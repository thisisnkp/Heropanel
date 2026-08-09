package mail

import "testing"

// parseQueue reads postfix's real `postqueue -j` shape and skips torn lines.
func TestParseQueue(t *testing.T) {
	raw := `{"queue_name":"deferred","queue_id":"B1F5D2A","arrival_time":1753272000,"message_size":1523,"sender":"info@example.com","recipients":[{"address":"x@nowhere.invalid","delay_reason":"Host not found"}]}
{"queue_name":"active","queue_id":"C2E6F3B","arrival_time":1753272060,"message_size":900,"sender":"a@b.example","recipients":[{"address":"c@d.example"}]}
{"torn json...
`
	msgs := parseQueue(raw)
	if len(msgs) != 2 {
		t.Fatalf("parsed %d messages, want 2 (torn line skipped)", len(msgs))
	}
	m := msgs[0]
	if m.ID != "B1F5D2A" || m.Queue != "deferred" || m.SizeBytes != 1523 || m.Sender != "info@example.com" {
		t.Errorf("message = %+v", m)
	}
	if len(m.Recipients) != 1 || m.Recipients[0].DelayReason != "Host not found" {
		t.Errorf("recipients = %+v", m.Recipients)
	}
	if m.Arrival != "2025-07-23T12:00:00Z" {
		t.Errorf("arrival = %q", m.Arrival)
	}
	if len(parseQueue("")) != 0 {
		t.Error("an empty queue must parse to zero messages")
	}
}
