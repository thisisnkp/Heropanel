package license

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Where the lease and the local state live. Both under the panel's own data
// directory, which npd's systemd unit already grants ReadWritePaths on — a
// third location would need a unit change nobody would remember to make.
const (
	// TokenFile holds the signed lease. 0600: it is not a secret in the sense
	// that forging one is impossible without the server's private key, but it
	// names the account and the plan, and there is no reason for it to be
	// world-readable on a shared box.
	TokenFile = "license.token"
	// StateFile is the local anti-rollback state and the heartbeat credential.
	//
	// The two live in one file on purpose. The state file is what stops the
	// grace period being extended by moving the system clock, so it is the file
	// somebody trying to cheat will delete — and because the installation
	// secret is in it, deleting it costs them the ability to heartbeat at all.
	// Recovering from that needs the licence key, which is exactly the person
	// who is entitled to do it. Cheating and recovery use the same door, and
	// only the owner has the handle.
	StateFile = ".lstate"
)

// ErrNotActivated is returned by operations that need a licence this
// installation has never held.
var ErrNotActivated = errors.New("this installation has not been activated")

// Store persists the lease and the local state.
//
// Every write is atomic — a temp file in the same directory, then rename — so a
// power cut cannot leave a half-written token that reads as tampering, or a
// half-written state file that loses the monotonic clock.
type Store struct {
	dir  string
	ring Keyring

	mu    sync.Mutex
	cache *persisted
}

// persisted is the on-disk shape of the state file. Unix seconds rather than
// RFC 3339 strings: this file is compared against a clock, and a format that
// needs parsing to compare is a format that can be made to fail to parse.
type persisted struct {
	LID           string `json:"lid,omitempty"`
	Fingerprint   string `json:"fp,omitempty"`
	InstallSecret string `json:"install_secret,omitempty"`
	// LastSeen is the furthest forward this installation has ever known the
	// time to be. It only ever increases.
	LastSeen int64 `json:"last_seen"`
	// TokenIAT is the issue time of the lease currently on disk.
	//
	// Separate from LastSeen, and the distinction matters: LastSeen is about
	// *the clock* and advances every time the panel runs, while this is about
	// *the lease* and only moves when a newer one is accepted. Judging a token's
	// freshness against LastSeen — which is the obvious-looking shortcut — makes
	// every valid lease condemn itself as a replay the moment the clock passes
	// its issue time, which is a second after it is written.
	TokenIAT      int64 `json:"token_iat,omitempty"`
	ActivatedAt   int64 `json:"activated_at,omitempty"`
	LastHeartbeat int64 `json:"last_heartbeat,omitempty"`
	RevokedAt     int64 `json:"revoked_at,omitempty"`
	TamperedSince int64 `json:"tampered_since,omitempty"`
}

// NewStore opens (and creates) the licence state directory.
func NewStore(dir string, ring Keyring) (*Store, error) {
	if dir == "" {
		return nil, errors.New("licence store: no directory")
	}
	// 0700: the state file inside carries the heartbeat credential.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("licence store: %w", err)
	}
	return &Store{dir: dir, ring: ring}, nil
}

func (s *Store) tokenPath() string { return filepath.Join(s.dir, TokenFile) }
func (s *Store) statePath() string { return filepath.Join(s.dir, StateFile) }

// state reads the state file, caching it. A missing file is an empty state, not
// an error: that is what a fresh install looks like.
func (s *Store) state() *persisted {
	if s.cache != nil {
		return s.cache
	}
	p := &persisted{}
	if b, err := os.ReadFile(s.statePath()); err == nil {
		// A corrupt state file is treated as empty rather than fatal. The
		// consequence is a lost monotonic floor, which is a weakening; the
		// alternative is a panel that will not start because a JSON file has a
		// stray byte, which is an outage.
		_ = json.Unmarshal(b, p)
	}
	s.cache = p
	return p
}

func (s *Store) writeState(p *persisted) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.statePath(), b, 0o600); err != nil {
		return err
	}
	s.cache = p
	return nil
}

// Snapshot reads everything the ladder needs, verifying the token on the way.
//
// `now` is the caller's clock. It is used to advance the monotonic floor, so
// calling Snapshot regularly is what keeps the floor honest — and the panel
// calls it on every gated action.
func (s *Store) Snapshot(now time.Time) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.state()
	snap := Snapshot{
		Activated: st.ActivatedAt > 0,
		LastSeen:  unixOrZero(st.LastSeen),
		RevokedAt: unixOrZero(st.RevokedAt),
	}

	raw, err := os.ReadFile(s.tokenPath())
	switch {
	case err != nil:
		// Missing or unreadable. For a machine that was never activated this is
		// simply "no licence yet"; for one that was, it is tampering, and is
		// recorded as such without telling anyone.
		if snap.Activated {
			snap.TamperedSince = s.noteTamper(st, now)
		}
	default:
		claims, verr := s.ring.Verify(string(raw))
		switch {
		case verr != nil:
			snap.TamperedSince = s.noteTamper(st, now)
		case st.TokenIAT > 0 && claims.IAT < st.TokenIAT:
			// An older lease than the one this installation already accepted:
			// a token copied back over a newer one, usually alongside a clock
			// change. Refused as tampering, and silently — the point of the slow
			// degrade is that whoever is trying gets nothing to iterate against.
			snap.TamperedSince = s.noteTamper(st, now)
		default:
			snap.Claims = &claims
			s.clearTamper(st)
		}
	}

	s.advance(st, now, snap.Claims)
	return snap
}

// noteTamper records when verification first failed, and returns it. Called on
// every read while the trouble persists, so the *first* time is what sticks.
func (s *Store) noteTamper(st *persisted, now time.Time) time.Time {
	if st.TamperedSince == 0 {
		st.TamperedSince = now.Unix()
		_ = s.writeState(st)
	}
	return unixOrZero(st.TamperedSince)
}

func (s *Store) clearTamper(st *persisted) {
	if st.TamperedSince != 0 {
		st.TamperedSince = 0
		_ = s.writeState(st)
	}
}

// advance moves the monotonic floor forward, never back.
//
// It takes the maximum of what is stored, the current clock, and the token's
// own issue time. The last of those is what makes the floor resistant to a
// clock that was wrong from the very first boot: the server's `iat` is a
// timestamp this installation could not have invented.
func (s *Store) advance(st *persisted, now time.Time, claims *Claims) {
	floor := st.LastSeen
	if n := now.Unix(); n > floor {
		floor = n
	}
	if claims != nil && claims.IAT > floor {
		floor = claims.IAT
	}
	if floor > st.LastSeen {
		st.LastSeen = floor
		_ = s.writeState(st)
	}
}

// SaveActivation records a fresh activation: the lease, the credential every
// later heartbeat is signed with, and the identity to heartbeat as.
func (s *Store) SaveActivation(token, lid, fingerprint, installSecret string, claims Claims, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := writeFileAtomic(s.tokenPath(), []byte(token), 0o600); err != nil {
		return err
	}
	st := s.state()
	st.LID, st.Fingerprint, st.InstallSecret = lid, fingerprint, installSecret
	st.ActivatedAt = now.Unix()
	st.LastHeartbeat = now.Unix()
	st.TokenIAT = claims.IAT
	// A fresh activation clears a tamper record: whatever was wrong with the
	// old token, there is a new one signed by the server in front of us.
	st.TamperedSince = 0
	st.RevokedAt = 0
	s.advanceLocked(st, now, &claims)
	return s.writeState(st)
}

// SaveToken replaces the lease after a heartbeat.
//
// A token older than the floor is refused rather than written. That is the
// same rule Snapshot applies on read, enforced again here so a replayed
// response never reaches the disk in the first place.
func (s *Store) SaveToken(token string, claims Claims, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.state()
	// Two rules, because they catch different things. A lease older than the one
	// we hold is a replay of an old response; a lease issued before a moment
	// this installation has already lived through cannot be fresh whatever it
	// claims. Both are refused here so a replayed response never reaches the
	// disk in the first place.
	if claims.IAT < st.TokenIAT {
		return fmt.Errorf("licence token is older than the one this installation already holds")
	}
	if claims.IAT < st.LastSeen {
		return fmt.Errorf("licence token is older than this installation's last known time")
	}
	if err := writeFileAtomic(s.tokenPath(), []byte(token), 0o600); err != nil {
		return err
	}
	st.LastHeartbeat = now.Unix()
	st.TamperedSince = 0
	st.TokenIAT = claims.IAT
	s.advanceLocked(st, now, &claims)
	return s.writeState(st)
}

func (s *Store) advanceLocked(st *persisted, now time.Time, claims *Claims) {
	if n := now.Unix(); n > st.LastSeen {
		st.LastSeen = n
	}
	if claims != nil && claims.IAT > st.LastSeen {
		st.LastSeen = claims.IAT
	}
}

// Identity is what the heartbeat needs: who we are and what we sign with.
func (s *Store) Identity() (lid, fingerprint, secret string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state()
	if st.LID == "" || st.InstallSecret == "" {
		return "", "", "", ErrNotActivated
	}
	return st.LID, st.Fingerprint, st.InstallSecret, nil
}

// MarkRevoked records that the server pushed a revoke. Persisted because the
// panel must stay locked across a restart — otherwise revocation would last
// until the next `systemctl restart npd`.
func (s *Store) MarkRevoked(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state()
	if st.RevokedAt != 0 {
		return nil
	}
	st.RevokedAt = now.Unix()
	return s.writeState(st)
}

// Touch advances the monotonic floor. Called by the heartbeat loop so an
// installation that is running but offline still cannot be walked backwards.
func (s *Store) Touch(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state()
	s.advanceLocked(st, now, nil)
	_ = s.writeState(st)
}

// LastHeartbeat is when the server was last successfully reached.
func (s *Store) LastHeartbeat() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return unixOrZero(s.state().LastHeartbeat)
}

// Clear forgets the lease and the credential after a successful deactivation.
//
// It deliberately does **not** reset LastSeen. The monotonic floor is a
// property of the machine, not of the licence: letting a deactivate-reactivate
// cycle reset it would make it the cheapest way to unwind the clock.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.tokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	st := s.state()
	// The monotonic floor survives; the lease does not. TokenIAT going with it
	// is right: there is no lease to be older than any more.
	next := &persisted{LastSeen: st.LastSeen}
	return s.writeState(next)
}

// writeFileAtomic writes via a temp file in the same directory and renames.
//
// Same directory because rename is only atomic within a filesystem, and /tmp is
// very often a different one. The mode is set on the temp file before the
// rename, so the final path is never briefly readable by anyone else.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp) // no-op once the rename has succeeded
	}()

	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	// Flushed before the rename: without it a crash can leave the new name
	// pointing at an empty file, which reads as a deleted token.
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func unixOrZero(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}
