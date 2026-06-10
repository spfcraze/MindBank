package mcp

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Healer manages self-healing for the MCP server with exponential backoff.
type Healer struct {
	mu            sync.RWMutex
	binaryPath    string
	maxAttempts   int
	attempts      int
	lastFailure   time.Time
	lastSuccess   time.Time
	disabled      bool
}

// NewHealer creates a healer for the given MCP binary.
func NewHealer(binaryPath string, maxAttempts int) *Healer {
	return &Healer{
		binaryPath:  binaryPath,
		maxAttempts: maxAttempts,
	}
}

// ShouldAttempt returns true if a restart attempt should be allowed.
func (h *Healer) ShouldAttempt() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.disabled {
		return false
	}

	if h.attempts >= h.maxAttempts {
		return false
	}

	// Exponential backoff: 1s, 2s, 4s, max 30s
	if !h.lastFailure.IsZero() {
		backoff := h.backoffDuration()
		if time.Since(h.lastFailure) < backoff {
			return false
		}
	}

	return true
}

// RecordFailure increments failure count and sets last failure time.
func (h *Healer) RecordFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attempts++
	h.lastFailure = time.Now()
	slog.Warn("mcp heal failure", "attempt", h.attempts, "max", h.maxAttempts)
}

// RecordSuccess resets failure count.
func (h *Healer) RecordSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attempts = 0
	h.lastSuccess = time.Now()
	h.lastFailure = time.Time{}
}

// backoffDuration returns the current backoff duration.
func (h *Healer) backoffDuration() time.Duration {
	// Exponential: 1s, 2s, 4s, 8s, 16s, max 30s
	backoff := time.Duration(1 << uint(h.attempts)) * time.Second
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}
	return backoff
}

// BackoffDuration returns the current backoff (exported for testing).
func (h *Healer) BackoffDuration() time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.backoffDuration()
}

// AttemptRestart tries to restart the MCP server.
func (h *Healer) AttemptRestart(env []string) error {
	if !h.ShouldAttempt() {
		return fmt.Errorf("healing disabled: max attempts (%d) reached", h.maxAttempts)
	}

	// Check binary exists
	if _, err := os.Stat(h.binaryPath); err != nil {
		h.RecordFailure()
		return fmt.Errorf("binary not found: %w", err)
	}

	// Kill existing processes
	exec.Command("pkill", "-f", "mindbank-mcp").Run()
	time.Sleep(500 * time.Millisecond)

	// Start new process
	cmd := exec.Command(h.binaryPath)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		h.RecordFailure()
		return fmt.Errorf("failed to start: %w", err)
	}

	slog.Info("mcp healer restarted server", "pid", cmd.Process.Pid)
	return nil
}

// Disable permanently disables healing.
func (h *Healer) Disable() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disabled = true
}

// IsDisabled returns true if healing is disabled.
func (h *Healer) IsDisabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.disabled
}
