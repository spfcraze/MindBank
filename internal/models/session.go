package models

import (
	"encoding/json"
	"time"
)

// Session tracks a conversation that produces memories.
type Session struct {
	ID            string          `json:"id"`
	WorkspaceName string          `json:"workspace_name"`
	Name          string          `json:"name"`
	IsActive      bool            `json:"is_active"`
	Metadata      json.RawMessage `json:"metadata"`
	Summary       string          `json:"summary"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// SessionCreate is the request body for creating a session.
type SessionCreate struct {
	WorkspaceName string          `json:"workspace_name"`
	Name          string          `json:"name,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

// SessionNode tracks which nodes were mentioned in a session.
type SessionNode struct {
	SessionID     string    `json:"session_id"`
	NodeID        string    `json:"node_id"`
	MentionCount  int       `json:"mention_count"`
	FirstMentioned time.Time `json:"first_mentioned"`
	LastMentioned  time.Time `json:"last_mentioned"`
}
