package license

import (
	"time"
)

// State is where this installation sits on the degradation ladder.
//
// The ladder is evaluated **on this machine**, from the token's `exp`. It is
// not a thing the server pushes: a panel with no network must reach the same
// answer as one with, or an outage at the licence server would degrade the
// whole fleet at once.
type State string

const (
	// StateActive — the lease is current. Everything works.
	StateActive State = "active"
	// StateGrace — expired, inside the grace window. Everything still works;
	// the UI carries a warning. This window is the whole reason a licence
	// server outage is not a customer outage.
	StateGrace State = "grace"
	// StateDegraded — creating new things is blocked. Existing sites,
	// databases, mail, cron and backups continue untouched.
	StateDegraded State = "degraded"
	// StateLocked — the panel UI is a reactivation page. Services are still
	// untouched: every site on the box keeps serving traffic.
	StateLocked State = "locked"
)

const (
	// DefaultGrace applies when a token does not carry its own. Fourteen days
	// is long enough that a card that failed on a Friday is a conversation, not
	// an incident.
	DefaultGrace = 14 * 24 * time.Hour

	// DegradedWindow is how long the degraded rung lasts once grace ends. With
	// the default grace this puts `locked` at expiry + 30 days, exactly as the
	// product defines it.
	//
	// Derived from grace rather than a fixed "30 days from expiry" so that a
	// longer grace pushes the whole ladder out instead of eating the degraded
	// window. A support agent who extends someone's grace to 25 days means to
	// be kind; under a fixed 30-day lock they would have shortened that
	// customer's degraded period from 16 days to 5.
	DegradedWindow = 16 * 24 * time.Hour

	// TamperGrace is how long a machine whose token will not verify keeps
	// behaving normally before it starts to degrade. See Snapshot.Tampered.
	TamperGrace = 3 * 24 * time.Hour

	// clockTolerance absorbs ordinary NTP correction. A clock nudged a few
	// seconds is a clock being kept, not a clock being moved.
	clockTolerance = 60 * time.Second
)

// Snapshot is everything the ladder is computed from: the verified claims, plus
// the local anti-rollback state. Kept as a plain value so Evaluate is a pure
// function of it — which is what lets the boundaries be tested at the second
// rather than by waiting a month.
type Snapshot struct {
	// Claims is nil when there is no usable token: never activated, file
	// deleted, or verification failed.
	Claims *Claims
	// Activated is true once this machine has held a licence, even if the token
	// is now unreadable. It is what separates "you have not entered a key" from
	// "something happened to your token".
	Activated bool
	// LastSeen is the furthest forward this installation has ever known the
	// time to be. The clock may not go behind it.
	LastSeen time.Time
	// RevokedAt is set when the server told us so on a heartbeat. Revocation is
	// the one state that is pushed rather than waited out.
	RevokedAt time.Time
	// TamperedSince is when the token first failed to verify or went missing.
	TamperedSince time.Time
}

// Status is the answer the panel, the CLI and the UI all read.
type Status struct {
	State  State  `json:"state"`
	Reason string `json:"reason"`

	Activated bool     `json:"activated"`
	Plan      string   `json:"plan,omitempty"`
	LID       string   `json:"lid,omitempty"`
	Limits    Limits   `json:"limits"`
	Features  []string `json:"features,omitempty"`

	ExpiresAt     time.Time `json:"expires_at,omitzero"`
	GraceUntil    time.Time `json:"grace_until,omitzero"`
	DegradedUntil time.Time `json:"degraded_until,omitzero"`
	// SubscriptionEndsAt is when the *subscription* ends, which is usually much
	// later than the seven-day token. Zero for a perpetual or free licence.
	SubscriptionEndsAt time.Time `json:"subscription_ends_at,omitzero"`

	// ClockRollback is true when this machine's clock is behind the furthest
	// point it has ever reached. Surfaced rather than silently corrected: a
	// server whose clock jumped backwards has a real problem — a bad NTP peer,
	// a hypervisor restoring a snapshot — and the operator should see it.
	ClockRollback bool `json:"clock_rollback"`
	// Tampered is true when the token would not verify. Deliberately not
	// surfaced to an anonymous caller — see the note on Evaluate.
	Tampered bool `json:"-"`
	Revoked  bool `json:"revoked"`
}

// Banner is the one line the panel shows an operator, or "" when there is
// nothing to say. Written to be read by someone who is not thinking about
// licensing at all.
func (s Status) Banner() string {
	switch s.State {
	case StateActive:
		if s.ClockRollback {
			return "This server's clock is behind where the panel last saw it. Check that NTP is running."
		}
		return ""
	case StateGrace:
		return "Your NexPanel licence has expired. Everything still works — renew within " +
			humanDays(time.Until(s.GraceUntil)) + " to avoid interruption."
	case StateDegraded:
		return "Your NexPanel licence has expired. Creating new sites, databases and users is paused. " +
			"Your websites, mail and backups are unaffected."
	case StateLocked:
		if !s.Activated {
			return "This NexPanel install has not been activated yet. Enter your licence key to begin."
		}
		if s.Revoked {
			return "This NexPanel licence has been revoked. Your websites keep running; the panel is limited until it is reactivated."
		}
		return "Your NexPanel licence has lapsed. Your websites keep running; the panel is limited until it is renewed."
	}
	return ""
}

// Evaluate walks the ladder. Pure: same snapshot and same instant, same answer.
//
// Two local defences are folded in here rather than bolted on elsewhere,
// because both are about *what time it is*, which is the only input an attacker
// on the box controls freely:
//
//   - **The clock cannot go backwards.** Time is floored at LastSeen, so
//     setting the system date back to last year does not restore a lapsed
//     licence — it does nothing at all. This is the whole reason .lstate exists.
//   - **Tampering degrades slowly and silently.** A token that will not verify
//     does not produce an error the moment somebody edits it. It behaves
//     normally for three days and then starts down the ladder. Whoever is
//     probing gets no feedback to iterate against, and a genuine corruption —
//     a half-written file after a power cut — has three days to be noticed and
//     fixed by a heartbeat before it costs the customer anything.
func Evaluate(s Snapshot, now time.Time) Status {
	out := Status{Activated: s.Activated}

	// The monotonic floor. Everything below reasons from `at`, never from the
	// wall clock directly.
	at := now
	if !s.LastSeen.IsZero() && at.Before(s.LastSeen) {
		out.ClockRollback = at.Before(s.LastSeen.Add(-clockTolerance))
		at = s.LastSeen
	}

	// Revocation is immediate and is not a ladder position to walk down.
	if !s.RevokedAt.IsZero() {
		out.State, out.Revoked, out.Reason = StateLocked, true, "the licence was revoked"
		out.fill(s.Claims)
		return out
	}

	// The three boundaries the switch below reasons from. Computed once, from
	// either the token or the tamper record, so there is exactly one ladder.
	var exp, graceUntil, degradedUntil time.Time

	if claims := s.Claims; claims != nil {
		exp = claims.ExpiresAt()
		graceUntil = exp.Add(claims.GraceFor())
		degradedUntil = graceUntil.Add(DegradedWindow)
		out.fill(claims)
	} else {
		if !s.Activated {
			out.State, out.Reason = StateLocked, "this installation has not been activated"
			return out
		}
		// Activated, but no usable token: deleted, edited, or a replayed older
		// lease. The machine behaves exactly as it did for TamperGrace and then
		// starts down the ladder.
		out.Tampered = true
		out.Reason = "the licence token could not be verified"
		since := s.TamperedSince
		if since.IsZero() {
			since = at
		}
		exp = since.Add(TamperGrace)
		// No grace rung, on purpose. Grace is a *warning banner*, and a banner
		// appearing the moment somebody edits the token is precisely the
		// feedback the slow degrade exists to withhold. Nothing visible changes
		// until creation stops.
		graceUntil = exp
		degradedUntil = graceUntil.Add(DegradedWindow)
		// The dates are deliberately not published on a tampered install: they
		// would tell a prober exactly when their edit was noticed.
	}

	switch {
	case at.Before(exp):
		out.State = StateActive
		if out.Reason == "" {
			out.Reason = "the licence is current"
		}
		// A clock behind where the panel has already been is not trusted to
		// report `active`. The floor above means this only happens inside the
		// tolerance or when LastSeen is itself ahead, and either way the honest
		// answer is the rung down rather than full function.
		if out.ClockRollback {
			out.State, out.Reason = StateGrace, "this server's clock has moved backwards"
		}
	case at.Before(graceUntil):
		out.State = StateGrace
		if out.Reason == "" {
			out.Reason = "the licence expired and is inside its grace period"
		}
	case at.Before(degradedUntil):
		out.State = StateDegraded
		if out.Reason == "" {
			out.Reason = "the licence expired more than " + humanDays(graceUntil.Sub(exp)) + " ago"
		}
	default:
		out.State = StateLocked
		if out.Reason == "" {
			out.Reason = "the licence lapsed"
		}
	}
	return out
}

// fill copies the entitlements a verified token carries. Never called for a
// synthetic one: a machine whose token will not verify must not be handed the
// plan and limits of a token nobody could read.
func (s *Status) fill(c *Claims) {
	if c == nil {
		return
	}
	s.Plan, s.LID, s.Limits, s.Features = c.Plan, c.LID, c.Lim, c.Feat
	if c.Exp > 0 {
		s.ExpiresAt = c.ExpiresAt()
		s.GraceUntil = s.ExpiresAt.Add(c.GraceFor())
		s.DegradedUntil = s.GraceUntil.Add(DegradedWindow)
	}
	if c.SExp != nil {
		s.SubscriptionEndsAt = time.Unix(*c.SExp, 0).UTC()
	}
}

// humanDays renders a duration the way a banner should read.
func humanDays(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours() / 24)
	switch {
	case days >= 2:
		return itoa(days) + " days"
	case days == 1:
		return "1 day"
	case d >= time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return itoa(h) + " hours"
	default:
		return "less than an hour"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
