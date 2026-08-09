package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// StartWorkers ensures the consumer group exists and launches n worker
// goroutines that consume until ctx is cancelled.
func (d *Dispatcher) StartWorkers(ctx context.Context, n int) error {
	if err := d.ensureGroup(ctx); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		go d.workLoop(ctx, fmt.Sprintf("w%d", i))
	}
	d.log.Info("job workers started", "count", n, "stream", d.stream)
	return nil
}

// ensureGroup creates the stream + consumer group, tolerating an existing group.
func (d *Dispatcher) ensureGroup(ctx context.Context) error {
	err := d.rdb.XGroupCreateMkStream(ctx, d.stream, d.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("job: create consumer group: %w", err)
	}
	return nil
}

func (d *Dispatcher) workLoop(ctx context.Context, consumer string) {
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := d.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    d.group,
			Consumer: consumer,
			Streams:  []string{d.stream, ">"},
			Count:    1,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if ctx.Err() != nil || err == redis.Nil {
				continue
			}
			// Transient error; back off briefly.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, stream := range res {
			for _, msg := range stream.Messages {
				d.process(ctx, msg)
				_ = d.rdb.XAck(ctx, d.stream, d.group, msg.ID).Err()
			}
		}
	}
}

func (d *Dispatcher) process(ctx context.Context, msg redis.XMessage) {
	uid, _ := msg.Values["job"].(string)
	if uid == "" {
		return
	}
	j, err := d.repo.Get(ctx, uid)
	if err != nil {
		d.log.Warn("job: message references missing job", "uid", uid, "err", err)
		return
	}
	h, ok := d.handler(j.Type)
	if !ok {
		_ = d.repo.Fail(ctx, uid, "no handler registered for type "+j.Type)
		return
	}

	if err := d.repo.SetRunning(ctx, uid); err != nil {
		d.log.Error("job: set running failed", "uid", uid, "err", err)
	}

	result, herr := d.run(ctx, h, j)
	if herr != nil {
		_ = d.repo.Fail(ctx, uid, herr.Error())
		d.publish(ctx, uid, 100, "failed", string(StatusFailed))
		d.log.Warn("job failed", "uid", uid, "type", j.Type, "err", herr)
		return
	}
	raw, _ := json.Marshal(result)
	if err := d.repo.Complete(ctx, uid, raw); err != nil {
		d.log.Error("job: complete failed", "uid", uid, "err", err)
	}
	d.publish(ctx, uid, 100, "done", string(StatusSucceeded))
}

// run executes the handler, recovering panics into errors.
func (d *Dispatcher) run(ctx context.Context, h Handler, j *Job) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("job handler panic: %v", r)
		}
	}()
	return h(ctx, j, &dbProgress{d: d, ctx: ctx, uid: j.UID})
}

// dbProgress persists progress and publishes it for future WebSocket streaming.
type dbProgress struct {
	d   *Dispatcher
	ctx context.Context
	uid string
}

func (p *dbProgress) Report(pct int, step string) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	_ = p.d.repo.SetProgress(p.ctx, p.uid, pct)
	p.d.publish(p.ctx, p.uid, pct, step, string(StatusRunning))
}

// publish emits a progress event to a per-job Pub/Sub channel (consumed by the
// realtime WebSocket hub once it exists; harmless no-op subscriber otherwise).
func (d *Dispatcher) publish(ctx context.Context, uid string, pct int, step, status string) {
	evt, _ := json.Marshal(map[string]any{
		"job": uid, "progress": pct, "step": step, "status": status,
	})
	_ = d.rdb.Publish(ctx, "job:"+uid, evt).Err()
}
