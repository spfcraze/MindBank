package mcp

import (
	"testing"
	"time"
)

func TestHealingBackoff(t *testing.T) {
	h := NewHealer("/bin/true", 3) // 3 max attempts

	// First attempt should allow immediately
	if !h.ShouldAttempt() {
		t.Error("first attempt should be allowed")
	}

	// Record failure
	h.RecordFailure()

	// After 1 failure, backoff = 2s (1 << 1 = 2)
	// Wait for backoff
	time.Sleep(2100 * time.Millisecond)

	// Second attempt should allow (count < max)
	if !h.ShouldAttempt() {
		t.Error("second attempt should be allowed")
	}

	// Record 2 more failures with waits
	h.RecordFailure()
	time.Sleep(4100 * time.Millisecond) // backoff ~4s
	h.RecordFailure()

	// 4th attempt should be blocked (max reached)
	if h.ShouldAttempt() {
		t.Error("4th attempt should be blocked after 3 failures")
	}
}

func TestHealingBackoffTiming(t *testing.T) {
	h := NewHealer("/bin/true", 3)
	h.RecordFailure()

	// Should not allow immediately after failure (backoff)
	if h.ShouldAttempt() {
		t.Error("should not allow immediately after failure")
	}

	// Wait for backoff (2s after first failure)
	time.Sleep(2100 * time.Millisecond)

	// Should allow after backoff
	if !h.ShouldAttempt() {
		t.Error("should allow after backoff period")
	}
}

func TestHealingSuccessReset(t *testing.T) {
	h := NewHealer("/bin/true", 3)
	h.RecordFailure()
	h.RecordFailure()

	// Record success
	h.RecordSuccess()

	// Should reset failure count
	if !h.ShouldAttempt() {
		t.Error("should allow after success resets failure count")
	}
}

func TestHealingMaxBackoff(t *testing.T) {
	h := NewHealer("/bin/true", 3)

	// Multiple failures should cap backoff
	h.RecordFailure()
	if h.BackoffDuration() > 5*time.Second {
		t.Error("backoff should be capped at reasonable max")
	}
}
