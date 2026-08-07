package models

import (
	"time"
)

// ProfileCategory is the type of user profile fact.
type ProfileCategory string

const (
	ProfilePreference ProfileCategory = "preference"
	ProfileFact       ProfileCategory = "fact"
	ProfileGoal       ProfileCategory = "goal"
	ProfileProject    ProfileCategory = "project"
	ProfileSkill      ProfileCategory = "skill"
	ProfileConstraint ProfileCategory = "constraint"
)

// Profile is a structured user fact extracted from nodes.
type Profile struct {
	ID           string          `json:"id"`
	Category     ProfileCategory `json:"category"`
	Fact         string          `json:"fact"`
	Confidence   float32         `json:"confidence"`
	SourceNodeID *string         `json:"source_node_id,omitempty"`
	ValidFrom    time.Time       `json:"valid_from"`
	ValidTo      *time.Time      `json:"valid_to,omitempty"`
	Metadata     []byte          `json:"metadata,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// ProfileCreate is the request body for creating a profile.
type ProfileCreate struct {
	Category     ProfileCategory `json:"category"`
	Fact         string          `json:"fact"`
	Confidence   float32         `json:"confidence,omitempty"`
	SourceNodeID *string         `json:"source_node_id,omitempty"`
	Metadata     []byte          `json:"metadata,omitempty"`
}

// ProfileUpdate is the request body for updating a profile.
type ProfileUpdate struct {
	Fact       *string  `json:"fact,omitempty"`
	Confidence *float32 `json:"confidence,omitempty"`
	Metadata   []byte   `json:"metadata,omitempty"`
}
