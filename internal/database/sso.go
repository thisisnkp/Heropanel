package database

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
)

// Sign-on sessions for an external database client (Adminer, phpMyAdmin).
//
// The design decision worth stating plainly: HeroPanel does not keep database
// user passwords. It could — there is a perfectly good cipher in pkg/secrets and
// most panels do exactly that — but then one panel compromise hands over every
// customer's standing database credentials, and there is no way to tell after
// the fact which ones were used.
//
// So a hand-off mints a throwaway MariaDB account instead: random password,
// granted on exactly one database, dropped a few minutes later by the sweeper.
// The credentials go straight to the browser to be POSTed at the client's login
// form and are never stored anywhere. The cost is a little machinery (this file
// and a sweep loop); the benefit is that the blast radius of a session is one
// database for fifteen minutes.
const (
	// SSOTTL is how long a hand-off account lives. Long enough to click through
	// a login form, short enough that a leaked credential is nearly worthless.
	SSOTTL = 15 * time.Minute

	// SSOSweepInterval is how often expired accounts are dropped.
	SSOSweepInterval = 5 * time.Minute

	// ssoUserPrefix marks accounts this panel owns, so the sweeper can never
	// mistake a real database user for one of its own.
	ssoUserPrefix = "hpsso_"
)

// sqlTime is the timestamp format both SQLite (TEXT) and MariaDB (DATETIME)
// accept, so the service writes times without dialect branching.
const sqlTime = "2006-01-02 15:04:05"

// SSOSession is a one-time credential set for an external database client. It is
// returned exactly once, at creation, and never persisted.
type SSOSession struct {
	// URL is where the client's login form lives; the browser POSTs to it.
	URL string `json:"url"`
	// Driver/Server/Database identify the target for Adminer's auth[] fields.
	Driver   string `json:"driver"`
	Server   string `json:"server"`
	Database string `json:"database"`
	// Username/Password are the throwaway account. Shown once.
	Username  string `json:"username"`
	Password  string `json:"password"`
	ExpiresAt string `json:"expires_at"`
}

// SSOSessionRecord is the persistence row. It holds no secret — just enough for
// the sweeper to know which account to drop and when.
type SSOSessionRecord struct {
	ID           int64  `db:"id"`
	UID          string `db:"uid"`
	DBInstanceID int64  `db:"db_instance_id"`
	Username     string `db:"username"`
	CreatedAt    string `db:"created_at"`
	ExpiresAt    string `db:"expires_at"`
}

// SSORepo is the persistence contract for hand-off sessions.
type SSORepo interface {
	InsertSSOSession(ctx context.Context, r *SSOSessionRecord) error
	ListExpiredSSOSessions(ctx context.Context, now string) ([]SSOSessionRecord, error)
	DeleteSSOSession(ctx context.Context, uid string) error
}

// WithAdminer wires the URL of the database client to hand off to and the store
// that tracks hand-off sessions. Without it, StartSSO reports "unavailable" —
// there is nowhere to hand off to. Returns s for chaining.
func (s *Service) WithAdminer(url string, repo SSORepo) *Service {
	s.adminerURL = strings.TrimSpace(url)
	s.ssoRepo = repo
	return s
}

// StartSSO mints a throwaway account for one database and returns the
// credentials for the browser to POST at the client's login form.
func (s *Service) StartSSO(ctx context.Context, dbUID string) (*SSOSession, error) {
	if s.adminerURL == "" || s.ssoRepo == nil {
		return nil, errx.New(errx.KindUnavailable, "adminer_unavailable",
			"No database client is configured. Set database.adminer_url and restart the panel.")
	}
	rec, err := s.repo.GetDatabaseByUID(ctx, dbUID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}

	// A fresh account per hand-off: sessions never share a credential, so
	// revoking one cannot cut another short.
	uid := idgen.NewULID()
	username := ssoUserPrefix + strings.ToLower(uid[len(uid)-12:])
	password, err := randomPassword()
	if err != nil {
		return nil, err
	}
	expires := time.Now().UTC().Add(SSOTTL)

	// Record before creating, so a crash between the two leaves a row the sweeper
	// will clean up rather than an orphan account nothing knows about.
	row := &SSOSessionRecord{
		UID: uid, DBInstanceID: rec.ID, Username: username,
		ExpiresAt: expires.Format(sqlTime),
	}
	if err := s.ssoRepo.InsertSSOSession(ctx, row); err != nil {
		return nil, err
	}

	if _, err := s.broker.Invoke(ctx, "db.user.create", map[string]any{
		"username": username, "host": "localhost", "password": password,
	}); err != nil {
		_ = s.ssoRepo.DeleteSSOSession(ctx, uid)
		return nil, err
	}
	// Scoped to this one database. A hand-off must never be a way to reach a
	// database the operator did not open.
	if _, err := s.broker.Invoke(ctx, "db.grant", map[string]any{
		"database": rec.Name, "username": username, "host": "localhost",
		"privileges": []string{"ALL"},
	}); err != nil {
		_, _ = s.broker.Invoke(ctx, "db.user.drop", map[string]any{
			"username": username, "host": "localhost",
		})
		_ = s.ssoRepo.DeleteSSOSession(ctx, uid)
		return nil, err
	}

	return &SSOSession{
		URL:      s.adminerURL,
		Driver:   "server", // Adminer's name for MySQL/MariaDB
		Server:   "localhost",
		Database: rec.Name,
		Username: username,
		Password: password,
		// RFC3339 so a browser can compare it without guessing a format.
		ExpiresAt: expires.Format(time.RFC3339),
	}, nil
}

// SweepSSO drops every expired hand-off account. Returns how many were removed.
func (s *Service) SweepSSO(ctx context.Context) (int, error) {
	if s.ssoRepo == nil {
		return 0, nil
	}
	expired, err := s.ssoRepo.ListExpiredSSOSessions(ctx, time.Now().UTC().Format(sqlTime))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range expired {
		// Belt and braces: only ever drop accounts this panel minted. A row that
		// somehow named a real user must not turn the sweeper into a weapon.
		if !strings.HasPrefix(row.Username, ssoUserPrefix) {
			continue
		}
		if s.broker != nil {
			if _, err := s.broker.Invoke(ctx, "db.user.drop", map[string]any{
				"username": row.Username, "host": "localhost",
			}); err != nil {
				// Leave the row: the next sweep retries. Dropping it here would
				// strand a live account with nothing tracking it.
				continue
			}
		}
		if err := s.ssoRepo.DeleteSSOSession(ctx, row.UID); err == nil {
			n++
		}
	}
	return n, nil
}

// RunSSOSweeper drops expired hand-off accounts until ctx is cancelled. It
// sweeps once on startup, which also cleans up after a panel restart that left
// sessions behind.
func (s *Service) RunSSOSweeper(ctx context.Context, log *slog.Logger) {
	t := time.NewTicker(SSOSweepInterval)
	defer t.Stop()
	for {
		if n, err := s.SweepSSO(ctx); err != nil && log != nil {
			log.Warn("database sign-on sweep failed", "err", err)
		} else if n > 0 && log != nil {
			log.Info("dropped expired database sign-on accounts", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// randomPassword returns a password with no shell- or SQL-awkward characters,
// which keeps it safe to round-trip through an HTML form and a GRANT.
func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "password_gen_failed",
			"Could not generate a session password.")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
