// Package cache provides HeroPanel's two-tier caching primitives.
//
// The panel uses a two-tier cache (see docs/01-architecture.md §3.4):
//
//   - LocalCache  — an in-process ("normal") sharded LRU cache with TTL. This
//     is tier L1: nanosecond reads, no network hop.
//   - RedisCache  — a distributed cache backed by Redis (tier L2), wired up in
//     internal/cache where the Redis client dependency lives.
//   - TieredCache — composes an L1 in front of an L2, with the read path
//     L1 -> L2 -> loader and single-flight stampede protection.
//
// This package is intentionally dependency-light (standard library only) so it
// can be reused by both the core (hpd) and satellite modules via the plugin SDK
// without pulling heavy transitive dependencies — protecting the RAM budget.
//
// Value semantics: implementations copy values on both Set and Get, so callers
// may freely mutate the byte slices they pass in or receive back without
// corrupting cached data.
package cache

import (
	"context"
	"time"
)

// Cache is the single interface every tier implements. Call sites depend only
// on this interface and never need to know which tier served a read.
type Cache interface {
	// Get returns the cached value for key. found is false (with a nil error)
	// on a cache miss. The returned slice is a private copy.
	Get(ctx context.Context, key string) (value []byte, found bool, err error)

	// Set stores value under key with the given ttl. A ttl <= 0 means the entry
	// does not expire on time and is only subject to capacity (LRU) eviction.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes the given keys. Deleting an absent key is not an error.
	Delete(ctx context.Context, keys ...string) error

	// GetOrLoad returns the cached value for key, or invokes loader exactly once
	// (per process, via single-flight) on a miss, caches the result with ttl,
	// and returns it. Concurrent callers for the same key share one load.
	GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader Loader) ([]byte, error)
}

// Loader computes a value on a cache miss for GetOrLoad.
type Loader func(ctx context.Context) ([]byte, error)

// Stats is a point-in-time snapshot of cache counters.
type Stats struct {
	Hits      uint64 // reads served from this cache
	Misses    uint64 // reads not present in this cache
	Sets      uint64 // successful Set operations
	Deletes   uint64 // keys removed via Delete
	Evictions uint64 // entries dropped due to capacity or expiry
}

// StatsReporter is implemented by caches that expose counters.
type StatsReporter interface {
	Stats() Stats
}

// clone returns a private copy of b (nil-safe). Used to enforce value copy
// semantics so cached data cannot be mutated through a returned slice.
func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
