package models

import (
	"encoding/json"
	"time"
)

// Workspace is the top-level container for mindmap data.
type Workspace struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
