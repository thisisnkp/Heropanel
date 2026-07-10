package cache

import (
	"context"
	"encoding/json"
	"time"
)

// Typed JSON helpers layer struct (de)serialization over the byte-oriented
// Cache interface. They are the ergonomic entry point for most call sites.

// GetJSON reads key and unmarshals it into T. found is false on a miss.
func GetJSON[T any](ctx context.Context, c Cache, key string) (value T, found bool, err error) {
	var zero T
	b, ok, err := c.Get(ctx, key)
	if err != nil || !ok {
		return zero, ok, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return zero, false, err
	}
	return v, true, nil
}

// SetJSON marshals v and stores it under key with ttl.
func SetJSON[T any](ctx context.Context, c Cache, key string, v T, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl)
}

// GetOrLoadJSON returns key as T, invoking loader (once, via single-flight) on a
// miss and caching the marshaled result with ttl.
func GetOrLoadJSON[T any](ctx context.Context, c Cache, key string, ttl time.Duration, loader func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	b, err := c.GetOrLoad(ctx, key, ttl, func(ctx context.Context) ([]byte, error) {
		v, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(v)
	})
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return zero, err
	}
	return v, nil
}
