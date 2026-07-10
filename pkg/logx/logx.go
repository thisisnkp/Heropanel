// Package logx provides HeroPanel's structured logging setup on top of the
// standard library log/slog. All components (core, broker, modules) use it so
// logs share one JSON shape with correlation ids.
package logx

import (
	"io"
	"log/slog"
	"strings"
)

// Format selects the log encoding.
type Format string

const (
	FormatJSON Format = "json"
	FormatText Format = "text"
)

// Options configures a logger.
type Options struct {
	Level     slog.Level
	Format    Format
	AddSource bool
}

// New builds a *slog.Logger writing to w with the given options.
func New(w io.Writer, opts Options) *slog.Logger {
	ho := &slog.HandlerOptions{Level: opts.Level, AddSource: opts.AddSource}
	var h slog.Handler
	if opts.Format == FormatText {
		h = slog.NewTextHandler(w, ho)
	} else {
		h = slog.NewJSONHandler(w, ho)
	}
	return slog.New(h)
}

// ParseLevel maps a case-insensitive string to an slog.Level, defaulting to
// Info for unrecognized input.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
