package mail

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// The mail queue, as postfix reports it (`postqueue -j`, one JSON object per
// line). The broker hands back raw lines; the schema lives here where it can
// be unit-tested over fixtures.

// QueueRecipient is one recipient of a queued message.
type QueueRecipient struct {
	Address     string `json:"address"`
	DelayReason string `json:"delay_reason,omitempty"`
}

// QueueMessage is one message sitting in the queue.
type QueueMessage struct {
	ID         string           `json:"id"`
	Queue      string           `json:"queue"` // active | deferred | hold | incoming
	Sender     string           `json:"sender"`
	SizeBytes  int64            `json:"size_bytes"`
	Arrival    string           `json:"arrival"` // RFC3339
	Recipients []QueueRecipient `json:"recipients"`
}

// postqueueLine mirrors postfix's `postqueue -j` schema (the fields we show).
type postqueueLine struct {
	QueueName   string `json:"queue_name"`
	QueueID     string `json:"queue_id"`
	ArrivalTime int64  `json:"arrival_time"`
	MessageSize int64  `json:"message_size"`
	Sender      string `json:"sender"`
	Recipients  []struct {
		Address     string `json:"address"`
		DelayReason string `json:"delay_reason"`
	} `json:"recipients"`
}

// parseQueue parses postqueue -j output, skipping torn or foreign lines — a
// queue view that shows what it could parse beats one that errors whole.
func parseQueue(raw string) []QueueMessage {
	out := []QueueMessage{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pl postqueueLine
		if err := json.Unmarshal([]byte(line), &pl); err != nil || pl.QueueID == "" {
			continue
		}
		msg := QueueMessage{
			ID: pl.QueueID, Queue: pl.QueueName, Sender: pl.Sender, SizeBytes: pl.MessageSize,
			Arrival: time.Unix(pl.ArrivalTime, 0).UTC().Format(time.RFC3339),
		}
		for _, r := range pl.Recipients {
			msg.Recipients = append(msg.Recipients, QueueRecipient{Address: r.Address, DelayReason: r.DelayReason})
		}
		out = append(out, msg)
	}
	return out
}

// QueueList returns the current mail queue. running=false means postfix
// itself is down — an answer the UI shows, not an error.
func (s *Service) QueueList(ctx context.Context) ([]QueueMessage, bool, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, false, err
	}
	res, err := s.broker.Invoke(ctx, "mail.queue.list", map[string]any{})
	if err != nil {
		return nil, false, err
	}
	raw, _ := res["raw"].(string)
	running, _ := res["running"].(bool)
	return parseQueue(raw), running, nil
}

// QueueFlush asks postfix to attempt delivery of everything deferred.
func (s *Service) QueueFlush(ctx context.Context) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	_, err := s.broker.Invoke(ctx, "mail.queue.flush", map[string]any{})
	return err
}

// QueueDelete removes specific messages by ID (validated broker-side too).
func (s *Service) QueueDelete(ctx context.Context, ids []string) (int, error) {
	if err := s.requireAvailable(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, errx.Validation("bad_input", "No queue IDs given.")
	}
	res, err := s.broker.Invoke(ctx, "mail.queue.delete", map[string]any{"ids": ids})
	if err != nil {
		return 0, err
	}
	deleted, _ := res["deleted"].(float64)
	return int(deleted), nil
}

// Usage is one mailbox's storage state. Known=false means the mailbox has
// never been delivered to (no quota state yet) — not an error.
type Usage struct {
	Known   bool  `json:"known"`
	UsedKB  int64 `json:"used_kb"`
	LimitKB int64 `json:"limit_kb"`
}

// DomainUsage reads every mailbox's quota state for a domain via doveadm.
func (s *Service) DomainUsage(ctx context.Context, domainUID string) (map[string]Usage, error) {
	if err := s.requireAvailable(); err != nil {
		return nil, err
	}
	dom, err := s.repo.GetDomainByUID(ctx, domainUID)
	if err != nil {
		return nil, err
	}
	accts, err := s.repo.ListAccounts(ctx, dom.ID)
	if err != nil {
		return nil, err
	}
	if len(accts) == 0 {
		return map[string]Usage{}, nil
	}
	addrs := make([]string, len(accts))
	for i := range accts {
		addrs[i] = accts[i].LocalPart + "@" + dom.Domain
	}
	res, err := s.broker.Invoke(ctx, "mail.quota", map[string]any{"addresses": addrs})
	if err != nil {
		return nil, err
	}
	out := map[string]Usage{}
	quotas, _ := res["quotas"].(map[string]any)
	for addr, v := range quotas {
		row, _ := v.(map[string]any)
		known, _ := row["known"].(bool)
		used, _ := row["used_kb"].(float64)
		limit, _ := row["limit_kb"].(float64)
		out[addr] = Usage{Known: known, UsedKB: int64(used), LimitKB: int64(limit)}
	}
	return out, nil
}
