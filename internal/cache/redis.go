// Package cache wires HeroPanel's two-tier cache: it adapts Redis as the L2
// tier behind the pkg/cache.Cache interface, composes it with an in-process L1
// (pkg/cache.LocalCache), and maintains cross-process coherence via a Redis
// Pub/Sub invalidation bus. See docs/01 §3.4 and ADR-0005.
//
// pkg/cache is imported as pcache to avoid a name clash with this package.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// keyPrefix namespaces cache entries in Redis so they never collide with the
// queue or Pub/Sub keyspaces.
const keyPrefix = "hp:c:"

// RedisCache is the L2 (shared, distributed) tier. It implements pcache.Cache.
type RedisCache struct {
	client redis.UniversalClient
	sf     pcache.FlightGroup
}

// NewRedisCache adapts a redis client as an L2 cache.
func NewRedisCache(client redis.UniversalClient) *RedisCache {
	return &RedisCache{client: client}
}

func (c *RedisCache) k(key string) string { return keyPrefix + key }

// Get implements pcache.Cache.
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := c.client.Get(ctx, c.k(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// Set implements pcache.Cache. A ttl <= 0 stores the entry without expiry.
func (c *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return c.client.Set(ctx, c.k(key), value, ttl).Err()
}

// Delete implements pcache.Cache.
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	pk := make([]string, len(keys))
	for i, key := range keys {
		pk[i] = c.k(key)
	}
	return c.client.Del(ctx, pk...).Err()
}

// GetOrLoad implements pcache.Cache with single-flight stampede protection.
func (c *RedisCache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader pcache.Loader) ([]byte, error) {
	if v, ok, err := c.Get(ctx, key); err != nil {
		return nil, err
	} else if ok {
		return v, nil
	}
	v, err := c.sf.Do(key, func() ([]byte, error) {
		if v, ok, err := c.Get(ctx, key); err != nil {
			return nil, err
		} else if ok {
			return v, nil
		}
		return loader(ctx)
	})
	if err != nil {
		return nil, err
	}
	_ = c.Set(ctx, key, v, ttl)
	return v, nil
}

// Health pings Redis; used by the readiness probe.
func (c *RedisCache) Health(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
