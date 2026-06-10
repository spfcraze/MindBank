package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(10, time.Second) // 10 req/sec

	// Should allow 10 requests from non-localhost IP
	for i := 0; i < 10; i++ {
		if !rl.Allow("192.168.1.50") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 11th should be blocked
	if rl.Allow("192.168.1.50") {
		t.Error("11th request should be blocked")
	}
}

func TestRateLimiterLocalhostBypass(t *testing.T) {
	rl := NewRateLimiter(1, time.Second) // strict: 1 req/sec

	// Localhost should always bypass
	for i := 0; i < 100; i++ {
		if !rl.Allow("127.0.0.1") {
			t.Errorf("localhost request %d should bypass limit", i+1)
		}
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(5, time.Second)

	// IP1 uses all tokens
	for i := 0; i < 5; i++ {
		rl.Allow("192.168.1.1")
	}

	// IP2 should still have full quota
	for i := 0; i < 5; i++ {
		if !rl.Allow("192.168.1.2") {
			t.Errorf("IP2 request %d should be allowed independently", i+1)
		}
	}

	// IP2 6th blocked
	if rl.Allow("192.168.1.2") {
		t.Error("IP2 6th request should be blocked")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	rl := NewRateLimiter(2, 100*time.Millisecond)

	// Use both tokens
	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.1")
	if rl.Allow("10.0.0.1") {
		t.Error("3rd request should be blocked")
	}

	// Wait for refill
	time.Sleep(150 * time.Millisecond)
	if !rl.Allow("10.0.0.1") {
		t.Error("request after refill should be allowed")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(2, time.Second)
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 2 requests allowed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rr.Code)
		}
	}

	// 3rd blocked
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
}
