package models

import (
	"encoding/json"
	"time"
)

// EdgeType represents the kind of relationship between nodes.
type EdgeType string

const (
	EdgeContains        EdgeType = "contains"
	EdgeRelatesTo       EdgeType = "relates_to"
	EdgeDependsOn       EdgeType = "depends_on"
	EdgeDecidedBy       EdgeType = "decided_by"
	EdgeParticipatedIn  EdgeType = "participated_in"
	EdgeProduced        EdgeType = "produced"
	EdgeContradicts     EdgeType = "contradicts"
	EdgeSupports        EdgeType = "supports"
	EdgeTemporalNext    EdgeType = "temporal_next"
	EdgeMentions        EdgeType = "mentions"
	EdgeLearnedFrom     EdgeType = "learned_from"
	// Epistemic / empirical validation edges
	EdgeTestedBy        EdgeType = "tested_by"
	EdgeInvalidatedBy   EdgeType = "invalidated_by"
	EdgeDerivedFrom     EdgeType = "derived_from"
	EdgeAssumed         EdgeType = "assumed"
	// Temporal evolution edges
	EdgeSupersededBy    EdgeType = "superseded_by"
	EdgeRefinedBy       EdgeType = "refined_by"
	EdgeMergedInto      EdgeType = "merged_into"
	// Action / agent edges
	EdgeCreatedBy       EdgeType = "created_by"
	EdgeReviewedBy      EdgeType = "reviewed_by"
	EdgeExecutedBy      EdgeType = "executed_by"
	// Contrast / negation edges
	EdgeFailedDueTo     EdgeType = "failed_due_to"
	EdgeIncompatibleWith EdgeType = "incompatible_with"
	EdgePreconditionFor EdgeType = "precondition_for"
)

// IsValid returns true if the edge type is a known enum value.
func (et EdgeType) IsValid() bool {
	switch et {
	case EdgeContains, EdgeRelatesTo, EdgeDependsOn, EdgeDecidedBy,
		EdgeParticipatedIn, EdgeProduced, EdgeContradicts, EdgeSupports,
		EdgeTemporalNext, EdgeMentions, EdgeLearnedFrom,
		EdgeTestedBy, EdgeInvalidatedBy, EdgeDerivedFrom, EdgeAssumed,
		EdgeSupersededBy, EdgeRefinedBy, EdgeMergedInto,
		EdgeCreatedBy, EdgeReviewedBy, EdgeExecutedBy,
		EdgeFailedDueTo, EdgeIncompatibleWith, EdgePreconditionFor:
		return true
	}
	return false
}

// Edge is a directed connection between two nodes.
type Edge struct {
	ID            string          `json:"id"`
	WorkspaceName string          `json:"workspace_name"`
	SourceID      string          `json:"source_id"`
	TargetID      string          `json:"target_id"`
	EdgeType      EdgeType        `json:"edge_type"`
	Weight        float32         `json:"weight"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

// EdgeCreate is the request body for creating an edge.
type EdgeCreate struct {
	WorkspaceName string          `json:"workspace_name"`
	SourceID      string          `json:"source_id"`
	TargetID      string          `json:"target_id"`
	EdgeType      EdgeType        `json:"edge_type"`
	Weight        *float32        `json:"weight,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}
