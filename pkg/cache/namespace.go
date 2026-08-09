package cache

import (
	"context"
	"time"
)

// Namespace returns a view of c whose keys are transparently prefixed with
// "<prefix>:". This lets different subsystems (rbac, sessions, settings, dns,
// ...) share one underlying cache without key collisions and invalidate their
// own namespace independently.
func Namespace(c Cache, prefix string) Cache {
	return &namespaced{c: c, prefix: prefix + ":"}
}

type namespaced struct {
	c      Cache
	prefix string
}

func (n *namespaced) key(k string) string { return n.prefix + k }

func (n *namespaced) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return n.c.Get(ctx, n.key(key))
}

func (n *namespaced) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return n.c.Set(ctx, n.key(key), value, ttl)
}

func (n *namespaced) Delete(ctx context.Context, keys ...string) error {
	pk := make([]string, len(keys))
	for i, k := range keys {
		pk[i] = n.key(k)
	}
	return n.c.Delete(ctx, pk...)
}

func (n *namespaced) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader Loader) ([]byte, error) {
	return n.c.GetOrLoad(ctx, n.key(key), ttl, loader)
}
