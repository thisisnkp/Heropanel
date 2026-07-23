package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/thisisnkp/heropanel/internal/monitor"
	"github.com/thisisnkp/heropanel/pkg/errx"
	"github.com/thisisnkp/heropanel/pkg/idgen"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

// AlertStore implements monitor.AlertRepo and monitor.AlertAdmin over the
// datastore. Notification targets are sealed with the panel's data key, so a
// Telegram bot token never sits in the database in the clear.
type AlertStore struct {
	db     *DB
	cipher *secrets.Cipher
}

// NewAlertStore constructs an AlertStore. cipher may be unconfigured; rules whose
// kind needs a target are then refused (see CreateRule).
func NewAlertStore(db *DB, cipher *secrets.Cipher) *AlertStore {
	return &AlertStore{db: db, cipher: cipher}
}

var (
	_ monitor.AlertRepo  = (*AlertStore)(nil)
	_ monitor.AlertAdmin = (*AlertStore)(nil)
)

// targetAAD binds a sealed target to its rule, so a ciphertext moved to another
// row will not open.
func targetAAD(uid string) string { return "alert_rules:" + uid + ":notify_target" }

// CreateRule seals the target and inserts the rule.
func (s *AlertStore) CreateRule(ctx context.Context, in monitor.AlertRuleInput) (*monitor.AlertRule, error) {
	uid := idgen.NewULID()
	var enc any // NULL for the log kind
	if in.NotifyKind != "log" {
		if s.cipher == nil || !s.cipher.Configured() {
			return nil, errx.New(errx.KindUnavailable, "secrets_unavailable",
				"A data key (HP_SECRET_KEY) is required to store a notification target.")
		}
		raw, _ := json.Marshal(in.NotifyTarget)
		blob, err := s.cipher.Seal(raw, targetAAD(uid))
		if err != nil {
			return nil, errx.Internal(err)
		}
		enc = blob
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_rules (uid, name, metric, op, threshold, for_sec, enabled, notify_kind, notify_target_enc)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		uid, in.Name, in.Metric, in.Op, in.Threshold, in.ForSec, in.NotifyKind, enc)
	if err != nil {
		return nil, errx.Internal(err)
	}
	return &monitor.AlertRule{
		UID: uid, Name: in.Name, Metric: in.Metric, Op: in.Op, Threshold: in.Threshold,
		ForSec: in.ForSec, Enabled: true, NotifyKind: in.NotifyKind,
	}, nil
}

// ruleRow scans a rule; the target is decrypted only when asked for (ActiveRules).
type ruleRow struct {
	UID          string  `db:"uid"`
	Name         string  `db:"name"`
	Metric       string  `db:"metric"`
	Op           string  `db:"op"`
	Threshold    float64 `db:"threshold"`
	ForSec       int     `db:"for_sec"`
	Enabled      int     `db:"enabled"`
	NotifyKind   string  `db:"notify_kind"`
	NotifyTarget *string `db:"notify_target_enc"`
}

func (r ruleRow) toRule() monitor.AlertRule {
	return monitor.AlertRule{
		UID: r.UID, Name: r.Name, Metric: r.Metric, Op: r.Op, Threshold: r.Threshold,
		ForSec: r.ForSec, Enabled: r.Enabled == 1, NotifyKind: r.NotifyKind,
	}
}

const ruleSelect = `SELECT uid, name, metric, op, threshold, for_sec, enabled, notify_kind, notify_target_enc FROM alert_rules`

// ListRules returns every rule, targets NOT decrypted (they are write-only).
func (s *AlertStore) ListRules(ctx context.Context) ([]monitor.AlertRule, error) {
	var rows []ruleRow
	if err := s.db.SelectContext(ctx, &rows, ruleSelect+` ORDER BY created_at DESC`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]monitor.AlertRule, len(rows))
	for i := range rows {
		out[i] = rows[i].toRule()
	}
	return out, nil
}

// ActiveRules returns enabled rules with their targets decrypted, for the
// evaluator. A target that will not open (missing key, tampered row) is dropped
// with its rule rather than firing to nowhere.
func (s *AlertStore) ActiveRules(ctx context.Context) ([]monitor.AlertRule, error) {
	var rows []ruleRow
	if err := s.db.SelectContext(ctx, &rows, ruleSelect+` WHERE enabled = 1`); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]monitor.AlertRule, 0, len(rows))
	for _, r := range rows {
		rule := r.toRule()
		if r.NotifyKind != "log" && r.NotifyTarget != nil && *r.NotifyTarget != "" {
			if s.cipher == nil {
				continue
			}
			raw, err := s.cipher.Open(*r.NotifyTarget, targetAAD(r.UID))
			if err != nil {
				continue
			}
			_ = json.Unmarshal(raw, &rule.NotifyTarget)
		}
		out = append(out, rule)
	}
	return out, nil
}

// SetRuleEnabled toggles a rule.
func (s *AlertStore) SetRuleEnabled(ctx context.Context, uid string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE alert_rules SET enabled = ?, updated_at = ? WHERE uid = ?`,
		v, fmtTS(time.Now()), uid)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errx.NotFound("rule_not_found", "No such alert rule.")
	}
	return nil
}

// DeleteRule removes a rule (its events are left as history).
func (s *AlertStore) DeleteRule(ctx context.Context, uid string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE uid = ?`, uid)
	if err != nil {
		return errx.Internal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errx.NotFound("rule_not_found", "No such alert rule.")
	}
	return nil
}

// RecordEvent appends a firing or resolution.
func (s *AlertStore) RecordEvent(ctx context.Context, ruleUID, state string, value float64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_events (rule_uid, state, value, at) VALUES (?, ?, ?, ?)`,
		ruleUID, state, value, fmtTS(at))
	if err != nil {
		return errx.Internal(err)
	}
	return nil
}

// ListEvents returns the most recent events, newest first.
func (s *AlertStore) ListEvents(ctx context.Context, limit int) ([]monitor.AlertEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []struct {
		RuleUID string  `db:"rule_uid"`
		State   string  `db:"state"`
		Value   float64 `db:"value"`
		At      string  `db:"at"`
	}
	if err := s.db.SelectContext(ctx, &rows,
		`SELECT rule_uid, state, value, at FROM alert_events ORDER BY at DESC, id DESC LIMIT ?`, limit); err != nil {
		return nil, errx.Internal(err)
	}
	out := make([]monitor.AlertEvent, len(rows))
	for i, r := range rows {
		out[i] = monitor.AlertEvent{RuleUID: r.RuleUID, State: r.State, Value: r.Value, At: r.At}
	}
	return out, nil
}
