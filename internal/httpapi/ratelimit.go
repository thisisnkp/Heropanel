package httpapi

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/thisisnkp/heropanel/internal/config"
)

// rateLimiter is a simple per-client token-bucket limiter kept in process. It is
// the L1/dev limiter; Redis-backed distributed limiting replaces/augments it
// when Redis is wired (docs/04 §5). A janitor evicts idle clients so the map
// cannot grow unbounded.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*bucket
	rps      float64
	burst    float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(ctx context.Context, cfg config.RateLimit) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*bucket),
		rps:      cfg.RPS,
		burst:    float64(cfg.Burst),
	}
	go rl.janitor(ctx)
	return rl
}

// allow reports whether a request from key may proceed, consuming one token.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.visitors[key]
	if !ok {
		rl.visitors[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}
	// Refill based on elapsed time, capped at burst.
	b.tokens += now.Sub(b.last).Seconds() * rl.rps
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) janitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cutoff := time.Now().Add(-3 * time.Minute)
			rl.mu.Lock()
			for k, b := range rl.visitors {
				if b.last.Before(cutoff) {
					delete(rl.visitors, k)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *rateLimiter) middleware() mw {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientIP(r)) {
				w.Header().Set("Retry-After", "1")
				writeAPIError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many requests.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the client address. RealIP middleware has already
// normalized r.RemoteAddr from trusted proxy headers where applicable.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
