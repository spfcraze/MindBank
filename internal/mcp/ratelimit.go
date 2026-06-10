package mcp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter per IP.
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*bucket
	burst    int
	interval time.Duration
}

type bucket struct {
	tokens    int
	lastRefill time.Time
}

// NewRateLimiter creates a rate limiter with the given burst and interval.
// burst: max tokens per interval (e.g., 100 requests)
// interval: time between full refills (e.g., 10 seconds)
func NewRateLimiter(burst int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*bucket),
		burst:    burst,
		interval: interval,
	}
}

// Allow checks if a request from the given IP is allowed.
// Localhost (127.0.0.1, ::1) always bypasses rate limiting.
func (rl *RateLimiter) Allow(ip string) bool {
	// Always allow localhost for multi-session safety
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok {
		b = &bucket{tokens: rl.burst, lastRefill: time.Now()}
		rl.buckets[ip] = b
	}

	// Refill tokens based on elapsed time
	elapsed := time.Since(b.lastRefill)
	tokensToAdd := int(elapsed / rl.interval * time.Duration(rl.burst))
	if tokensToAdd > 0 {
		b.tokens = min(b.tokens+tokensToAdd, rl.burst)
		b.lastRefill = time.Now()
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}
	return false
}

// Middleware wraps an HTTP handler with rate limiting.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r.RemoteAddr)
		if !rl.Allow(ip) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractIP extracts the IP address from RemoteAddr (host:port).
func extractIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If no port, assume it's just an IP
		return strings.TrimSpace(remoteAddr)
	}
	return ip
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
