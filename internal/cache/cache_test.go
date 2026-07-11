package cache_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	icache "github.com/thisisnkp/heropanel/internal/cache"
	"github.com/thisisnkp/heropanel/internal/config"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close(); mr.Close() })
	return mr, client
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRedisCacheGetSetDelete(t *testing.T) {
	_, client := newMiniRedis(t)
	c := icache.NewRedisCache(client)
	ctx := context.Background()

	if _, ok, _ := c.Get(ctx, "missing"); ok {
		t.Fatal("expected miss")
	}
	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || string(v) != "v" {
		t.Fatalf("get = (%q,%v,%v)", v, ok, err)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("expected miss after delete")
	}
}

func TestRedisCacheGetOrLoad(t *testing.T) {
	_, client := newMiniRedis(t)
	c := icache.NewRedisCache(client)
	ctx := context.Background()

	var calls int
	load := func(ctx context.Context) ([]byte, error) {
		calls++
		return []byte("loaded"), nil
	}
	for i := 0; i < 3; i++ {
		v, err := c.GetOrLoad(ctx, "k", time.Minute, load)
		if err != nil || string(v) != "loaded" {
			t.Fatalf("getorload = (%q,%v)", v, err)
		}
	}
	if calls != 1 {
		t.Fatalf("loader called %d times, want 1 (subsequent from cache)", calls)
	}
}

func TestBuildL1OnlyWhenRedisDisabled(t *testing.T) {
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })

	w, err := icache.Build(context.Background(), config.Redis{Addr: ""}, l1, "origin", discardLogger())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if w.RedisHealth != nil {
		t.Fatal("expected no Redis health checker when disabled")
	}
	ctx := context.Background()
	if err := w.Cache.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, ok, _ := w.Cache.Get(ctx, "k"); !ok || string(v) != "v" {
		t.Fatalf("L1-only get failed: (%q,%v)", v, ok)
	}
}

func TestBuildTieredReadThroughAndHealth(t *testing.T) {
	mr, _ := newMiniRedis(t)
	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	w, err := icache.Build(ctx, config.Redis{Addr: mr.Addr()}, l1, "origin", discardLogger())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if w.RedisHealth == nil {
		t.Fatal("expected a Redis health checker when enabled")
	}
	if err := w.RedisHealth.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}

	// Write goes through to L2; a fresh L1 then reads it back (promotion).
	if err := w.Cache.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	l1.Delete(ctx, "k") // simulate cold L1
	if v, ok, _ := w.Cache.Get(ctx, "k"); !ok || string(v) != "v" {
		t.Fatalf("read-through from L2 failed: (%q,%v)", v, ok)
	}
}

// TestInvalidationEvictsPeerL1 simulates two processes (A and B) sharing Redis:
// a write on A must evict the key from B's in-process L1.
func TestInvalidationEvictsPeerL1(t *testing.T) {
	mr, clientA := newMiniRedis(t)
	clientB := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = clientB.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Process B: local L1 + a running invalidator.
	l1B := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1B.Close() })
	ivB := icache.NewInvalidator(clientB, "B", l1B, discardLogger())
	go func() { _ = ivB.Run(ctx) }()
	select {
	case <-ivB.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("invalidator B did not become ready")
	}

	// B has a cached value in its L1.
	if err := l1B.Set(ctx, "k", []byte("stale"), time.Hour); err != nil {
		t.Fatal(err)
	}

	// Process A publishes an invalidation for "k".
	ivA := icache.NewInvalidator(clientA, "A", pcache.NewLocal(pcache.LocalConfig{}), discardLogger())
	if err := ivA.Publish(ctx, "k"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// B's L1 entry must disappear.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok, _ := l1B.Get(ctx, "k"); !ok {
			break // evicted — success
		}
		if time.Now().After(deadline) {
			t.Fatal("peer L1 was not invalidated in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestInvalidationIgnoresOwnOrigin ensures a process does not evict its own L1
// on its own published message.
func TestInvalidationIgnoresOwnOrigin(t *testing.T) {
	mr, client := newMiniRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	l1 := pcache.NewLocal(pcache.LocalConfig{})
	t.Cleanup(func() { _ = l1.Close() })
	iv := icache.NewInvalidator(client, "SELF", l1, discardLogger())
	go func() { _ = iv.Run(ctx) }()
	select {
	case <-iv.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("invalidator did not become ready")
	}

	_ = l1.Set(ctx, "k", []byte("mine"), time.Hour)
	if err := iv.Publish(ctx, "k"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Give the message time to be delivered; the entry must survive.
	time.Sleep(200 * time.Millisecond)
	if _, ok, _ := l1.Get(ctx, "k"); !ok {
		t.Fatal("own-origin invalidation should not evict our L1 entry")
	}
	_ = mr
}
