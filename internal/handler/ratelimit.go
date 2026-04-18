package handler

import (
	"net/http"
	"sync"
	"time"
)

// SimpleRateLimiter provides per-IP rate limiting.
type SimpleRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	rate    int           // requests per window
	window  time.Duration // time window
	maxIPs  int           // max tracked IPs (prevents memory exhaustion)
}

type clientBucket struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a rate limiter (requests per window).
func NewRateLimiter(rate int, window time.Duration) *SimpleRateLimiter {
	rl := &SimpleRateLimiter{
		clients: make(map[string]*clientBucket),
		rate:    rate,
		window:  window,
		maxIPs:  10000, // cap at 10k unique IPs
	}
	// Cleanup old entries every minute
	go func() {
		for range time.Tick(time.Minute) {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *SimpleRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for ip, b := range rl.clients {
		if now.After(b.resetAt) {
			delete(rl.clients, ip)
		}
	}
}

// Middleware returns rate limiting middleware.
func (rl *SimpleRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr

		rl.mu.Lock()
		b, exists := rl.clients[ip]
		now := time.Now()

		if !exists || now.After(b.resetAt) {
			// Check map size cap to prevent memory exhaustion
			if !exists && len(rl.clients) >= rl.maxIPs {
				rl.mu.Unlock()
				// Allow request but don't track (fail-open)
				next.ServeHTTP(w, r)
				return
			}
			rl.clients[ip] = &clientBucket{count: 1, resetAt: now.Add(rl.window)}
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}

		b.count++
		if b.count > rl.rate {
			rl.mu.Unlock()
			w.Header().Set("Retry-After", "60")
			respondError(w, 429, "rate limit exceeded")
			return
		}
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
