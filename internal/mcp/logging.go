package mcp

import (
	"log/slog"
	"time"
)

// RequestLog records MCP request metrics for observability.
type RequestLog struct {
	Method   string        `json:"method"`
	Tool     string        `json:"tool,omitempty"`
	Duration time.Duration `json:"duration_ms"`
	Success  bool          `json:"success"`
}

// logRequest logs MCP request timing and outcome.
func (s *Server) logRequest(method string, tool string, duration time.Duration, success bool) {
	slog.Info("mcp request",
		"method", method,
		"tool", tool,
		"duration_ms", duration.Milliseconds(),
		"success", success,
	)
}
