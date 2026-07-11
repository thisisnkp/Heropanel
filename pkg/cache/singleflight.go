package cache

import (
	"fmt"
	"sync"
)

// FlightGroup deduplicates concurrent calls for the same key so an expensive
// loader runs once while other goroutines wait for and share its result.
//
// It is a small, self-contained equivalent of golang.org/x/sync/singleflight,
// reimplemented here to keep pkg/cache free of external dependencies. A panic
// inside the loader is recovered and surfaced as an error so waiters never
// deadlock. The zero value is ready to use.
type FlightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	val []byte
	err error
}

// Do executes fn for key, ensuring only one execution is in flight for a given
// key at a time. Duplicate callers wait for the original to complete and
// receive the same results.
func (g *FlightGroup) Do(key string, fn func() ([]byte, error)) ([]byte, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := new(flightCall)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = safeCall(fn)
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}

// safeCall runs fn, converting a panic into an error.
func safeCall(fn func() ([]byte, error)) (val []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			val, err = nil, fmt.Errorf("cache: loader panic: %v", r)
		}
	}()
	return fn()
}
