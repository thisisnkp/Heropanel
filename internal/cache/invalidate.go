package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// invalidateChannel is the Redis Pub/Sub channel carrying L1 invalidations.
const invalidateChannel = "hp:cache:invalidate"

// invalMsg is published when keys are written or deleted, telling other
// processes to drop those keys from their in-process L1.
type invalMsg struct {
	Origin string   `json:"origin"`
	Keys   []string `json:"keys"`
}

// Invalidator keeps L1 caches coherent across processes/nodes. On a write it
// publishes the affected keys; on receiving a peer's message it evicts those
// keys from the local L1. Messages from this process (matched by Origin) are
// ignored so a writer keeps its own fresh L1 entry.
type Invalidator struct {
	client   redis.UniversalClient
	channel  string
	originID string
	local    pcache.Cache // the L1 to evict from
	log      *slog.Logger
	ready    chan struct{}
}

// NewInvalidator constructs an Invalidator. originID must be unique per process.
func NewInvalidator(client redis.UniversalClient, originID string, local pcache.Cache, log *slog.Logger) *Invalidator {
	if log == nil {
		log = slog.Default()
	}
	return &Invalidator{
		client:   client,
		channel:  invalidateChannel,
		originID: originID,
		local:    local,
		log:      log,
		ready:    make(chan struct{}),
	}
}

// Publish broadcasts that keys have changed and should be evicted from peers'
// L1 caches.
func (iv *Invalidator) Publish(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	payload, err := json.Marshal(invalMsg{Origin: iv.originID, Keys: keys})
	if err != nil {
		return err
	}
	return iv.client.Publish(ctx, iv.channel, payload).Err()
}

// Ready is closed once the subscription is established. Useful in tests.
func (iv *Invalidator) Ready() <-chan struct{} { return iv.ready }

// Run subscribes and processes invalidations until ctx is cancelled. It is
// intended to run in its own goroutine.
func (iv *Invalidator) Run(ctx context.Context) error {
	pubsub := iv.client.Subscribe(ctx, iv.channel)
	defer func() { _ = pubsub.Close() }()

	// Block until the subscription is confirmed so callers/tests know the bus
	// is live before relying on it.
	if _, err := pubsub.Receive(ctx); err != nil {
		return err
	}
	close(iv.ready)

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			iv.handle(msg.Payload)
		}
	}
}

func (iv *Invalidator) handle(payload string) {
	var m invalMsg
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		iv.log.Warn("cache: bad invalidation message", "err", err)
		return
	}
	if m.Origin == iv.originID {
		return // our own write; keep our fresh L1 entry
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = iv.local.Delete(ctx, m.Keys...)
}

// invalidatingCache decorates a composed cache so that explicit Set/Delete
// operations also publish an invalidation. GetOrLoad's load-populate path does
// NOT publish: a value loaded from the source of truth is authoritative and
// needs no cross-process invalidation.
type invalidatingCache struct {
	inner pcache.Cache
	iv    *Invalidator
	log   *slog.Logger
}

func (c *invalidatingCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return c.inner.Get(ctx, key)
}

func (c *invalidatingCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := c.inner.Set(ctx, key, value, ttl); err != nil {
		return err
	}
	if err := c.iv.Publish(ctx, key); err != nil {
		c.log.Warn("cache: invalidation publish failed", "err", err, "op", "set")
	}
	return nil
}

func (c *invalidatingCache) Delete(ctx context.Context, keys ...string) error {
	if err := c.inner.Delete(ctx, keys...); err != nil {
		return err
	}
	if err := c.iv.Publish(ctx, keys...); err != nil {
		c.log.Warn("cache: invalidation publish failed", "err", err, "op", "delete")
	}
	return nil
}

func (c *invalidatingCache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader pcache.Loader) ([]byte, error) {
	return c.inner.GetOrLoad(ctx, key, ttl, loader)
}
