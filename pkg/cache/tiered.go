package cache

import (
	"context"
	"sync/atomic"
	"time"
)

// TieredConfig configures a TieredCache.
type TieredConfig struct {
	// PromoteTTL is the TTL used when an L2 hit is copied up into L1. If 0, a
	// small default is used. Kept short so L1 stays fresh and coherent.
	PromoteTTL time.Duration
}

const defaultPromoteTTL = 30 * time.Second

// TieredCache composes an L1 cache (fast, in-process) in front of an L2 cache
// (shared, e.g. Redis). Reads go L1 -> L2 -> loader; L2 hits are promoted into
// L1; writes and deletes fan out to both tiers. L2 may be nil, in which case
// the tiered cache degrades to L1-only (minimal/SQLite installs without Redis).
//
// Coherence across processes/nodes is maintained by the L2 layer publishing
// invalidations (see internal/cache) which drop L1 entries; PromoteTTL is a
// time-based backstop on top of that.
type TieredCache struct {
	L1  Cache
	L2  Cache // may be nil
	cfg TieredConfig
	sf  FlightGroup

	stats struct {
		l1Hits, l2Hits, misses atomic.Uint64
	}
}

// NewTiered composes l1 and l2 into a TieredCache. l1 must be non-nil; l2 may be
// nil for an L1-only cache.
func NewTiered(l1, l2 Cache, cfg TieredConfig) *TieredCache {
	if cfg.PromoteTTL <= 0 {
		cfg.PromoteTTL = defaultPromoteTTL
	}
	return &TieredCache{L1: l1, L2: l2, cfg: cfg}
}

// Get implements Cache: L1 first, then L2 (promoting the value into L1).
func (t *TieredCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if v, ok, err := t.L1.Get(ctx, key); err != nil {
		return nil, false, err
	} else if ok {
		t.stats.l1Hits.Add(1)
		return v, true, nil
	}
	if t.L2 == nil {
		t.stats.misses.Add(1)
		return nil, false, nil
	}
	v, ok, err := t.L2.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if ok {
		t.stats.l2Hits.Add(1)
		_ = t.L1.Set(ctx, key, v, t.cfg.PromoteTTL)
		return v, true, nil
	}
	t.stats.misses.Add(1)
	return nil, false, nil
}

// Set implements Cache: writes to L2 (shared, authoritative) then L1.
func (t *TieredCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if t.L2 != nil {
		if err := t.L2.Set(ctx, key, value, ttl); err != nil {
			return err
		}
	}
	// L1 TTL is capped by PromoteTTL so a long-lived L2 entry doesn't pin a
	// stale copy in-process beyond the coherence backstop.
	l1ttl := ttl
	if l1ttl <= 0 || l1ttl > t.cfg.PromoteTTL {
		l1ttl = t.cfg.PromoteTTL
	}
	return t.L1.Set(ctx, key, value, l1ttl)
}

// Delete implements Cache: removes from both tiers.
func (t *TieredCache) Delete(ctx context.Context, keys ...string) error {
	if t.L2 != nil {
		if err := t.L2.Delete(ctx, keys...); err != nil {
			return err
		}
	}
	return t.L1.Delete(ctx, keys...)
}

// GetOrLoad implements Cache with single-flight across both tiers.
func (t *TieredCache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader Loader) ([]byte, error) {
	if v, ok, err := t.Get(ctx, key); err != nil {
		return nil, err
	} else if ok {
		return v, nil
	}
	v, err := t.sf.Do(key, func() ([]byte, error) {
		if v, ok, err := t.Get(ctx, key); err != nil {
			return nil, err
		} else if ok {
			return v, nil
		}
		return loader(ctx)
	})
	if err != nil {
		return nil, err
	}
	if err := t.Set(ctx, key, v, ttl); err != nil {
		return nil, err
	}
	return clone(v), nil
}

// Stats reports tiered hit/miss counters.
func (t *TieredCache) Stats() Stats {
	l1 := t.stats.l1Hits.Load()
	l2 := t.stats.l2Hits.Load()
	return Stats{
		Hits:   l1 + l2,
		Misses: t.stats.misses.Load(),
	}
}

// TierStats breaks hits down by tier for observability.
func (t *TieredCache) TierStats() (l1Hits, l2Hits, misses uint64) {
	return t.stats.l1Hits.Load(), t.stats.l2Hits.Load(), t.stats.misses.Load()
}
