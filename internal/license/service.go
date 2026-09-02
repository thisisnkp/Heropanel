package license

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/thisisnkp/nexpanel/pkg/errx"
)

// Service is the panel's licence client.
//
// A nil *Service means licensing is not configured, and every gate below
// allows. That is the development and self-build case, and it is safe to leave
// open because it is not a bypass anybody gains anything by: a build with no
// pinned key already trusts no licence server, so there is nothing for it to
// enforce. Official builds pin a key at compile time and bootstrap always
// constructs the service for them.
type Service struct {
	store   *Store
	client  *Client
	ring    Keyring
	pinned  bool
	version string
	log     *slog.Logger

	// now is injectable so the ladder, the store and the loop can all be driven
	// through a month of calendar in a millisecond of test.
	now func() time.Time

	// refresh carries a manual "check now" from the UI or the CLI into the
	// background loop. Buffered by one: a second press while the first is still
	// running is the same request, not a queue.
	refresh chan struct{}
}

// Options configure the client.
type Options struct {
	// Dir is where the lease and local state live (default /var/lib/nexpanel).
	Dir string
	// ServerURL is the licence server root.
	ServerURL string
	// ExtraKeys are trusted **only** when this binary pins none. See LoadKeyring.
	ExtraKeys map[string]string
	Version   string
	Logger    *slog.Logger
	Now       func() time.Time
}

// New builds the client. It does not talk to the network.
func New(o Options) (*Service, error) {
	ring, pinned, err := LoadKeyring(o.ExtraKeys)
	if err != nil {
		return nil, err
	}
	if len(ring) == 0 {
		return nil, errors.New("licence: no signing key is pinned and none is configured")
	}
	dir := o.Dir
	if dir == "" {
		dir = "/var/lib/nexpanel"
	}
	store, err := NewStore(dir, ring)
	if err != nil {
		return nil, err
	}
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:   store,
		client:  NewClient(o.ServerURL),
		ring:    ring,
		pinned:  pinned,
		version: o.Version,
		log:     log,
		now:     now,
		refresh: make(chan struct{}, 1),
	}, nil
}

// Pinned reports whether the verification key came from the binary rather than
// from configuration. False means this is a development build, which the
// bootstrap logs loudly — a panel that would trust a key someone can edit is
// not enforcing anything, and pretending otherwise in the UI would be a lie.
func (s *Service) Pinned() bool { return s != nil && s.pinned }

// Status is the current position on the ladder.
func (s *Service) Status() Status {
	if s == nil {
		return Status{State: StateActive, Activated: true, Reason: "licensing is not configured in this build"}
	}
	now := s.now()
	return Evaluate(s.store.Snapshot(now), now)
}

// LastContact is when the licence server was last reached. Zero means never.
func (s *Service) LastContact() time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.store.LastHeartbeat()
}

// ── the gates ───────────────────────────────────────────────────────────────
//
// Each of these evaluates the ladder for itself rather than consulting a shared
// `licensed` boolean, and each is called from its own feature handler. That is
// deliberate and it is the difference between a licence check and a licence
// *speed bump*: one central IsLicensed() is one function to patch out, one
// branch to invert, one return value to pin to true — and the whole product is
// unlocked. Five independent evaluations at five call sites have to be found
// and defeated five times, and a miss leaves the feature still gated.
//
// Every one of them refuses only *creation*. Nothing here can stop a site
// serving, a database answering, mail flowing or a backup running.

// CanCreateSite gates new sites: on the ladder, and on the plan's count.
func (s *Service) CanCreateSite(existing func() int) error {
	if s == nil {
		return nil
	}
	now := s.now()
	st := Evaluate(s.store.Snapshot(now), now)
	if err := creationAllowed(st, "sites"); err != nil {
		return err
	}
	return withinLimit(existing, st.Limits.Sites, "site", st.Plan)
}

// CanCreateDatabase gates new databases.
func (s *Service) CanCreateDatabase(existing func() int) error {
	if s == nil {
		return nil
	}
	now := s.now()
	st := Evaluate(s.store.Snapshot(now), now)
	if err := creationAllowed(st, "databases"); err != nil {
		return err
	}
	return withinLimit(existing, st.Limits.DBs, "database", st.Plan)
}

// CanCreateUser gates new panel users.
func (s *Service) CanCreateUser(existing func() int) error {
	if s == nil {
		return nil
	}
	now := s.now()
	st := Evaluate(s.store.Snapshot(now), now)
	if err := creationAllowed(st, "users"); err != nil {
		return err
	}
	return withinLimit(existing, st.Limits.Users, "panel user", st.Plan)
}

// CanUseDocker gates Docker deploys. A container already running is never
// touched — this refuses to create or pull, nothing more.
func (s *Service) CanUseDocker() error {
	if s == nil {
		return nil
	}
	now := s.now()
	st := Evaluate(s.store.Snapshot(now), now)
	if err := creationAllowed(st, "Docker deploys"); err != nil {
		return err
	}
	return featureAllowed(st, "docker", "Docker")
}

// CanUseAI gates the AI features.
//
// There is no AI surface in the panel yet, so this currently has no call site.
// It is here because the licence carries the entitlement and the plan sells it;
// the alternative is discovering at the point of writing that feature that the
// gate has to be designed then, under time pressure, by someone who did not
// write this file.
func (s *Service) CanUseAI() error {
	if s == nil {
		return nil
	}
	now := s.now()
	st := Evaluate(s.store.Snapshot(now), now)
	if err := creationAllowed(st, "AI features"); err != nil {
		return err
	}
	return featureAllowed(st, "ai", "AI")
}

// Limits is what the plan entitles, for display beside the current counts.
func (s *Service) Limits() Limits {
	if s == nil {
		return Limits{}
	}
	return s.Status().Limits
}

// creationAllowed is the ladder half of every gate. Separate from the limit
// check so the two refusals read differently: one says renew, the other says
// upgrade, and telling a customer to renew a current licence sends them to
// support for nothing.
func creationAllowed(st Status, what string) error {
	switch st.State {
	case StateActive, StateGrace:
		return nil
	case StateDegraded:
		return errx.New(errx.KindPaymentRequired, "license_degraded",
			"Creating "+what+" is paused because this licence has expired. "+
				"Your websites, mail and backups are unaffected. Renew to resume.")
	default:
		if !st.Activated {
			return errx.New(errx.KindPaymentRequired, "license_required",
				"This NexPanel install has not been activated. Enter a licence key to continue.")
		}
		return errx.New(errx.KindPaymentRequired, "license_locked",
			"This licence has lapsed, so the panel is limited to reactivation. "+
				"Your websites and services keep running.")
	}
}

// withinLimit compares a count against the plan.
//
// The count arrives as a function, not a number, so it is only taken when the
// ladder has already allowed the action. Counting first would mean a locked
// panel querying every site on the box on its way to refusing — work done to
// reach a decision that had already been made, and on the one code path where
// the datastore is least likely to be healthy.
//
// A limit of zero for a plan we know the name of is a real limit of none; a
// zero on a licence with no plan is an unknown, and an unknown must not lock a
// customer out of their own panel.
func withinLimit(existing func() int, limit int, noun, plan string) error {
	if limit <= 0 && plan == "" {
		return nil
	}
	count := 0
	if existing != nil {
		count = existing()
	}
	if count < limit {
		return nil
	}
	return errx.New(errx.KindPaymentRequired, "license_limit_reached",
		fmt.Sprintf("The %s plan covers %d %s%s, and this server has %d. Upgrade to add more.",
			plan, limit, noun, plural(limit), count))
}

func featureAllowed(st Status, feature, label string) error {
	if len(st.Features) == 0 {
		// A licence that lists no features at all is one we could not read the
		// entitlements of — a development build, or an older server. Refusing
		// every feature on that basis would break more than it protects.
		return nil
	}
	for _, f := range st.Features {
		if strings.EqualFold(f, feature) {
			return nil
		}
	}
	return errx.New(errx.KindPaymentRequired, "license_feature",
		label+" is not included in the "+st.Plan+" plan.")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ── operations ──────────────────────────────────────────────────────────────

// Activate binds this machine to a licence key.
func (s *Service) Activate(ctx context.Context, key string) (Status, error) {
	if s == nil {
		return Status{}, errors.New("licensing is not configured in this build")
	}
	fp := Collect()
	now := s.now()

	res, err := s.client.Activate(ctx, key, fp, hostname(), osName(), s.version)
	if err != nil {
		return Status{}, err
	}
	// The server's word for it is not enough. The token is verified against the
	// pinned key before a byte of it is written, so a server that has been
	// impersonated hands over something this panel refuses rather than
	// something it stores and trusts.
	claims, err := s.ring.Verify(res.Token)
	if err != nil {
		return Status{}, fmt.Errorf("the licence server returned a token this panel cannot verify: %w", err)
	}
	if claims.FP != fp.Hash {
		return Status{}, errors.New("the licence server issued a token for a different machine")
	}
	if err := s.store.SaveActivation(res.Token, claims.LID, fp.Hash, res.InstallSecret, claims, now); err != nil {
		return Status{}, err
	}
	s.log.Info("licence activated", "lid", claims.LID, "plan", claims.Plan,
		"reactivated", res.Reactivated, "expires", claims.ExpiresAt())
	return s.Status(), nil
}

// Refresh performs one heartbeat now. Used by the CLI and by the panel's
// "Refresh licence" button.
func (s *Service) Refresh(ctx context.Context) (Status, error) {
	if s == nil {
		return Status{}, errors.New("licensing is not configured in this build")
	}
	if err := s.beat(ctx); err != nil {
		return s.Status(), err
	}
	return s.Status(), nil
}

// Deactivate releases this machine's slot on the licence.
//
// The local state is cleared **only if the server agreed**. Clearing it on a
// failed call would leave a slot held on the server that this machine can no
// longer release — the customer would have to phone support to get back
// something they were trying to give away.
func (s *Service) Deactivate(ctx context.Context) error {
	if s == nil {
		return errors.New("licensing is not configured in this build")
	}
	lid, fp, secret, err := s.store.Identity()
	if err != nil {
		return err
	}
	if err := s.client.Deactivate(ctx, lid, fp, secret); err != nil {
		return err
	}
	s.log.Info("licence deactivated", "lid", lid)
	return s.store.Clear()
}

// beat is one heartbeat: refresh the lease, act on any command.
func (s *Service) beat(ctx context.Context) error {
	lid, fp, secret, err := s.store.Identity()
	if err != nil {
		return err
	}
	now := s.now()
	res, err := s.client.Heartbeat(ctx, lid, fp, secret, hostname(), osName(), s.version)
	if err != nil {
		return err
	}

	for _, cmd := range res.Commands {
		switch cmd {
		case CmdRevoke:
			// Recorded locally so it survives a restart. The panel locks; every
			// site on the box keeps serving.
			if err := s.store.MarkRevoked(now); err != nil {
				s.log.Error("could not record licence revocation", "err", err)
			}
			s.log.Warn("licence revoked by the licence server", "lid", lid)
		case CmdPlanChanged:
			s.log.Info("licence plan changed", "lid", lid, "plan", res.License.Plan)
		case CmdForceUpdate:
			s.log.Warn("this panel is older than the licence's minimum version", "version", s.version)
		}
	}

	if res.Token == "" {
		// A revoked licence gets commands and no lease. Not an error: the
		// server answered, and the panel now knows exactly where it stands.
		return nil
	}
	claims, err := s.ring.Verify(res.Token)
	if err != nil {
		return fmt.Errorf("the licence server returned a token this panel cannot verify: %w", err)
	}
	if claims.FP != fp {
		return errors.New("the licence server issued a token for a different machine")
	}
	return s.store.SaveToken(res.Token, claims, now)
}

// ── the background loop ─────────────────────────────────────────────────────

// heartbeatInterval and heartbeatJitter: once a day, spread over four hours.
//
// The spread is the point. Ten thousand panels installed by the same script and
// restarted by the same package upgrade would otherwise beat within the same
// second, turning a daily formality into a daily thundering herd. Drawn per
// cycle rather than once, so the fleet keeps re-spreading instead of settling
// back into lockstep after a synchronised restart.
const (
	heartbeatInterval = 24 * time.Hour
	heartbeatJitter   = 4 * time.Hour
	// retryInterval applies when the last attempt failed. Sooner than a day,
	// because a panel that missed a beat is a panel walking toward its grace
	// period; still far enough apart to be no burden on a recovering server.
	retryInterval = 1 * time.Hour
)

// Run keeps the lease fresh. Started by bootstrap under safe.Go, so a panic in
// here cannot take npd down.
//
// It beats on boot and then daily, and it never treats a failure as a licence
// problem: the ladder is computed from the token on disk, and this loop only
// ever tries to replace it with a newer one.
func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	// A short settle before the first beat. On a host that just booted, DNS and
	// the default route are often not up for a second or two, and a first
	// attempt that fails for that reason would push the next one an hour out.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.refresh:
		case <-timer.C:
		}

		// Advance the monotonic floor whether or not the beat succeeds. An
		// installation that has been running offline for a week must not be
		// walkable back to where it started just because it never reached the
		// server.
		s.store.Touch(s.now())

		next := heartbeatInterval
		switch err := s.beat(ctx); {
		case err == nil:
		case errors.Is(err, ErrNotActivated):
			// Nothing to refresh until somebody enters a key. Check back on the
			// normal cadence rather than hourly: the CLI and the UI both
			// trigger a refresh the moment activation happens.
		case ctx.Err() != nil:
			return
		default:
			s.log.Warn("licence heartbeat failed", "err", err,
				"note", "the panel keeps running on its stored lease")
			next = retryInterval
		}

		timer.Reset(next + time.Duration(randInt63n(int64(heartbeatJitter))))
	}
}

// Trigger asks the background loop to beat now. Non-blocking: a second press
// while one is in flight is the same request, not a queue.
func (s *Service) Trigger() {
	if s == nil {
		return
	}
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// osName is the distribution's own name, read from os-release. Display only —
// it tells support what they are looking at.
func osName() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}
