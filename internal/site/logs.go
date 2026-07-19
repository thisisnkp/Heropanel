package site

import (
	"context"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Log kinds a site keeps.
const (
	LogAccess = "access"
	LogError  = "error"
)

// Default and maximum tail depth. The cap is the same one the broker enforces;
// it is repeated here so a bad request is refused before it costs a socket
// round-trip, not because the edge is trusted to be the only guard.
const (
	DefaultLogLines = 200
	MaxLogLines     = 5000
)

// LogView is a page of one of a site's log files.
type LogView struct {
	Kind    string `json:"kind"`
	Lines   int    `json:"lines"`
	Content string `json:"content"`
	// Exists is false when the file is not there yet. That is the ordinary state
	// of a site nobody has visited, and it is reported as a fact rather than an
	// error so the UI can say "no requests yet" instead of showing a fault.
	Exists bool `json:"exists"`
}

func validateLogKind(kind string) error {
	switch kind {
	case LogAccess, LogError:
		return nil
	}
	return errx.Validation("bad_log_kind", "Log kind must be \"access\" or \"error\".")
}

// Logs returns the tail of a site's access or error log.
//
// It goes through the broker because the logs belong to the site: the tree is
// 0750 owned by the site's own Linux user, and hpd runs as the unprivileged
// panel user. Reading them directly would mean relaxing exactly the isolation
// the per-site user exists to enforce.
func (s *Service) Logs(ctx context.Context, uid, kind string, lines int) (*LogView, error) {
	if err := validateLogKind(kind); err != nil {
		return nil, err
	}
	if lines == 0 {
		lines = DefaultLogLines
	}
	if lines < 1 || lines > MaxLogLines {
		return nil, errx.Validation("bad_log_lines", "lines must be between 1 and 5000.")
	}
	if s.broker == nil {
		return nil, errx.New(errx.KindUnavailable, "broker_unavailable",
			"The broker is not available; site logs cannot be read.")
	}

	rec, err := s.repo.GetByUID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !rec.HomeDir.Valid || rec.HomeDir.String == "" {
		// A site that never finished provisioning has no log directory. This is
		// not an error condition to shout about; there is simply nothing yet.
		return &LogView{Kind: kind, Exists: false}, nil
	}

	out, err := s.broker.Invoke(ctx, "site.read_log", map[string]any{
		"root":  rec.HomeDir.String,
		"kind":  kind,
		"lines": lines,
	})
	if err != nil {
		return nil, err
	}

	v := &LogView{Kind: kind}
	if c, ok := out["content"].(string); ok {
		v.Content = c
	}
	if e, ok := out["exists"].(bool); ok {
		v.Exists = e
	}
	// JSON numbers decode as float64 through the broker envelope.
	if n, ok := out["lines"].(float64); ok {
		v.Lines = int(n)
	}
	return v, nil
}
