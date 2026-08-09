package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLocalSetGet(t *testing.T) {
	c := NewLocal(LocalConfig{})
	defer c.Close()
	ctx := context.Background()

	if _, ok, _ := c.Get(ctx, "missing"); ok {
		t.Fatal("expected miss for absent key")
	}
	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || string(v) != "v" {
		t.Fatalf("got (%q,%v,%v), want (\"v\",true,nil)", v, ok, err)
	}
}

func TestLocalExpiry(t *testing.T) {
	c := NewLocal(LocalConfig{})
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte("v"), 15*time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); !ok {
		t.Fatal("expected hit before expiry")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("expected miss after expiry")
	}
	if s := c.Stats(); s.Evictions == 0 {
		t.Fatal("expected an eviction recorded for the expired entry")
	}
}

func TestLocalLRUEviction(t *testing.T) {
	// 1 shard, capacity 3 -> inserting a 4th evicts the LRU.
	c := NewLocal(LocalConfig{Shards: 1, MaxEntries: 3})
	defer c.Close()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_ = c.Set(ctx, fmt.Sprintf("k%d", i), []byte("v"), 0)
	}
	// Touch k0 so k1 becomes the LRU.
	if _, ok, _ := c.Get(ctx, "k0"); !ok {
		t.Fatal("k0 should be present")
	}
	_ = c.Set(ctx, "k3", []byte("v"), 0) // evicts k1

	if _, ok, _ := c.Get(ctx, "k1"); ok {
		t.Fatal("k1 should have been evicted as LRU")
	}
	for _, k := range []string{"k0", "k2", "k3"} {
		if _, ok, _ := c.Get(ctx, k); !ok {
			t.Fatalf("%s should still be present", k)
		}
	}
	if c.Len() != 3 {
		t.Fatalf("len = %d, want 3", c.Len())
	}
}

func TestLocalDelete(t *testing.T) {
	c := NewLocal(LocalConfig{})
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "a", []byte("1"), 0)
	_ = c.Set(ctx, "b", []byte("2"), 0)
	_ = c.Delete(ctx, "a", "nope")
	if _, ok, _ := c.Get(ctx, "a"); ok {
		t.Fatal("a should be deleted")
	}
	if _, ok, _ := c.Get(ctx, "b"); !ok {
		t.Fatal("b should remain")
	}
}

func TestLocalMaxValueBytes(t *testing.T) {
	c := NewLocal(LocalConfig{MaxValueBytes: 4})
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "big", []byte("too large"), 0)
	if _, ok, _ := c.Get(ctx, "big"); ok {
		t.Fatal("oversized value should not be cached")
	}
	_ = c.Set(ctx, "ok", []byte("fit"), 0)
	if _, ok, _ := c.Get(ctx, "ok"); !ok {
		t.Fatal("small value should be cached")
	}
}

func TestLocalValueIsCopied(t *testing.T) {
	c := NewLocal(LocalConfig{})
	defer c.Close()
	ctx := context.Background()

	in := []byte("abc")
	_ = c.Set(ctx, "k", in, 0)
	in[0] = 'X' // mutate caller's slice after Set

	out, _, _ := c.Get(ctx, "k")
	if string(out) != "abc" {
		t.Fatalf("cache must not alias caller input; got %q", out)
	}
	out[0] = 'Y' // mutate returned slice
	again, _, _ := c.Get(ctx, "k")
	if string(again) != "abc" {
		t.Fatalf("cache must not alias returned slice; got %q", again)
	}
}

func TestLocalGetOrLoadSingleFlight(t *testing.T) {
	c := NewLocal(LocalConfig{})
	defer c.Close()
	ctx := context.Background()

	var calls int
	var mu sync.Mutex
	loader := func(ctx context.Context) ([]byte, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // widen the race window
		return []byte("loaded"), nil
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			v, err := c.GetOrLoad(ctx, "k", time.Minute, loader)
			if err != nil || string(v) != "loaded" {
				t.Errorf("got (%q,%v)", v, err)
			}
		}()
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("loader called %d times, want 1 (single-flight)", calls)
	}
}

func TestLocalConcurrentAccess(t *testing.T) {
	c := NewLocal(LocalConfig{Shards: 8, MaxEntries: 1024})
	defer c.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				k := fmt.Sprintf("g%d-k%d", g, i%64)
				_ = c.Set(ctx, k, []byte("v"), time.Minute)
				_, _, _ = c.Get(ctx, k)
				if i%10 == 0 {
					_ = c.Delete(ctx, k)
				}
			}
		}(g)
	}
	wg.Wait()
}
