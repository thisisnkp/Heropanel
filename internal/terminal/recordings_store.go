package terminal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Storing, listing, serving and expiring recorded sessions.
//
// The asciicast files live on disk rather than in the database: terminal output
// is unbounded, and a row that can grow to megabytes is the wrong shape for a
// datastore the panel also uses for its hot path. The database keeps only what
// you search and authorise on.

// DefaultRetention is how long a recording is kept. A terminal session is the
// most powerful thing the panel hands out, so there has to be a window in which
// "what did that person actually do" is answerable — and a limit after which the
// panel stops holding a transcript of someone's work.
const DefaultRetention = 30 * 24 * time.Hour

// Recording is one recorded session's metadata.
//
// Timestamps are strings in the panel's shared "2006-01-02 15:04:05" UTC layout,
// matching every other table: SQLite stores them as TEXT, and scanning that into
// a time.Time fails at runtime rather than at compile time — which is exactly
// how it got past unit tests and only surfaced in the live e2e.
type Recording struct {
	ID     int64  `db:"id" json:"-"`
	UID    string `db:"uid" json:"uid"`
	SiteID int64  `db:"site_id" json:"-"`
	// SiteUID and SiteName are joined in rather than stored. The cross-site
	// listing is what an auditor actually reads, and "a session on site #7" is
	// not something anyone can act on; the internal id stays unexposed.
	SiteUID     string `db:"site_uid" json:"site_uid"`
	SiteName    string `db:"site_name" json:"site_name"`
	ActorUserID int64  `db:"actor_user_id" json:"actor_user_id"`
	ActorEmail  string `db:"actor_email" json:"actor_email"`
	ActorIP     string `db:"actor_ip" json:"actor_ip"`
	SystemUser  string `db:"system_user" json:"system_user"`
	Path        string `db:"path" json:"-"` // relative to the recordings dir; never exposed
	SizeBytes   int64  `db:"size_bytes" json:"size_bytes"`
	DurationMS  int64  `db:"duration_ms" json:"duration_ms"`
	Truncated   bool   `db:"truncated" json:"truncated"`
	StartedAt   string `db:"started_at" json:"started_at"`
	EndedAt     string `db:"ended_at" json:"ended_at,omitempty"`
	ExpiresAt   string `db:"expires_at" json:"expires_at"`
}

// TimeLayout is the timestamp format recordings are stored in, shared with the
// rest of the datastore.
const TimeLayout = "2006-01-02 15:04:05"

// FormatTime renders a timestamp in the stored layout, always UTC.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Recordings is the metadata store.
type Recordings interface {
	Create(ctx context.Context, r *Recording) error
	Finish(ctx context.Context, uid string, size, durationMS int64, truncated bool) error
	Get(ctx context.Context, uid string) (*Recording, error)
	List(ctx context.Context, siteID int64, limit, offset int) ([]Recording, error)
	Delete(ctx context.Context, uid string) (string, error)
	Expired(ctx context.Context, now time.Time, limit int) ([]Recording, error)
}

// RecordingStore writes and reads the asciicast files themselves.
type RecordingStore struct {
	dir       string
	meta      Recordings
	retention time.Duration
}

// NewRecordingStore constructs the store. An empty dir disables recording
// entirely — the terminal still works, it is simply not recorded, which is the
// right behaviour for an operator who has turned the feature off.
func NewRecordingStore(dir string, meta Recordings, retention time.Duration) *RecordingStore {
	if retention <= 0 {
		retention = DefaultRetention
	}
	return &RecordingStore{dir: strings.TrimSpace(dir), meta: meta, retention: retention}
}

// Enabled reports whether sessions are recorded.
func (s *RecordingStore) Enabled() bool { return s != nil && s.dir != "" && s.meta != nil }

// SessionMeta is what a new recording needs to know about the session.
type SessionMeta struct {
	SiteID      int64
	SiteUID     string
	ActorUserID int64
	ActorEmail  string
	ActorIP     string
	SystemUser  string
	Cols, Rows  uint16
}

// Session couples an open recorder with the file and row it writes to.
type Session struct {
	*Recorder
	store   *RecordingStore
	file    *os.File
	uid     string
	started time.Time
}

// Begin creates the file and the metadata row, and returns a recorder wired to
// them. A failure to start recording is deliberately *not* fatal to the terminal
// session: the shell is what the operator asked for, and refusing it because the
// audit artifact could not be opened would turn a disk problem into an outage.
// The caller logs the error and carries on with a nil Session.
func (s *RecordingStore) Begin(ctx context.Context, m SessionMeta) (*Session, error) {
	if !s.Enabled() {
		return nil, nil
	}
	now := time.Now().UTC()
	// One directory per day keeps a busy panel from putting a hundred thousand
	// files in one directory, which is slow to list and slow to sweep.
	rel := filepath.ToSlash(filepath.Join(now.Format("2006-01-02"), m.SiteUID))
	if err := os.MkdirAll(filepath.Join(s.dir, filepath.FromSlash(rel)), 0o750); err != nil {
		return nil, errx.Wrap(err, errx.KindInternal, "recording_dir_failed",
			"Could not create the recordings directory.")
	}

	// The UID is minted here rather than by the store, so the file can be named
	// after it and the row and the file can never disagree about which recording
	// is which.
	uid := idgen.NewULID()
	rec := &Recording{
		UID:         uid,
		SiteID:      m.SiteID,
		ActorUserID: m.ActorUserID,
		ActorEmail:  m.ActorEmail,
		ActorIP:     m.ActorIP,
		SystemUser:  m.SystemUser,
		Path:        rel + "/" + uid + ".cast",
		StartedAt:   FormatTime(now),
		ExpiresAt:   FormatTime(now.Add(s.retention)),
	}
	if err := s.meta.Create(ctx, rec); err != nil {
		return nil, err
	}

	// 0600: a recording is a transcript of privileged work. Nothing but hpd reads
	// it, and it reaches an operator only through an authorised HTTP request.
	full, err := s.resolve(rec.Path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_, _ = s.meta.Delete(ctx, rec.UID)
		return nil, errx.Wrap(err, errx.KindInternal, "recording_open_failed",
			"Could not open the recording file.")
	}

	r, err := NewRecorder(f, m.Cols, m.Rows, nil)
	if err != nil {
		_ = f.Close()
		return nil, errx.Wrap(err, errx.KindInternal, "recording_header_failed",
			"Could not start the recording.")
	}
	return &Session{Recorder: r, store: s, file: f, uid: rec.UID, started: now}, nil
}

// End closes the recording and records its size, duration, and completeness.
func (rs *Session) End(ctx context.Context) error {
	if rs == nil {
		return nil
	}
	_ = rs.Recorder.Close()
	size, truncated := rs.Recorder.Written(), rs.Recorder.Truncated()
	if err := rs.file.Close(); err != nil {
		truncated = true
	}
	return rs.store.meta.Finish(ctx, rs.uid, size, time.Since(rs.started).Milliseconds(), truncated)
}

// UID identifies the recording this session is writing, for the audit trail.
func (rs *Session) UID() string {
	if rs == nil {
		return ""
	}
	return rs.uid
}

// List, Get and Delete proxy the metadata store, with Delete also removing the
// file.
func (s *RecordingStore) List(ctx context.Context, siteID int64, limit, offset int) ([]Recording, error) {
	if !s.Enabled() {
		return []Recording{}, nil
	}
	return s.meta.List(ctx, siteID, limit, offset)
}

func (s *RecordingStore) Get(ctx context.Context, uid string) (*Recording, error) {
	if !s.Enabled() {
		return nil, errx.New(errx.KindUnavailable, "recording_disabled", "Session recording is not enabled.")
	}
	return s.meta.Get(ctx, uid)
}

func (s *RecordingStore) Delete(ctx context.Context, uid string) error {
	if !s.Enabled() {
		return errx.New(errx.KindUnavailable, "recording_disabled", "Session recording is not enabled.")
	}
	rel, err := s.meta.Delete(ctx, uid)
	if err != nil {
		return err
	}
	if full, rErr := s.resolve(rel); rErr == nil {
		_ = os.Remove(full)
	}
	return nil
}

// Open streams a recording's asciicast to the caller.
func (s *RecordingStore) Open(ctx context.Context, uid string) (io.ReadCloser, *Recording, error) {
	rec, err := s.Get(ctx, uid)
	if err != nil {
		return nil, nil, err
	}
	full, err := s.resolve(rec.Path)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		// The row outliving its file is possible (a manual delete, a restore from
		// a partial backup) and must read as a missing recording, not a crash.
		return nil, nil, errx.NotFound("recording_file_missing",
			"This recording's data is no longer on disk.")
	}
	return f, rec, nil
}

// Purge deletes recordings past their retention date, file and row. It returns
// how many went.
func (s *RecordingStore) Purge(ctx context.Context, now time.Time) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	expired, err := s.meta.Expired(ctx, now, 200)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rec := range expired {
		if err := s.Delete(ctx, rec.UID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// PurgeInterval is how often the retention sweeper runs. Hourly is frequent
// enough that a recording goes close to its expiry, and rare enough to be
// invisible.
const PurgeInterval = time.Hour

// RunPurger deletes expired recordings until ctx is cancelled. It sweeps once
// immediately, so a panel restarted after a long outage does not keep
// transcripts past their retention until the first tick.
func (s *RecordingStore) RunPurger(ctx context.Context, log *slog.Logger) {
	if !s.Enabled() {
		return
	}
	t := time.NewTicker(PurgeInterval)
	defer t.Stop()
	for {
		if n, err := s.Purge(ctx, time.Now()); err != nil {
			if log != nil {
				log.Warn("terminal recording sweep failed", "err", err)
			}
		} else if n > 0 && log != nil {
			log.Info("deleted expired terminal recordings", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Retention is how long recordings are kept.
func (s *RecordingStore) Retention() time.Duration { return s.retention }

// resolve turns a stored relative path into an absolute one, refusing anything
// that would escape the recordings directory.
//
// Paths here are written by this package, not by a user — but a recording is
// served over HTTP by UID, and a row whose path was tampered with (a restored
// backup, a hand-edited database) must not become a way to read arbitrary files
// through the panel.
func (s *RecordingStore) resolve(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash("/" + strings.TrimPrefix(rel, "/")))
	full := filepath.Join(s.dir, clean)
	base, err := filepath.Abs(s.dir)
	if err != nil {
		return "", errx.Internal(err)
	}
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", errx.Internal(err)
	}
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", errx.Forbidden("recording_path_escape", "That recording path is not inside the recordings directory.")
	}
	return abs, nil
}
