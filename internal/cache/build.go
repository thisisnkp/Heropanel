package cache

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/thisisnkp/heropanel/internal/config"
	pcache "github.com/thisisnkp/heropanel/pkg/cache"
)

// Wiring is the assembled cache and its owned resources.
type Wiring struct {
	// Cache is the fully-composed cache the application uses.
	Cache pcache.Cache
	// RedisHealth is the L2 health checker, or nil when Redis is disabled.
	RedisHealth *RedisCache
	// Close releases owned resources (the Redis client).
	Close func() error
}

// Configured reports whether Redis is enabled (a non-empty address).
func Configured(cfg config.Redis) bool { return strings.TrimSpace(cfg.Addr) != "" }

// Build composes the two-tier cache from cfg:
//   - Redis configured   -> TieredCache{L1, RedisL2} wrapped with the Pub/Sub
//     invalidation bus (the bus is started on a goroutine bound to ctx).
//   - Redis not configured -> L1-only (single process; no cross-process
//     coherence is needed).
//
// originID must uniquely identify this process (e.g. a per-boot ULID).
func Build(ctx context.Context, cfg config.Redis, l1 *pcache.LocalCache, originID string, log *slog.Logger) (Wiring, error) {
	if log == nil {
		log = slog.Default()
	}
	if !Configured(cfg) {
		log.Info("cache: Redis disabled; running L1-only")
		return Wiring{
			Cache: pcache.NewTiered(l1, nil, pcache.TieredConfig{}),
			Close: func() error { return nil },
		}, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return Wiring{}, fmt.Errorf("cache: connect Redis %s: %w", cfg.Addr, err)
	}

	l2 := NewRedisCache(client)
	tiered := pcache.NewTiered(l1, l2, pcache.TieredConfig{})

	iv := NewInvalidator(client, originID, l1, log)
	go func() {
		if err := iv.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("cache: invalidation bus stopped", "err", err)
		}
	}()

	log.Info("cache: two-tier enabled (L1 + Redis L2 with invalidation bus)", "addr", cfg.Addr)
	return Wiring{
		Cache:       &invalidatingCache{inner: tiered, iv: iv, log: log},
		RedisHealth: l2,
		Close:       client.Close,
	}, nil
}
