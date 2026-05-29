package antibot

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// RateLimiterConfig configures the IP rate limiter.
type RateLimiterConfig struct {
	// Max requests allowed within Window. Default: 3.
	Max int
	// Sliding window duration. Default: 1 hour.
	Window time.Duration
	// How often stale entries are purged. Default: 5 minutes.
	CleanupInterval time.Duration
	// Injectable clock for testing. Default: time.Now.
	Now func() time.Time
}

// IPRateLimiter tracks per-IP request timestamps and rejects IPs
// that exceed the configured rate.
type IPRateLimiter struct {
	mu       sync.Mutex
	hits     map[string][]time.Time
	max      int
	window   time.Duration
	now      func() time.Time
	stopOnce sync.Once
	done     chan struct{}
}

// NewIPRateLimiter creates a rate limiter and starts a background cleanup
// goroutine. Call Stop() when done to release the goroutine.
func NewIPRateLimiter(cfg RateLimiterConfig) *IPRateLimiter {
	if cfg.Max <= 0 {
		cfg.Max = 3
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Hour
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	l := &IPRateLimiter{
		hits:   make(map[string][]time.Time),
		max:    cfg.Max,
		window: cfg.Window,
		now:    cfg.Now,
		done:   make(chan struct{}),
	}

	go l.cleanup(cfg.CleanupInterval)
	return l
}

// Stop halts the background cleanup goroutine.
func (l *IPRateLimiter) Stop() {
	l.stopOnce.Do(func() { close(l.done) })
}

// Middleware returns HTTP middleware that enforces the rate limit.
// Rejections return 200 with an HTML error fragment.
func (l *IPRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			ip := clientIP(r)
			if !l.allow(ip) {
				log.Printf("antibot: rate limit exceeded for %s", ip)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = fmt.Fprint(w, `<p class="error">Too many requests. Please try again later.</p>`)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// allow records a hit for the IP and returns whether the request is allowed.
func (l *IPRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)

	// Prune expired entries for this IP.
	timestamps := l.hits[ip]
	valid := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.max {
		l.hits[ip] = valid
		return false
	}

	l.hits[ip] = append(valid, now)
	return true
}

// cleanup periodically removes stale IP entries.
func (l *IPRateLimiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			l.mu.Lock()
			now := l.now()
			cutoff := now.Add(-l.window)
			for ip, timestamps := range l.hits {
				valid := timestamps[:0]
				for _, t := range timestamps {
					if t.After(cutoff) {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(l.hits, ip)
				} else {
					l.hits[ip] = valid
				}
			}
			l.mu.Unlock()
		}
	}
}
