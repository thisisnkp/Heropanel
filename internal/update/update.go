package update

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/internal/installer"
	"github.com/thisisnkp/nexpanel/pkg/edkey"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
	"github.com/thisisnkp/nexpanel/pkg/semver"
)

// Update states, as persisted in panel_updates.state.
const (
	StateStaged     = "staged"      // artifacts downloaded and verified, not yet applied
	StateApplying   = "applying"    // handed to the installer; npd is about to be restarted
	StateSucceeded  = "succeeded"   // the new version came up and answered
	StateFailed     = "failed"      // could not apply, and could not be rolled back either
	StateRolledBack = "rolled_back" // the new version did not come up; the old one was restored
)

// Row is one attempt to move this installation to another release.
type Row struct {
	ID          int64
	UID         string
	FromVersion string
	ToVersion   string
	Channel     string
	State       string
	Error       string
	StartedAt   string
	FinishedAt  string // empty while in flight
}

// Repo persists update attempts (implemented by internal/repository).
type Repo interface {
	Insert(ctx context.Context, r *Row) error
	Latest(ctx context.Context) (*Row, error)
	List(ctx context.Context, limit int) ([]Row, error)
	SetState(ctx context.Context, uid, state, errMsg string, finished bool) error
}

// Gateway is the broker surface this needs — the same interface every other
// privileged service takes.
type Gateway interface {
	Invoke(ctx context.Context, capability string, input any) (map[string]any, error)
}

// Snapshotter takes a panel database backup before an update is applied.
// Satisfied by *backup.Service.
type Snapshotter interface {
	PanelAvailable() bool
	CreatePanelBackup(ctx context.Context) (*BackupRef, error)
}

// BackupRef is the little the updater needs to know about a snapshot it took.
type BackupRef struct{ UID string }

// Status is what the panel shows an operator.
type Status struct {
	Current   string `json:"current"`
	Channel   string `json:"channel"`
	Available string `json:"available,omitempty"`
	Notes     string `json:"notes,omitempty"`
	UpToDate  bool   `json:"up_to_date"`
	// Configured is false when no release source or no pinned key is set, in
	// which case the panel shows why rather than an inert button.
	Configured bool   `json:"configured"`
	Reason     string `json:"reason,omitempty"`
	Last       *Row   `json:"-"`
	LastState  string `json:"last_state,omitempty"`
	LastTarget string `json:"last_target,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

// Config is the operator's self-update settings, mirrored from config.Update.
type Config struct {
	Channel string
	BaseURL string
	PubKey  string
}

// Service checks for, downloads, and hands off panel updates.
type Service struct {
	repo    Repo
	gw      Gateway
	snap    Snapshotter
	cfg     Config
	pub     ed25519.PublicKey
	version string
	dataDir string
	http    *http.Client
	log     *slog.Logger
}

// NewService wires the updater. A bad or missing public key is not an error
// here: it leaves the service *unconfigured*, which the UI reports as a reason
// rather than an outage — a panel with no release key must keep working, it
// just must not self-update.
func NewService(repo Repo, gw Gateway, cfg Config, version, dataDir string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Channel == "" {
		cfg.Channel = ChannelStable
	}
	s := &Service{
		repo: repo, gw: gw, cfg: cfg, version: version, dataDir: dataDir, log: log,
		// Generous, because this fetches binaries; the per-attempt retry in
		// download() is what handles a flaky link, not a longer single timeout.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
	if cfg.PubKey != "" {
		if pub, err := edkey.PublicKey(cfg.PubKey); err == nil {
			s.pub = pub
		} else {
			log.Warn("self-update: the pinned release key is unreadable; updates are disabled", "err", err)
		}
	}
	return s
}

// WithSnapshotter enables the pre-update panel database backup. Optional in the
// same way every other cross-module dependency here is: without it an update
// still runs, it just has no snapshot to fall back on if a migration goes wrong.
func (s *Service) WithSnapshotter(sn Snapshotter) *Service {
	s.snap = sn
	return s
}

// WithHTTPClient overrides the HTTP client (tests point it at a local server).
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	if c != nil {
		s.http = c
	}
	return s
}

// Version is the release this process is running.
func (s *Service) Version() string { return s.version }

// configuredErr explains why self-update is off, or nil when it is on. Both
// halves are required: a release source with no key would let whoever serves
// that URL replace the root component on this host, and a key with no source
// has nothing to verify.
func (s *Service) configuredErr() error {
	if s == nil || s.repo == nil {
		return errx.New(errx.KindUnavailable, "update_unavailable",
			"Self-update is unavailable because the panel has no datastore.")
	}
	if strings.TrimSpace(s.cfg.BaseURL) == "" {
		return errx.New(errx.KindUnavailable, "update_source_unset",
			"No release source is configured (update.base_url / NP_UPDATE_BASE_URL).")
	}
	if s.pub == nil {
		return errx.New(errx.KindUnavailable, "update_key_unset",
			"No release public key is pinned (NP_RELEASE_PUBKEY), so a downloaded release could not be trusted.")
	}
	if !ValidChannel(s.cfg.Channel) {
		return errx.New(errx.KindValidation, "invalid_channel",
			fmt.Sprintf("Unknown release channel %q — expected stable, beta or nightly.", s.cfg.Channel))
	}
	return nil
}

// Status reports the running version, the configured channel, and — when a
// check succeeds — whatever that channel currently offers. A failed check is
// not an error: the panel is not broken because a release server is
// unreachable, so the status simply carries the reason.
func (s *Service) Status(ctx context.Context) *Status {
	st := &Status{Current: s.version, Channel: s.cfg.Channel, UpToDate: true}
	if s != nil && s.repo != nil {
		if last, err := s.repo.Latest(ctx); err == nil && last != nil {
			st.Last, st.LastState, st.LastTarget, st.LastError = last, last.State, last.ToVersion, last.Error
		}
	}
	if err := s.configuredErr(); err != nil {
		st.Reason = reason(err)
		return st
	}
	st.Configured = true

	rel, err := s.check(ctx)
	if err != nil {
		st.Reason = reason(err)
		return st
	}
	st.Notes = rel.Notes
	if semver.Newer(s.version, rel.Version) {
		st.Available, st.UpToDate = rel.Version, false
	}
	return st
}

// check fetches and verifies the channel manifest, returning what this panel's
// channel currently offers.
func (s *Service) check(ctx context.Context) (ChannelRelease, error) {
	base := strings.TrimSuffix(s.cfg.BaseURL, "/")
	raw, err := s.fetch(ctx, base+"/channels.json")
	if err != nil {
		return ChannelRelease{}, err
	}
	sig, err := s.fetch(ctx, base+"/channels.json.sig")
	if err != nil {
		return ChannelRelease{}, err
	}
	m, err := ParseManifest(raw, string(sig), s.pub)
	if err != nil {
		return ChannelRelease{}, err
	}
	return m.Release(s.cfg.Channel)
}

// Check forces a manifest fetch and reports what the channel offers.
func (s *Service) Check(ctx context.Context) (*Status, error) {
	if err := s.configuredErr(); err != nil {
		return nil, err
	}
	return s.Status(ctx), nil
}

// releaseVerifier is the signature/checksum half, kept as a named indirection so
// the staging tests can assert it is the installer's implementation that runs
// and not a second copy.
var (
	verifyDetachedSig = installer.VerifyDetachedSig
	verifyChecksum    = installer.VerifyChecksum
)

// ParseReleaseKey exposes key parsing for callers that need to validate an
// operator-supplied key before storing it.
func ParseReleaseKey(s string) (ed25519.PublicKey, error) { return edkey.PublicKey(s) }

// newUID mints an update record id, using the same generator as every other
// resource here.
func newUID() string { return idgen.NewULID() }
