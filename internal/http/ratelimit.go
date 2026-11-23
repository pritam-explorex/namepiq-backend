package httpapi

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type rateBucket struct {
	lim  *rate.Limiter
	last time.Time
}

// rateLimiter is a keyed token-bucket store (IP or user).
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

func newRateLimiter(every time.Duration, burst int) *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*rateBucket{},
		rate:    rate.Every(every),
		burst:   burst,
		ttl:     30 * time.Minute,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &rateBucket{lim: rate.NewLimiter(rl.rate, rl.burst), last: now}
		rl.buckets[key] = b
	}
	b.last = now
	// occasional prune
	if len(rl.buckets) > 5000 {
		for k, v := range rl.buckets {
			if now.Sub(v.last) > rl.ttl {
				delete(rl.buckets, k)
			}
		}
	}
	return b.lim.Allow()
}

func (s *Server) rateLimit(rl *rateLimiter, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				key = clientIP(r)
			}
			if !rl.allow(key) {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests — try again shortly")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) rateKeyIP(r *http.Request) string {
	return "ip:" + clientIP(r)
}

func (s *Server) rateKeyUserOrIP(r *http.Request) string {
	if u, ok := UserFromContext(r.Context()); ok {
		return "user:" + u.ID.String()
	}
	return s.rateKeyIP(r)
}
