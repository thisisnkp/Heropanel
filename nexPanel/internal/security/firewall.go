// Package security is the host-hardening module: firewall (nftables) with a
// snapshot-and-revert safety net, malware scanning with quarantine, and the
// panel's own auth hardening. In-core, satellite-ready like its siblings.
package security

import (
	"context"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/thisisnkp/nexpanel/internal/broker"
	"github.com/thisisnkp/nexpanel/pkg/errx"
	"github.com/thisisnkp/nexpanel/pkg/idgen"
)

// The apply window: how long an unconfirmed change is allowed to stand before
// the guard reverts it. Bounded so a UI can offer a sane countdown.
const (
	DefaultWindow = 60 * time.Second
	MinWindow     = 10 * time.Second
	MaxWindow     = 60 * time.Minute
	// tsLayout matches the rest of the datastore's UTC timestamps.
	tsLayout = "2006-01-02 15:04:05"
)

var reComment = regexp.MustCompile(`^[\w .,:/#()+-]{0,255}$`)

// Rule is the API view of a firewall rule.
type Rule struct {
	UID      string `json:"uid"`
	Position int    `json:"position"`
	Action   string `json:"action"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	PortEnd  int    `json:"port_end"` // 0 = single port; else the range end
	Source   string `json:"source"`
	Comment  string `json:"comment"`
}

// RuleRecord is the persistence view.
type RuleRecord struct {
	ID        int64  `db:"id"`
	UID       string `db:"uid"`
	Position  int    `db:"position"`
	Action    string `db:"action"`
	Protocol  string `db:"protocol"`
	Port      int    `db:"port"`
	PortEnd   int    `db:"port_end"`
	Source    string `db:"source"`
	Comment   string `db:"comment"`
	CreatedAt string `db:"created_at"`
}

// State is the pending-apply state that drives the rollback timer.
type State struct {
	PendingToken string `db:"pending_token"`
	Deadline     string `db:"deadline"`
}

// IPListEntry is one geo/IP allow-block entry: a CIDR (v4 or v6) that is either
// always allowed or always dropped, enforced as an nftables set membership test
// ahead of the ordinary rules. A country block is just a bulk import of that
// country's CIDRs as block entries.
type IPListEntry struct {
	UID     string `json:"uid" db:"uid"`
	CIDR    string `json:"cidr" db:"cidr"`
	Mode    string `json:"mode" db:"mode"` // block | allow
	Comment string `json:"comment" db:"comment"`
	// Country is the ISO 3166-1 alpha-2 code an entry was bulk-imported from
	// (uppercase), or "" for a manually-added entry. It groups an import so it
	// can be re-imported (replaced) or removed as a unit.
	Country string `json:"country" db:"country"`
}

// IPListInput adds an entry.
type IPListInput struct {
	CIDR    string `json:"cidr"`
	Mode    string `json:"mode"`
	Comment string `json:"comment"`
}

// FirewallRepo is the persistence contract.
type FirewallRepo interface {
	ListRules(ctx context.Context) ([]RuleRecord, error)
	InsertRule(ctx context.Context, r *RuleRecord) error
	DeleteRule(ctx context.Context, uid string) error
	NextPosition(ctx context.Context) (int, error)
	GetState(ctx context.Context) (*State, error)
	SetState(ctx context.Context, token, deadline string) error
	ListIPEntries(ctx context.Context) ([]IPListEntry, error)
	InsertIPEntry(ctx context.Context, e *IPListEntry) error
	DeleteIPEntry(ctx context.Context, uid string) error
	// InsertIPEntries bulk-inserts imported entries in one transaction — a
	// country is thousands of CIDRs and a row-at-a-time loop is both slow and
	// non-atomic (a mid-import failure would leave a half-imported country).
	InsertIPEntries(ctx context.Context, entries []*IPListEntry) error
	// DeleteIPEntriesByCountry removes every entry imported from a country, so a
	// re-import replaces cleanly and a removal is a single act.
	DeleteIPEntriesByCountry(ctx context.Context, country string) error
}

// RuleInput adds a rule.
type RuleInput struct {
	Action   string `json:"action"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	PortEnd  int    `json:"port_end"` // 0 = single port; else the (inclusive) range end
	Source   string `json:"source"`
	Comment  string `json:"comment"`
}

// Firewall orchestrates the host firewall.
type Firewall struct {
	repo   FirewallRepo
	broker broker.Gateway
	window time.Duration
	now    func() time.Time

	// Geo country import: URL templates (each with one %s for the lowercase
	// country code) for the IPv4 and IPv6 aggregated-CIDR zone files, and the
	// fetcher used to retrieve them. geoFetch is injectable so tests need no
	// network. Empty templates disable country import.
	geoV4URL string
	geoV6URL string
	geoFetch func(ctx context.Context, url string) ([]byte, error)

	mu    sync.Mutex
	timer *time.Timer // the in-process revert timer for the current pending apply
}

// NewFirewall constructs the firewall service.
func NewFirewall(repo FirewallRepo, gw broker.Gateway) *Firewall {
	f := &Firewall{repo: repo, broker: gw, window: DefaultWindow, now: time.Now}
	f.geoFetch = f.httpFetch
	return f
}

// WithGeoSource sets the IPv4/IPv6 country-CIDR zone URL templates (each with a
// single %s for the lowercase ISO country code). Empty templates leave country
// import disabled.
func (f *Firewall) WithGeoSource(v4URL, v6URL string) *Firewall {
	f.geoV4URL, f.geoV6URL = v4URL, v6URL
	return f
}

// WithWindow overrides the confirmation window (tests use a short one).
func (f *Firewall) WithWindow(d time.Duration) *Firewall {
	if d >= MinWindow && d <= MaxWindow {
		f.window = d
	}
	return f
}

// Available reports whether the firewall can operate.
func (f *Firewall) Available() bool { return f != nil && f.broker != nil && f.repo != nil }

func (f *Firewall) requireAvailable() error {
	if f.Available() {
		return nil
	}
	return errx.New(errx.KindUnavailable, "firewall_unavailable",
		"Firewall management needs the broker and a datastore.")
}

// ListRules returns the ordered ruleset plus the pending state.
func (f *Firewall) ListRules(ctx context.Context) ([]Rule, *State, error) {
	recs, err := f.repo.ListRules(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Rule, len(recs))
	for i := range recs {
		out[i] = ruleView(&recs[i])
	}
	st, err := f.repo.GetState(ctx)
	if err != nil {
		return nil, nil, err
	}
	return out, st, nil
}

// AddRule validates and stores a rule. It does NOT apply — applying is an
// explicit, separately-confirmed act, because a firewall change can lock the
// operator out and must never be a side effect of editing a list.
func (f *Firewall) AddRule(ctx context.Context, in RuleInput) (*Rule, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	if err := validateRule(&in); err != nil {
		return nil, err
	}
	pos, err := f.repo.NextPosition(ctx)
	if err != nil {
		return nil, err
	}
	rec := &RuleRecord{
		UID: idgen.NewULID(), Position: pos, Action: in.Action,
		Protocol: in.Protocol, Port: in.Port, PortEnd: in.PortEnd, Source: in.Source, Comment: in.Comment,
	}
	if err := f.repo.InsertRule(ctx, rec); err != nil {
		return nil, err
	}
	v := ruleView(rec)
	return &v, nil
}

// DeleteRule removes a rule (unapplied until the next Apply).
func (f *Firewall) DeleteRule(ctx context.Context, uid string) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	return f.repo.DeleteRule(ctx, uid)
}

// ListIPEntries returns the geo/IP allow-block entries.
func (f *Firewall) ListIPEntries(ctx context.Context) ([]IPListEntry, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	return f.repo.ListIPEntries(ctx)
}

// AddIPEntry validates and stores an allow/block CIDR. Like a rule, it is not
// enforced until the next Apply (a firewall change is never a side effect).
func (f *Firewall) AddIPEntry(ctx context.Context, in IPListInput) (*IPListEntry, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))
	if in.Mode == "" {
		in.Mode = "block"
	}
	if in.Mode != "block" && in.Mode != "allow" {
		return nil, errx.Validation("invalid_mode", "Mode must be block or allow.")
	}
	in.CIDR = strings.TrimSpace(in.CIDR)
	if !validCIDROrIP(in.CIDR) {
		return nil, errx.Validation("invalid_cidr", "Enter a valid IPv4/IPv6 address or CIDR.")
	}
	if !reComment.MatchString(in.Comment) {
		return nil, errx.Validation("invalid_comment", "The comment has unsupported characters.")
	}
	e := &IPListEntry{UID: idgen.NewULID(), CIDR: in.CIDR, Mode: in.Mode, Comment: in.Comment}
	if err := f.repo.InsertIPEntry(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

// DeleteIPEntry removes an allow/block entry (unapplied until the next Apply).
func (f *Firewall) DeleteIPEntry(ctx context.Context, uid string) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	return f.repo.DeleteIPEntry(ctx, uid)
}

// validCIDROrIP accepts a bare address or a CIDR, of either family.
func validCIDROrIP(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// ApplyResult is returned by Apply.
type ApplyResult struct {
	Token    string `json:"token"`
	Deadline string `json:"deadline"`
	Window   int    `json:"window_sec"`
}

// Apply renders the current ruleset, applies it through the broker, and arms
// the revert timer. The change stands only if Confirm is called with the
// returned token before the deadline; otherwise the guard reverts it.
func (f *Firewall) Apply(ctx context.Context) (*ApplyResult, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	st, err := f.repo.GetState(ctx)
	if err != nil {
		return nil, err
	}
	if st.PendingToken != "" {
		return nil, errx.New(errx.KindConflict, "apply_pending",
			"A firewall change is already awaiting confirmation.")
	}

	recs, err := f.repo.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	lists, err := f.repo.ListIPEntries(ctx)
	if err != nil {
		return nil, err
	}
	ruleset := RenderRuleset(toRenderRules(recs), lists)
	if _, err := f.broker.Invoke(ctx, "firewall.apply", map[string]any{"ruleset": ruleset}); err != nil {
		return nil, err
	}

	token := idgen.NewULID()
	deadline := f.now().UTC().Add(f.window)
	if err := f.repo.SetState(ctx, token, deadline.Format(tsLayout)); err != nil {
		// The ruleset is applied but we could not record the pending state — revert
		// immediately rather than leaving an armed change we cannot track.
		_, _ = f.broker.Invoke(ctx, "firewall.rollback", nil)
		return nil, err
	}
	f.armLocked(token)
	return &ApplyResult{Token: token, Deadline: deadline.Format(tsLayout), Window: int(f.window.Seconds())}, nil
}

// Confirm makes the pending change permanent. The token must match the apply
// the operator saw — a stale confirm (from an earlier, already-reverted apply)
// is refused rather than silently confirming the wrong thing.
func (f *Firewall) Confirm(ctx context.Context, token string) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	st, err := f.repo.GetState(ctx)
	if err != nil {
		return err
	}
	if st.PendingToken == "" {
		return errx.New(errx.KindConflict, "nothing_pending", "There is no firewall change awaiting confirmation.")
	}
	if st.PendingToken != token {
		return errx.Validation("stale_token", "That confirmation is for a different firewall change.")
	}
	if _, err := f.broker.Invoke(ctx, "firewall.confirm", nil); err != nil {
		return err
	}
	f.clearLocked(ctx)
	return nil
}

// Rollback reverts the pending change now (an operator who realises the
// mistake need not wait for the timer).
func (f *Firewall) Rollback(ctx context.Context) error {
	if err := f.requireAvailable(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rollbackLocked(ctx)
}

// Status reports the live ruleset (from nft) and the pending state.
func (f *Firewall) Status(ctx context.Context) (map[string]any, error) {
	if err := f.requireAvailable(); err != nil {
		return nil, err
	}
	res, err := f.broker.Invoke(ctx, "firewall.status", nil)
	if err != nil {
		return nil, err
	}
	st, _ := f.repo.GetState(ctx)
	out := map[string]any{
		"ruleset": res["ruleset"], "running": res["running"],
	}
	if st != nil && st.PendingToken != "" {
		out["pending"] = true
		out["deadline"] = st.Deadline
	} else {
		out["pending"] = false
	}
	return out, nil
}

// RunGuard is the crash-recovery net: on start and every tick it reverts any
// pending change whose deadline has passed. The in-process timer handles the
// prompt case; this handles an npd that restarted mid-window (the deadline is
// persisted, so a fresh process still honours it).
func (f *Firewall) RunGuard(ctx context.Context, log interface{ Info(string, ...any) }) {
	if !f.Available() {
		return
	}
	f.sweep(ctx, log)
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.sweep(ctx, log)
		}
	}
}

func (f *Firewall) sweep(ctx context.Context, log interface{ Info(string, ...any) }) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.repo.GetState(ctx)
	if err != nil || st.PendingToken == "" || st.Deadline == "" {
		return
	}
	dl, err := time.Parse(tsLayout, st.Deadline)
	if err != nil {
		return
	}
	if f.now().UTC().Before(dl) {
		// Still within the window — make sure a timer is armed (it will not be
		// after a restart) so the prompt path also works.
		if f.timer == nil {
			f.armAtLocked(st.PendingToken, dl)
		}
		return
	}
	if err := f.rollbackLocked(ctx); err == nil && log != nil {
		log.Info("firewall change auto-reverted (unconfirmed)")
	}
}

// armLocked arms the revert timer for the full window from now.
func (f *Firewall) armLocked(token string) {
	f.armAtLocked(token, f.now().UTC().Add(f.window))
}

// armAtLocked arms the revert timer to fire at deadline.
func (f *Firewall) armAtLocked(token string, deadline time.Time) {
	if f.timer != nil {
		f.timer.Stop()
	}
	d := time.Until(deadline)
	if d < 0 {
		d = 0
	}
	f.timer = time.AfterFunc(d, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		// Only revert if this apply is still the pending one.
		st, err := f.repo.GetState(context.Background())
		if err != nil || st.PendingToken != token {
			return
		}
		_ = f.rollbackLocked(context.Background())
	})
}

// rollbackLocked reverts through the broker and clears the state. Caller holds mu.
func (f *Firewall) rollbackLocked(ctx context.Context) error {
	if _, err := f.broker.Invoke(ctx, "firewall.rollback", nil); err != nil {
		return err
	}
	f.clearLocked(ctx)
	return nil
}

// clearLocked drops the pending state and disarms the timer. Caller holds mu.
func (f *Firewall) clearLocked(ctx context.Context) {
	_ = f.repo.SetState(ctx, "", "")
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
}

// ── validation & mapping ─────────────────────────────────────────────────────

func validateRule(in *RuleInput) error {
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	in.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	in.Source = strings.TrimSpace(in.Source)
	if in.Action != "accept" && in.Action != "drop" {
		return errx.Validation("invalid_action", "Action must be accept or drop.")
	}
	if in.Protocol == "" {
		in.Protocol = "tcp"
	}
	if in.Protocol != "tcp" && in.Protocol != "udp" && in.Protocol != "any" {
		return errx.Validation("invalid_protocol", "Protocol must be tcp, udp or any.")
	}
	if in.Port < 0 || in.Port > 65535 {
		return errx.Validation("invalid_port", "Port must be between 0 and 65535.")
	}
	if in.PortEnd < 0 || in.PortEnd > 65535 {
		return errx.Validation("invalid_port_end", "The range end must be between 0 and 65535.")
	}
	if in.PortEnd > 0 && in.PortEnd <= in.Port {
		return errx.Validation("invalid_port_range", "The range end must be greater than the start port.")
	}
	if (in.Port > 0 || in.PortEnd > 0) && in.Protocol == "any" {
		return errx.Validation("port_needs_protocol", "A port needs an explicit tcp or udp protocol.")
	}
	if in.PortEnd > 0 && in.Port == 0 {
		return errx.Validation("range_needs_start", "A port range needs a start port.")
	}
	if in.Source != "" && !validSource(in.Source) {
		return errx.Validation("invalid_source", "Source must be an IPv4 or IPv6 address or CIDR.")
	}
	if !reComment.MatchString(in.Comment) {
		return errx.Validation("invalid_comment", "The comment has unsupported characters.")
	}
	return nil
}

// validSource accepts an IPv4 or IPv6 address or CIDR. The renderer picks
// `ip saddr` vs `ip6 saddr` from the family.
func validSource(s string) bool {
	if ip := net.ParseIP(s); ip != nil {
		return true
	}
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return false
}

func toRenderRules(recs []RuleRecord) []RenderRule {
	out := make([]RenderRule, len(recs))
	for i, r := range recs {
		out[i] = RenderRule{Position: r.Position, Action: r.Action, Protocol: r.Protocol,
			Port: r.Port, PortEnd: r.PortEnd, Source: r.Source}
	}
	return out
}

func ruleView(r *RuleRecord) Rule {
	return Rule{
		UID: r.UID, Position: r.Position, Action: r.Action,
		Protocol: r.Protocol, Port: r.Port, PortEnd: r.PortEnd, Source: r.Source, Comment: r.Comment,
	}
}
