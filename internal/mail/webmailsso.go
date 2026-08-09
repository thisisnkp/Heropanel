package mail

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
)

// Passwordless webmail sign-on via a Dovecot MASTER user.
//
// The panel never stores or learns a mailbox password (Roundcube authenticates
// each user against Dovecot directly). To open a mailbox's webmail without one,
// the panel mints a *one-time* Dovecot master credential: a random per-session
// master user + a random password, bound to exactly one mailbox and expiring in
// minutes. Dovecot's master passdb lets `mailbox*master` + that password log in
// AS the mailbox. The plaintext goes straight to the browser to be POSTed at
// Roundcube's login form and is never stored — only its bcrypt hash is, so the
// panel can re-render the master passwd-file, and only the sweeper can drop it.
//
// This is the same shape as the database sign-on hand-off: a throwaway,
// short-lived, single-target credential rather than a standing secret, so the
// blast radius of a leaked hand-off is one mailbox for a few minutes.

const (
	// WebmailSSOTTL is how long a hand-off master credential lives.
	WebmailSSOTTL = 10 * time.Minute
	// WebmailSSOSweep is how often expired master users are pruned.
	WebmailSSOSweep = 5 * time.Minute
	// ssoMasterPrefix marks master users this panel owns.
	ssoMasterPrefix = "npsso_"
)

// WebmailSSORecord is one live master-user session. It holds the bcrypt hash of
// the one-time password (never the plaintext) so the master file can be
// re-rendered from the live set.
type WebmailSSORecord struct {
	ID         int64  `db:"id"`
	UID        string `db:"uid"`
	Mailbox    string `db:"mailbox"`
	MasterUser string `db:"master_user"`
	PWHash     string `db:"pw_hash"`
	CreatedAt  string `db:"created_at"`
	ExpiresAt  string `db:"expires_at"`
}

// WebmailSSORepo is the persistence contract for hand-off sessions.
type WebmailSSORepo interface {
	InsertWebmailSSO(ctx context.Context, r *WebmailSSORecord) error
	ListActiveWebmailSSO(ctx context.Context, now string) ([]WebmailSSORecord, error)
	DeleteExpiredWebmailSSO(ctx context.Context, now string) (int64, error)
}

// WebmailHandoff is the one-time credential the browser POSTs at Roundcube.
type WebmailHandoff struct {
	URL       string `json:"url"`  // Roundcube login URL
	User      string `json:"user"` // mailbox*master (the _user field)
	Pass      string `json:"pass"` // one-time password (the _pass field)
	ExpiresAt string `json:"expires_at"`
}

// WithWebmailSSO enables webmail SSO: the Roundcube URL to hand off to and the
// store that tracks the one-time master users. Without it, StartWebmailSSO
// reports "unavailable".
func (s *Service) WithWebmailSSO(url string, repo WebmailSSORepo) *Service {
	s.webmailURL = strings.TrimSpace(url)
	s.ssoRepo = repo
	return s
}

// WebmailSSOAvailable reports whether SSO can run.
func (s *Service) WebmailSSOAvailable() bool {
	return s.Available() && s.webmailURL != "" && s.ssoRepo != nil
}

// StartWebmailSSO mints a one-time master credential for the mailbox named by
// the account UID and returns the hand-off for the browser. It re-renders the
// complete master passwd-file (declarative), so the new session takes effect and
// expired ones are dropped in the same apply.
func (s *Service) StartWebmailSSO(ctx context.Context, accountUID string) (*WebmailHandoff, error) {
	if !s.WebmailSSOAvailable() {
		return nil, errx.New(errx.KindUnavailable, "webmail_sso_unavailable",
			"Webmail SSO needs the broker, a webmail hostname and a datastore.")
	}
	addr, err := s.repo.AccountAddress(ctx, accountUID)
	if err != nil {
		return nil, err
	}

	uid := idgen.NewULID()
	master := ssoMasterPrefix + strings.ToLower(uid[len(uid)-12:])
	password, err := ssoPassword()
	if err != nil {
		return nil, err
	}
	hash, err := s.hash(password)
	if err != nil {
		return nil, errx.Internal(err)
	}
	expires := time.Now().UTC().Add(WebmailSSOTTL)

	// Record before applying, so a crash leaves a row the sweeper cleans up rather
	// than a master user in the file that nothing tracks.
	if err := s.ssoRepo.InsertWebmailSSO(ctx, &WebmailSSORecord{
		UID: uid, Mailbox: addr, MasterUser: master, PWHash: hash,
		ExpiresAt: expires.Format(sqlTimeMail),
	}); err != nil {
		return nil, err
	}
	if err := s.applyMasterFile(ctx); err != nil {
		return nil, err
	}
	return &WebmailHandoff{
		URL:       s.webmailURL,
		User:      addr + "*" + master,
		Pass:      password,
		ExpiresAt: expires.Format(time.RFC3339),
	}, nil
}

// applyMasterFile re-renders the master passwd-file from the live (non-expired)
// sessions and hands it to the broker.
func (s *Service) applyMasterFile(ctx context.Context) error {
	now := time.Now().UTC().Format(sqlTimeMail)
	rows, err := s.ssoRepo.ListActiveWebmailSSO(ctx, now)
	if err != nil {
		return err
	}
	if _, err := s.broker.Invoke(ctx, "mail.sso.apply", map[string]any{
		"master": renderMasterFile(rows),
	}); err != nil {
		return err
	}
	return nil
}

// renderMasterFile renders the Dovecot master passwd-file: one `user:hash` line
// per live session, deterministically ordered. Empty when there are none, which
// disables SSO entirely (the master passdb becomes inert).
func renderMasterFile(rows []WebmailSSORecord) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, r.MasterUser+":"+r.PWHash)
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// SweepWebmailSSO drops expired master users and, if any were removed,
// re-renders the master file so they can no longer authenticate.
func (s *Service) SweepWebmailSSO(ctx context.Context) (int, error) {
	if s.ssoRepo == nil {
		return 0, nil
	}
	now := time.Now().UTC().Format(sqlTimeMail)
	n, err := s.ssoRepo.DeleteExpiredWebmailSSO(ctx, now)
	if err != nil {
		return 0, err
	}
	if n > 0 && s.broker != nil {
		if err := s.applyMasterFile(ctx); err != nil {
			return int(n), err
		}
	}
	return int(n), nil
}

// RunWebmailSSOSweeper prunes expired master users until ctx is cancelled,
// sweeping once on startup (which also cleans up after a panel restart).
func (s *Service) RunWebmailSSOSweeper(ctx context.Context, log *slog.Logger) {
	t := time.NewTicker(WebmailSSOSweep)
	defer t.Stop()
	for {
		if n, err := s.SweepWebmailSSO(ctx); err != nil && log != nil {
			log.Warn("webmail SSO sweep failed", "err", err)
		} else if n > 0 && log != nil {
			log.Info("dropped expired webmail SSO master users", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ssoPassword returns a form-safe one-time password.
func ssoPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", errx.Wrap(err, errx.KindInternal, "password_gen_failed", "Could not generate a session password.")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sqlTimeMail is the timestamp layout both SQLite and MariaDB accept.
const sqlTimeMail = "2006-01-02 15:04:05"
