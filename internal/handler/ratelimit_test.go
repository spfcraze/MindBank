package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterStop(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)

	// Verify stop channel exists
	if rl.stopCh == nil {
		t.Fatal("expected stop channel to be initialized")
	}

	// Stop should not panic
	rl.Stop()

	// Verify we can create and stop multiple times
	rl2 := NewRateLimiter(10, time.Minute)
	rl2.Stop()
}

func TestRateLimiterMiddleware(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)
	defer rl.Stop()

	// Use a real HTTP request
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	// First request
	rec := httptest.NewRecorder()
	rl.Middleware(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code == 429 {
		t.Fatal("first request should be allowed")
	}

	// Second request
	rec = httptest.NewRecorder()
	rl.Middleware(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code == 429 {
		t.Fatal("second request should be allowed")
	}

	// Third request — rate limited
	rec = httptest.NewRecorder()
	rl.Middleware(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != 429 {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	// Wait for window
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	rec = httptest.NewRecorder()
	rl.Middleware(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code == 429 {
		t.Fatal("request after window should be allowed")
	}
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
