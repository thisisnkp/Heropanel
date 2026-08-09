package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func newTiered(t *testing.T) *TieredCache {
	t.Helper()
	l1 := NewLocal(LocalConfig{})
	l2 := NewLocal(LocalConfig{})
	t.Cleanup(func() { _ = l1.Close(); _ = l2.Close() })
	return NewTiered(l1, l2, TieredConfig{PromoteTTL: time.Minute})
}

func TestTieredSetWritesBothTiers(t *testing.T) {
	tc := newTiered(t)
	ctx := context.Background()

	if err := tc.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok, _ := tc.L1.Get(ctx, "k"); !ok {
		t.Fatal("L1 should hold the value")
	}
	if _, ok, _ := tc.L2.Get(ctx, "k"); !ok {
		t.Fatal("L2 should hold the value")
	}
}

func TestTieredPromotesFromL2(t *testing.T) {
	tc := newTiered(t)
	ctx := context.Background()

	// Seed only L2.
	_ = tc.L2.Set(ctx, "k", []byte("v"), time.Minute)

	// First read is an L2 hit that promotes into L1.
	if v, ok, _ := tc.Get(ctx, "k"); !ok || string(v) != "v" {
		t.Fatalf("expected L2 hit, got (%q,%v)", v, ok)
	}
	if _, ok, _ := tc.L1.Get(ctx, "k"); !ok {
		t.Fatal("value should have been promoted into L1")
	}

	l1Hits, l2Hits, _ := tc.TierStats()
	if l2Hits != 1 {
		t.Fatalf("l2Hits = %d, want 1", l2Hits)
	}

	// Second read is now an L1 hit.
	_, _, _ = tc.Get(ctx, "k")
	if l1Hits2, _, _ := tc.TierStats(); l1Hits2 != l1Hits+1 {
		t.Fatalf("expected an additional L1 hit, got %d", l1Hits2)
	}
}

func TestTieredL1OnlyWhenL2Nil(t *testing.T) {
	l1 := NewLocal(LocalConfig{})
	defer l1.Close()
	tc := NewTiered(l1, nil, TieredConfig{})
	ctx := context.Background()

	if err := tc.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, ok, _ := tc.Get(ctx, "k"); !ok || string(v) != "v" {
		t.Fatalf("L1-only get failed: (%q,%v)", v, ok)
	}
	if _, ok, _ := tc.Get(ctx, "absent"); ok {
		t.Fatal("expected miss")
	}
}

func TestTieredDeleteRemovesBoth(t *testing.T) {
	tc := newTiered(t)
	ctx := context.Background()

	_ = tc.Set(ctx, "k", []byte("v"), time.Minute)
	if err := tc.Delete(ctx, "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := tc.L1.Get(ctx, "k"); ok {
		t.Fatal("L1 should be cleared")
	}
	if _, ok, _ := tc.L2.Get(ctx, "k"); ok {
		t.Fatal("L2 should be cleared")
	}
}

func TestTieredGetOrLoadSingleFlight(t *testing.T) {
	tc := newTiered(t)
	ctx := context.Background()

	var calls int
	var mu sync.Mutex
	loader := func(ctx context.Context) ([]byte, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return []byte("loaded"), nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := tc.GetOrLoad(ctx, "k", time.Minute, loader); err != nil || string(v) != "loaded" {
				t.Errorf("got (%q,%v)", v, err)
			}
		}()
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("loader called %d times, want 1", calls)
	}
	// Result must have been written through to both tiers.
	if _, ok, _ := tc.L2.Get(ctx, "k"); !ok {
		t.Fatal("loaded value should be in L2")
	}
}

func TestNamespaceIsolation(t *testing.T) {
	base := NewLocal(LocalConfig{})
	defer base.Close()
	ctx := context.Background()

	a := Namespace(base, "rbac")
	b := Namespace(base, "sessions")

	_ = a.Set(ctx, "1", []byte("A"), time.Minute)
	_ = b.Set(ctx, "1", []byte("B"), time.Minute)

	if v, _, _ := a.Get(ctx, "1"); string(v) != "A" {
		t.Fatalf("namespace a leaked: %q", v)
	}
	if v, _, _ := b.Get(ctx, "1"); string(v) != "B" {
		t.Fatalf("namespace b leaked: %q", v)
	}
	// Deleting in one namespace must not affect the other.
	_ = a.Delete(ctx, "1")
	if _, ok, _ := b.Get(ctx, "1"); !ok {
		t.Fatal("delete crossed namespace boundary")
	}
}

func TestJSONHelpers(t *testing.T) {
	c := NewLocal(LocalConfig{})
	defer c.Close()
	ctx := context.Background()

	type user struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	want := user{ID: 7, Name: "ada"}

	if err := SetJSON(ctx, c, "u", want, time.Minute); err != nil {
		t.Fatalf("setjson: %v", err)
	}
	got, ok, err := GetJSON[user](ctx, c, "u")
	if err != nil || !ok || got != want {
		t.Fatalf("getjson got (%+v,%v,%v)", got, ok, err)
	}

	var loads int
	loaded, err := GetOrLoadJSON[user](ctx, c, "u2", time.Minute, func(ctx context.Context) (user, error) {
		loads++
		return user{ID: 9, Name: "lin"}, nil
	})
	if err != nil || loaded.ID != 9 {
		t.Fatalf("getorloadjson: (%+v,%v)", loaded, err)
	}
	// Second call should hit the cache, not the loader.
	_, _ = GetOrLoadJSON[user](ctx, c, "u2", time.Minute, func(ctx context.Context) (user, error) {
		loads++
		return user{}, nil
	})
	if loads != 1 {
		t.Fatalf("loader ran %d times, want 1", loads)
	}
}
