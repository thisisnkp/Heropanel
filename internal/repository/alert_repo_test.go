package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/thisisnkp/heropanel/internal/monitor"
	"github.com/thisisnkp/heropanel/internal/repository"
	"github.com/thisisnkp/heropanel/pkg/secrets"
)

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	c, err := secrets.FromBase64(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return c
}

func TestAlertStoreSealsTargetAndDecryptsForEvaluation(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewAlertStore(db, testCipher(t))
	ctx := context.Background()

	rule, err := store.CreateRule(ctx, monitor.AlertRuleInput{
		Name: "cpu", Metric: monitor.MetricCPU, Op: "gt", Threshold: 90, ForSec: 60,
		NotifyKind: "webhook", NotifyTarget: monitor.NotifyTarget{WebhookURL: "https://hooks.test/x"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A plain list must NOT carry the target — it is write-only.
	rules, err := store.ListRules(ctx)
	if err != nil || len(rules) != 1 {
		t.Fatalf("list = %d rules, err=%v; want 1", len(rules), err)
	}
	if rules[0].NotifyTarget.WebhookURL != "" {
		t.Error("ListRules leaked the notification target")
	}
	if rules[0].UID != rule.UID || rules[0].NotifyKind != "webhook" {
		t.Errorf("listed rule wrong: %+v", rules[0])
	}

	// The evaluator's view decrypts the target.
	active, err := store.ActiveRules(ctx)
	if err != nil || len(active) != 1 {
		t.Fatalf("active = %d, err=%v; want 1", len(active), err)
	}
	if active[0].NotifyTarget.WebhookURL != "https://hooks.test/x" {
		t.Errorf("ActiveRules did not decrypt the target: %q", active[0].NotifyTarget.WebhookURL)
	}

	// Disabling drops it from the evaluator's set but keeps it in the list.
	if err := store.SetRuleEnabled(ctx, rule.UID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if a, _ := store.ActiveRules(ctx); len(a) != 0 {
		t.Errorf("disabled rule still active: %d", len(a))
	}
	if r, _ := store.ListRules(ctx); len(r) != 1 || r[0].Enabled {
		t.Errorf("disabled rule missing or still enabled in list")
	}

	// Events round-trip newest-first.
	_ = store.RecordEvent(ctx, rule.UID, "firing", 95, time.Now())
	_ = store.RecordEvent(ctx, rule.UID, "resolved", 40, time.Now().Add(time.Second))
	events, err := store.ListEvents(ctx, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %d, err=%v; want 2", len(events), err)
	}
	if events[0].State != "resolved" || events[1].State != "firing" {
		t.Errorf("events not newest-first: %+v", events)
	}

	if err := store.DeleteRule(ctx, rule.UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if r, _ := store.ListRules(ctx); len(r) != 0 {
		t.Errorf("rule survived delete")
	}
}

// The log kind needs no data key; webhook/telegram do.
func TestAlertStoreLogKindNeedsNoKey(t *testing.T) {
	db := newTestDB(t)
	store := repository.NewAlertStore(db, nil) // no cipher
	ctx := context.Background()

	if _, err := store.CreateRule(ctx, monitor.AlertRuleInput{
		Name: "log only", Metric: monitor.MetricCPU, Op: "gt", Threshold: 95, NotifyKind: "log",
	}); err != nil {
		t.Fatalf("a log-kind rule must not need a data key: %v", err)
	}
	if _, err := store.CreateRule(ctx, monitor.AlertRuleInput{
		Name: "hook", Metric: monitor.MetricCPU, Op: "gt", Threshold: 95, NotifyKind: "webhook",
		NotifyTarget: monitor.NotifyTarget{WebhookURL: "https://x.test"},
	}); err == nil {
		t.Error("a webhook rule without a data key should be refused")
	}
}
