package cache

import (
	"container/list"
	"context"
	"hash/maphash"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults for LocalConfig.
const (
	defaultShards     = 16
	defaultMaxEntries = 10000
)

// LocalConfig configures a LocalCache (the L1 "normal" in-process cache).
type LocalConfig struct {
	// Shards is the number of independently-locked shards. Rounded up to the
	// next power of two. More shards reduce lock contention under concurrency.
	// Default: 16.
	Shards int

	// MaxEntries is the total soft cap on cached entries across all shards.
	// When exceeded, least-recently-used entries are evicted. Default: 10000.
	MaxEntries int

	// MaxValueBytes, if > 0, causes Set to silently skip values larger than
	// this many bytes (they are simply not cached). Default: 0 (no limit).
	MaxValueBytes int

	// JanitorInterval, if > 0, runs a background sweep that proactively removes
	// expired entries. If 0, expiry is handled lazily on read only.
	JanitorInterval time.Duration
}

func (c LocalConfig) withDefaults() LocalConfig {
	if c.Shards <= 0 {
		c.Shards = defaultShards
	}
	c.Shards = nextPow2(c.Shards)
	if c.MaxEntries <= 0 {
		c.MaxEntries = defaultMaxEntries
	}
	return c
}

// LocalCache is an in-process, sharded LRU cache with per-entry TTL. It is the
// L1 tier: fast, bounded, and safe for concurrent use. It holds hot, small,
// read-heavy data (RBAC sets, sessions, settings, capability set, per-site
// config, ...). It implements Cache and StatsReporter.
type LocalCache struct {
	cfg    LocalConfig
	seed   maphash.Seed
	mask   uint64
	shards []*localShard
	sf     flightGroup

	stats struct {
		hits, misses, sets, deletes, evictions atomic.Uint64
	}

	closeOnce sync.Once
	closed    chan struct{}
}

type localShard struct {
	mu    sync.Mutex
	items map[string]*list.Element // key -> element holding *localEntry
	ll    *list.List               // front = most-recently-used
	cap   int
}

type localEntry struct {
	key      string
	val      []byte
	expireAt int64 // unix nanoseconds; 0 = never expires on time
}

// NewLocal builds a LocalCache from cfg. Call Close to stop the background
// janitor (if any).
func NewLocal(cfg LocalConfig) *LocalCache {
	cfg = cfg.withDefaults()
	c := &LocalCache{
		cfg:    cfg,
		seed:   maphash.MakeSeed(),
		mask:   uint64(cfg.Shards - 1),
		shards: make([]*localShard, cfg.Shards),
		closed: make(chan struct{}),
	}
	// Distribute the entry budget evenly across shards (at least 1 each).
	perShard := cfg.MaxEntries / cfg.Shards
	if perShard < 1 {
		perShard = 1
	}
	for i := range c.shards {
		c.shards[i] = &localShard{
			items: make(map[string]*list.Element),
			ll:    list.New(),
			cap:   perShard,
		}
	}
	if cfg.JanitorInterval > 0 {
		go c.janitor(cfg.JanitorInterval)
	}
	return c
}

func (c *LocalCache) shardFor(key string) *localShard {
	h := maphash.String(c.seed, key)
	return c.shards[h&c.mask]
}

// Get implements Cache.
func (c *LocalCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	now := time.Now().UnixNano()
	s := c.shardFor(key)

	s.mu.Lock()
	val, ok, expired := s.get(key, now)
	s.mu.Unlock()

	switch {
	case ok:
		c.stats.hits.Add(1)
		return clone(val), true, nil
	case expired:
		c.stats.evictions.Add(1)
	}
	c.stats.misses.Add(1)
	return nil, false, nil
}

// Set implements Cache. A ttl <= 0 stores an entry that never expires on time.
func (c *LocalCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if c.cfg.MaxValueBytes > 0 && len(value) > c.cfg.MaxValueBytes {
		return nil // too large to cache; not an error
	}
	var expireAt int64
	if ttl > 0 {
		expireAt = time.Now().Add(ttl).UnixNano()
	}
	s := c.shardFor(key)

	s.mu.Lock()
	evicted := s.set(key, clone(value), expireAt)
	s.mu.Unlock()

	c.stats.sets.Add(1)
	if evicted {
		c.stats.evictions.Add(1)
	}
	return nil
}

// Delete implements Cache.
func (c *LocalCache) Delete(_ context.Context, keys ...string) error {
	for _, key := range keys {
		s := c.shardFor(key)
		s.mu.Lock()
		removed := s.del(key)
		s.mu.Unlock()
		if removed {
			c.stats.deletes.Add(1)
		}
	}
	return nil
}

// GetOrLoad implements Cache with single-flight stampede protection.
func (c *LocalCache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader Loader) ([]byte, error) {
	if v, ok, err := c.Get(ctx, key); err != nil {
		return nil, err
	} else if ok {
		return v, nil
	}
	v, err := c.sf.Do(key, func() ([]byte, error) {
		// Re-check under the flight in case a peer populated the key.
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
	return clone(v), nil
}

// Stats implements StatsReporter.
func (c *LocalCache) Stats() Stats {
	return Stats{
		Hits:      c.stats.hits.Load(),
		Misses:    c.stats.misses.Load(),
		Sets:      c.stats.sets.Load(),
		Deletes:   c.stats.deletes.Load(),
		Evictions: c.stats.evictions.Load(),
	}
}

// Len returns the current number of cached entries (approximate under load).
func (c *LocalCache) Len() int {
	n := 0
	for _, s := range c.shards {
		s.mu.Lock()
		n += s.ll.Len()
		s.mu.Unlock()
	}
	return n
}

// Close stops the background janitor. It is safe to call multiple times.
func (c *LocalCache) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *LocalCache) janitor(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			now := time.Now().UnixNano()
			for _, s := range c.shards {
				s.mu.Lock()
				n := s.sweep(now)
				s.mu.Unlock()
				if n > 0 {
					c.stats.evictions.Add(uint64(n))
				}
			}
		}
	}
}

// ── shard operations (caller holds s.mu) ────────────────────────────────────

// get returns (value, found, expired). expired is true when the key existed but
// was past its TTL (and has now been removed).
func (s *localShard) get(key string, now int64) (val []byte, found, expired bool) {
	elem, ok := s.items[key]
	if !ok {
		return nil, false, false
	}
	e := elem.Value.(*localEntry)
	if e.expireAt != 0 && now >= e.expireAt {
		s.ll.Remove(elem)
		delete(s.items, key)
		return nil, false, true
	}
	s.ll.MoveToFront(elem)
	return e.val, true, false
}

// set inserts or updates key and reports whether an LRU eviction occurred.
func (s *localShard) set(key string, val []byte, expireAt int64) (evicted bool) {
	if elem, ok := s.items[key]; ok {
		e := elem.Value.(*localEntry)
		e.val = val
		e.expireAt = expireAt
		s.ll.MoveToFront(elem)
		return false
	}
	elem := s.ll.PushFront(&localEntry{key: key, val: val, expireAt: expireAt})
	s.items[key] = elem
	if s.ll.Len() > s.cap {
		if back := s.ll.Back(); back != nil {
			be := back.Value.(*localEntry)
			s.ll.Remove(back)
			delete(s.items, be.key)
			return true
		}
	}
	return false
}

func (s *localShard) del(key string) bool {
	elem, ok := s.items[key]
	if !ok {
		return false
	}
	s.ll.Remove(elem)
	delete(s.items, key)
	return true
}

// sweep removes all expired entries and returns how many were removed.
func (s *localShard) sweep(now int64) int {
	removed := 0
	for elem := s.ll.Back(); elem != nil; {
		prev := elem.Prev()
		e := elem.Value.(*localEntry)
		if e.expireAt != 0 && now >= e.expireAt {
			s.ll.Remove(elem)
			delete(s.items, e.key)
			removed++
		}
		elem = prev
	}
	return removed
}

func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
