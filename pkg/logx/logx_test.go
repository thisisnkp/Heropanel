package logx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/thisisnkp/heropanel/pkg/logx"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"Warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"":      slog.LevelInfo, // default
		"weird": slog.LevelInfo, // default
	}
	for in, want := range cases {
		if got := logx.ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	log := logx.New(&buf, logx.Options{Level: slog.LevelInfo, Format: logx.FormatJSON})
	log.Info("hello", "k", "v")

	line := strings.TrimSpace(buf.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, line)
	}
	if m["msg"] != "hello" || m["k"] != "v" {
		t.Fatalf("unexpected log fields: %v", m)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := logx.New(&buf, logx.Options{Level: slog.LevelWarn, Format: logx.FormatJSON})
	log.Info("should be filtered")
	if buf.Len() != 0 {
		t.Fatalf("info log should be filtered at warn level, got %q", buf.String())
	}
	log.Warn("kept")
	if buf.Len() == 0 {
		t.Fatal("warn log should be emitted")
	}
}
