package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// Metric alerts.
//
// A rule watches one metric, compares it with an operator against a threshold,
// and fires when the breach has persisted for `for_sec` seconds — the duration is
// the whole point, because a single-tick CPU spike is not an incident and paging
// on one is how alerting gets muted. Firing records an event and sends a
// notification; when the metric recovers the rule resolves, recording that too,
// so an operator sees the incident's shape, not just its start.
//
// Evaluation is folded into the history persister (once a minute): the raw sample
// it just wrote is the sample the rules are checked against, so there is no second
// sampler and no extra baseline. Minute granularity is right for alerting.

// Alert metrics and operators.
const (
	MetricCPU      = "cpu"
	MetricMem      = "mem"
	MetricSwap     = "swap"
	MetricLoad1    = "load1"
	MetricDiskRoot = "disk_root"
)

// AlertRule is one threshold rule with its (decrypted) notification target.
type AlertRule struct {
	UID          string       `json:"uid"`
	Name         string       `json:"name"`
	Metric       string       `json:"metric"`
	Op           string       `json:"op"` // gt | lt
	Threshold    float64      `json:"threshold"`
	ForSec       int          `json:"for_sec"`
	Enabled      bool         `json:"enabled"`
	NotifyKind   string       `json:"notify_kind"` // log | webhook | telegram
	NotifyTarget NotifyTarget `json:"-"`           // never serialised back out
}

// NotifyTarget is the sealed side of a rule: where a firing goes.
type NotifyTarget struct {
	WebhookURL    string `json:"webhook_url,omitempty"`
	TelegramToken string `json:"telegram_token,omitempty"`
	TelegramChat  string `json:"telegram_chat,omitempty"`
}

// AlertEvent is a firing or a resolution.
type AlertEvent struct {
	RuleUID string  `json:"rule_uid"`
	State   string  `json:"state"` // firing | resolved
	Value   float64 `json:"value"`
	At      string  `json:"at"`
}

// AlertRepo is what the evaluator needs: the active rules, and a place to record
// what fired. Rule CRUD is a separate concern (the HTTP edge).
type AlertRepo interface {
	ActiveRules(ctx context.Context) ([]AlertRule, error)
	RecordEvent(ctx context.Context, ruleUID, state string, value float64, at time.Time) error
}

// Notifier sends a firing/resolution somewhere. The evaluator records the event
// regardless; the notifier is the outward push on top.
type Notifier interface {
	Notify(ctx context.Context, rule AlertRule, value float64, state string)
}

// breachState tracks one rule across ticks: when the breach began, and whether it
// has already fired (so it fires once per incident, not once per tick).
type breachState struct {
	since  time.Time
	firing bool
}

// Evaluator checks rules against each sample and drives notifications.
type Evaluator struct {
	repo     AlertRepo
	notifier Notifier
	log      *slog.Logger
	mu       sync.Mutex
	states   map[string]breachState
}

// NewEvaluator builds an evaluator. notifier may be nil (events are still
// recorded — the dashboard's alert list is itself a notification channel).
func NewEvaluator(repo AlertRepo, notifier Notifier, log *slog.Logger) *Evaluator {
	if log == nil {
		log = slog.Default()
	}
	return &Evaluator{repo: repo, notifier: notifier, log: log, states: map[string]breachState{}}
}

// Evaluate checks every active rule against one sample.
func (e *Evaluator) Evaluate(ctx context.Context, n NodeSample, now time.Time) {
	rules, err := e.repo.ActiveRules(ctx)
	if err != nil {
		e.log.Debug("monitor: could not load alert rules", "err", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	live := make(map[string]struct{}, len(rules))
	for _, r := range rules {
		live[r.UID] = struct{}{}
		value, ok := metricValue(n, r.Metric)
		if !ok {
			continue // metric not readable on this host (e.g. no disk) — skip
		}
		breaching := compare(value, r.Op, r.Threshold)
		st := e.states[r.UID]
		switch {
		case breaching:
			if st.since.IsZero() {
				st.since = now
			}
			if !st.firing && now.Sub(st.since) >= time.Duration(r.ForSec)*time.Second {
				st.firing = true
				e.fire(ctx, r, value, "firing", now)
			}
		case st.firing:
			// Was firing, now recovered → resolve.
			e.fire(ctx, r, value, "resolved", now)
			st = breachState{}
		default:
			st = breachState{}
		}
		e.states[r.UID] = st
	}
	// Forget state for rules that no longer exist.
	for uid := range e.states {
		if _, ok := live[uid]; !ok {
			delete(e.states, uid)
		}
	}
}

// fire records the event and pushes the notification.
func (e *Evaluator) fire(ctx context.Context, r AlertRule, value float64, state string, now time.Time) {
	if err := e.repo.RecordEvent(ctx, r.UID, state, value, now); err != nil {
		e.log.Debug("monitor: could not record alert event", "err", err, "rule", r.UID)
	}
	if e.notifier != nil {
		e.notifier.Notify(ctx, r, value, state)
	}
	e.log.Info("alert "+state, "rule", r.Name, "metric", r.Metric, "value", value, "threshold", r.Threshold)
}

// metricValue projects a sample onto the metric a rule watches.
func metricValue(n NodeSample, metric string) (float64, bool) {
	switch metric {
	case MetricCPU:
		return n.CPUPercent, true
	case MetricLoad1:
		return n.Load1, true
	case MetricMem:
		if n.MemTotalKB > 0 {
			return float64(n.MemUsedKB) / float64(n.MemTotalKB) * 100, true
		}
	case MetricSwap:
		if n.SwapTotalKB > 0 {
			return float64(n.SwapUsedKB) / float64(n.SwapTotalKB) * 100, true
		}
	case MetricDiskRoot:
		for _, d := range n.Disks {
			if d.Path == "/" {
				return d.UsedPercent, true
			}
		}
	}
	return 0, false
}

// compare applies a rule's operator.
func compare(value float64, op string, threshold float64) bool {
	if op == "lt" {
		return value < threshold
	}
	return value > threshold // default gt
}

// ── notifier ─────────────────────────────────────────────────────────────────

// HTTPNotifier delivers firings by webhook or Telegram (both outbound HTTP). The
// "log" kind sends nothing — the recorded event is its own notification.
type HTTPNotifier struct {
	client *http.Client
	log    *slog.Logger
}

// NewHTTPNotifier builds a notifier with a bounded client — a slow endpoint must
// never wedge the evaluator.
func NewHTTPNotifier(log *slog.Logger) *HTTPNotifier {
	if log == nil {
		log = slog.Default()
	}
	return &HTTPNotifier{client: &http.Client{Timeout: 10 * time.Second}, log: log}
}

func (h *HTTPNotifier) Notify(ctx context.Context, rule AlertRule, value float64, state string) {
	msg := fmt.Sprintf("[%s] %s: %s is %.1f (threshold %.1f)", state, rule.Name, rule.Metric, value, rule.Threshold)
	switch rule.NotifyKind {
	case "webhook":
		h.postJSON(ctx, rule.NotifyTarget.WebhookURL, map[string]any{
			"rule": rule.Name, "metric": rule.Metric, "state": state,
			"value": value, "threshold": rule.Threshold, "message": msg,
		})
	case "telegram":
		t := rule.NotifyTarget
		if t.TelegramToken == "" || t.TelegramChat == "" {
			return
		}
		h.postJSON(ctx, "https://api.telegram.org/bot"+t.TelegramToken+"/sendMessage",
			map[string]any{"chat_id": t.TelegramChat, "text": msg})
	}
}

func (h *HTTPNotifier) postJSON(ctx context.Context, url string, body map[string]any) {
	if url == "" {
		return
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		h.log.Debug("monitor: alert notification failed", "err", err)
		return
	}
	_ = resp.Body.Close()
}

// WithAlerts enables rule evaluation on each persist tick.
func (s *Service) WithAlerts(e *Evaluator) *Service {
	s.evaluator = e
	return s
}

// ── rule administration (the HTTP edge) ──────────────────────────────────────

// AlertRuleInput is a request to create a rule.
type AlertRuleInput struct {
	Name         string       `json:"name"`
	Metric       string       `json:"metric"`
	Op           string       `json:"op"`
	Threshold    float64      `json:"threshold"`
	ForSec       int          `json:"for_sec"`
	NotifyKind   string       `json:"notify_kind"`
	NotifyTarget NotifyTarget `json:"notify_target"`
}

// AlertAdmin is the rule/event management contract, implemented by the repo.
type AlertAdmin interface {
	CreateRule(ctx context.Context, in AlertRuleInput) (*AlertRule, error)
	ListRules(ctx context.Context) ([]AlertRule, error)
	SetRuleEnabled(ctx context.Context, uid string, enabled bool) error
	DeleteRule(ctx context.Context, uid string) error
	ListEvents(ctx context.Context, limit int) ([]AlertEvent, error)
}

// WithAlertAdmin wires the rule store for the HTTP edge.
func (s *Service) WithAlertAdmin(a AlertAdmin) *Service {
	s.alertAdmin = a
	return s
}

// AlertsEnabled reports whether rule management is available.
func (s *Service) AlertsEnabled() bool { return s != nil && s.alertAdmin != nil }

var (
	validMetrics = map[string]bool{MetricCPU: true, MetricMem: true, MetricSwap: true, MetricLoad1: true, MetricDiskRoot: true}
	validKinds   = map[string]bool{"log": true, "webhook": true, "telegram": true}
)

// CreateRule validates and stores a rule.
func (s *Service) CreateRule(ctx context.Context, in AlertRuleInput) (*AlertRule, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, errx.Validation("invalid_name", "A rule name is required.")
	}
	if !validMetrics[in.Metric] {
		return nil, errx.Validation("invalid_metric", "Metric must be one of cpu, mem, swap, load1, disk_root.")
	}
	if in.Op == "" {
		in.Op = "gt"
	}
	if in.Op != "gt" && in.Op != "lt" {
		return nil, errx.Validation("invalid_op", "Operator must be gt or lt.")
	}
	if in.NotifyKind == "" {
		in.NotifyKind = "log"
	}
	if !validKinds[in.NotifyKind] {
		return nil, errx.Validation("invalid_notify_kind", "Notify kind must be log, webhook or telegram.")
	}
	if in.ForSec < 0 || in.ForSec > 86400 {
		return nil, errx.Validation("invalid_duration", "The duration must be between 0 and 86400 seconds.")
	}
	switch in.NotifyKind {
	case "webhook":
		if !strings.HasPrefix(in.NotifyTarget.WebhookURL, "https://") && !strings.HasPrefix(in.NotifyTarget.WebhookURL, "http://") {
			return nil, errx.Validation("invalid_webhook", "A webhook URL (http/https) is required.")
		}
	case "telegram":
		if in.NotifyTarget.TelegramToken == "" || in.NotifyTarget.TelegramChat == "" {
			return nil, errx.Validation("invalid_telegram", "A Telegram bot token and chat id are required.")
		}
	}
	return s.alertAdmin.CreateRule(ctx, in)
}

// ListRules returns the configured rules (targets never included).
func (s *Service) ListRules(ctx context.Context) ([]AlertRule, error) {
	return s.alertAdmin.ListRules(ctx)
}

// SetRuleEnabled toggles a rule on or off.
func (s *Service) SetRuleEnabled(ctx context.Context, uid string, enabled bool) error {
	return s.alertAdmin.SetRuleEnabled(ctx, uid, enabled)
}

// DeleteRule removes a rule.
func (s *Service) DeleteRule(ctx context.Context, uid string) error {
	return s.alertAdmin.DeleteRule(ctx, uid)
}

// AlertEvents returns recent firings/resolutions.
func (s *Service) AlertEvents(ctx context.Context, limit int) ([]AlertEvent, error) {
	return s.alertAdmin.ListEvents(ctx, limit)
}
