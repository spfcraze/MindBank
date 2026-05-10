package models

import (
	"encoding/json"
	"time"
)

// Event type constants
const (
	EventSessionStart     = "session_start"
	EventUserPromptSubmit = "user_prompt_submit"
	EventPreToolUse       = "pre_tool_use"
	EventPostToolUse      = "post_tool_use"
	EventStop             = "stop"
)

// EventNode represents a lifecycle event within a session.
type EventNode struct {
	ID        string          `json:"id" db:"id"`
	SessionID string          `json:"session_id" db:"session_id"`
	EventType string          `json:"event_type" db:"event_type"`
	Sequence  int             `json:"sequence" db:"sequence"`
	Timestamp time.Time       `json:"timestamp" db:"timestamp"`
	Payload   json.RawMessage `json:"payload" db:"payload"`
}

// ValidEventTypes returns all valid event type strings.
func ValidEventTypes() []string {
	return []string{
		EventSessionStart,
		EventUserPromptSubmit,
		EventPreToolUse,
		EventPostToolUse,
		EventStop,
	}
}
