package models

import (
	"encoding/json"
	"time"
)

// Message is a single utterance in a session.
type Message struct {
	ID            int64           `json:"id"`
	SessionID     string          `json:"session_id"`
	Role          string          `json:"role"`
	Content       string          `json:"content"`
	SeqInSession  int             `json:"seq_in_session"`
	TokenCount    int             `json:"token_count"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

// MessageCreate is the request body for creating a message.
type MessageCreate struct {
	Role     string          `json:"role"`
	Content  string          `json:"content"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}
