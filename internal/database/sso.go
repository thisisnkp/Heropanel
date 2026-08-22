package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
)

// Single sign-on into phpMyAdmin.
//
// The design decision worth stating plainly: NexPanel does not keep database
// user passwords. It could — there is a perfectly good cipher in pkg/secrets and
// most panels do exactly that — but then one panel compromise hands over every
// customer's standing database credentials, and there is no way to tell after
// the fact which ones were used.
//
// So a hand-off mints a throwaway MariaDB account instead: random password,
// granted on exactly one database, dropped a few minutes later by the sweeper.
// Nothing is stored anywhere. The cost is a little machinery (this file and a
// sweep loop); the benefit is that the blast radius of a session is one database
// for fifteen minutes.
//
// The browser never sees those credentials either. It carries a one-time
// **ticket**, and phpMyAdmin's own signon script redeems it against this panel
// over loopback — so the password exists only between npd and phpMyAdmin, both
// on the same host. That also sidesteps phpMyAdmin's CSRF token, which makes a
// blind POST at its login form unreliable in the first place.
const (
	// SSOTTL is how long a hand-off account lives. Long enough to click through
	// a login form, short enough that a leaked credential is nearly worthless.
	SSOTTL = 15 * time.Minute

	// SSOSweepInterval is how often expired accounts are dropped.
	SSOSweepInterval = 5 * time.Minute

	// ssoUserPrefix marks accounts this panel owns, so the sweeper can never
	// mistake a real database user for one of its own.
	ssoUserPrefix = "npsso_"
)

// sqlTime is the timestamp format both SQLite (TEXT) and MariaDB (DATETIME)
// accept, so the service writes times without dialect branching.
const sqlTime = "2006-01-02 15:04:05"

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

// SSOTicketRecord is a pending hand-off. It holds no secret: only the hash of
// the ticket, and which database it opens.
type SSOTicketRecord struct {
	ID           int64  `db:"id"`
	UID          string `db:"uid"`
	DBInstanceID int64  `db:"db_instance_id"`
	TicketHash   string `db:"ticket_hash"`
	ActorUserID  int64  `db:"actor_user_id"`
	CreatedAt    string `db:"created_at"`
	ExpiresAt    string `db:"expires_at"`
}

// SSORepo is the persistence contract for hand-off sessions and tickets.
type SSORepo interface {
	InsertSSOSession(ctx context.Context, r *SSOSessionRecord) error
	ListExpiredSSOSessions(ctx context.Context, now string) ([]SSOSessionRecord, error)
	DeleteSSOSession(ctx context.Context, uid string) error

	InsertSSOTicket(ctx context.Context, r *SSOTicketRecord) error
	// RedeemSSOTicket atomically marks an unredeemed, unexpired ticket used and
	// returns it. The atomicity is the point: two requests arriving with the
	// same ticket must not both get a database account.
	RedeemSSOTicket(ctx context.Context, hash, now string) (*SSOTicketRecord, error)
	DeleteExpiredSSOTickets(ctx context.Context, now string) (int, error)
}

// WithAdminer wires the URL of the database client to hand off to and the store
// that tracks hand-off sessions. Without it, StartSSO reports "unavailable" —
// there is nowhere to hand off to. Returns s for chaining.
//
// The name is historical: NexPanel ships phpMyAdmin. The URL is the same field
// either way, and the hand-off it drives is StartPMASession, not StartSSO.
func (s *Service) WithAdminer(url string, repo SSORepo) *Service {
	s.adminerURL = strings.TrimSpace(url)
	s.ssoRepo = repo
	return s
}

// TicketTTL is how long a phpMyAdmin hand-off ticket lives.
//
// A minute, not fifteen: the ticket is redeemed by the browser navigating
// straight to phpMyAdmin, which takes a second. The account it mints is what
// lasts fifteen minutes.
const TicketTTL = 60 * time.Second

// PMAHandoff is what the panel gives the browser: a URL to open, and nothing
// else. No username, no password — the credentials are minted when phpMyAdmin
// itself redeems the ticket, server-side, over loopback.
type PMAHandoff struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

// PMACredentials is what phpMyAdmin's signon script gets back when it redeems a
// ticket. It is returned exactly once and never stored.
type PMACredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Server   string `json:"server"`
}

// StartPMASession mints a one-time ticket for one database and returns the URL
// to open phpMyAdmin with.
//
// The older hand-off (StartSSO) returned live credentials for the browser to
// POST at a login form. Two things are wrong with that against phpMyAdmin: its
// login form carries a CSRF token, so a blind POST is not reliable; and a
// working database password ends up in the page. Here the browser carries only
// a ticket that is worthless without loopback access to this panel, and the
// password never leaves the host.
func (s *Service) StartPMASession(ctx context.Context, dbUID string, actorUserID int64) (*PMAHandoff, error) {
	if s.adminerURL == "" || s.ssoRepo == nil {
		return nil, errx.New(errx.KindUnavailable, "phpmyadmin_unavailable",
			"phpMyAdmin is not configured. Set database.adminer_url and restart the panel.")
	}
	rec, err := s.getDatabase(ctx, dbUID)
	if err != nil {
		return nil, err
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}

	ticket, err := randomTicket()
	if err != nil {
		return nil, err
	}
	expires := time.Now().UTC().Add(TicketTTL)
	if err := s.ssoRepo.InsertSSOTicket(ctx, &SSOTicketRecord{
		UID: idgen.NewULID(), DBInstanceID: rec.ID, TicketHash: hashTicket(ticket),
		ActorUserID: actorUserID, ExpiresAt: expires.Format(sqlTime),
	}); err != nil {
		return nil, err
	}

	sep := "?"
	if strings.Contains(s.adminerURL, "?") {
		sep = "&"
	}
	return &PMAHandoff{
		URL:       s.adminerURL + sep + "np_ticket=" + url.QueryEscape(ticket),
		ExpiresAt: expires.Format(time.RFC3339),
	}, nil
}

// RedeemPMATicket exchanges a ticket for a throwaway database account.
//
// Called by phpMyAdmin's signon script over loopback, not by a browser — the
// HTTP edge enforces that. Redemption is single-use and atomic in the store, so
// a ticket that somehow leaked cannot be replayed behind the real one.
func (s *Service) RedeemPMATicket(ctx context.Context, ticket string) (*PMACredentials, error) {
	if s.ssoRepo == nil {
		return nil, errx.New(errx.KindUnavailable, "phpmyadmin_unavailable",
			"phpMyAdmin is not configured.")
	}
	if err := s.requireBroker(); err != nil {
		return nil, err
	}
	row, err := s.ssoRepo.RedeemSSOTicket(ctx, hashTicket(ticket), time.Now().UTC().Format(sqlTime))
	if err != nil {
		return nil, err
	}
	if row == nil {
		// One message for expired, already-used and never-existed alike. Telling
		// them apart tells a caller holding a guess whether the guess was close.
		return nil, errx.New(errx.KindForbidden, "ticket_invalid",
			"That sign-on ticket is not valid.")
	}
	rec, err := s.repo.GetDatabaseByID(ctx, row.DBInstanceID)
	if err != nil {
		return nil, err
	}
	return s.mintHandoffAccount(ctx, rec)
}

// mintHandoffAccount creates the throwaway MariaDB account for one database and
// returns its credentials. Shared by every hand-off path so there is one place
// that decides what a hand-off account may do.
func (s *Service) mintHandoffAccount(ctx context.Context, rec *InstanceRecord) (*PMACredentials, error) {
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

	return &PMACredentials{
		Username: username,
		Password: password,
		Database: rec.Name,
		Server:   "localhost",
	}, nil
}

// SweepSSO drops every expired hand-off account. Returns how many were removed.
//
// It also prunes expired tickets. Those hold no secret and grant nothing once
// expired, but a table that only ever grows is a table nobody notices until it
// is large.
func (s *Service) SweepSSO(ctx context.Context) (int, error) {
	if s.ssoRepo == nil {
		return 0, nil
	}
	now := time.Now().UTC().Format(sqlTime)
	if _, err := s.ssoRepo.DeleteExpiredSSOTickets(ctx, now); err != nil {
		// Not fatal: the accounts below are the part that matters, and a ticket
		// left in the table is inert.
		slog.Default().Warn("database: could not prune expired sign-on tickets", "err", err)
	}
	expired, err := s.ssoRepo.ListExpiredSSOSessions(ctx, now)
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

// randomTicket mints a hand-off ticket. 32 bytes, URL-safe: it travels in a
// query string.
func randomTicket() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "ticket_gen_failed",
			"Could not generate a sign-on ticket.")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashTicket is what is stored. Same reasoning as session tokens and API keys:
// a datastore that leaks must not hand anyone a usable ticket.
func hashTicket(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}
