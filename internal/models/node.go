package models

import (
	"encoding/json"
	"time"
)

// NodeType represents the kind of mindmap node.
type NodeType string

const (
	NodePerson     NodeType = "person"
	NodeAgent      NodeType = "agent"
	NodeProject    NodeType = "project"
	NodeTopic      NodeType = "topic"
	NodeDecision   NodeType = "decision"
	NodeFact       NodeType = "fact"
	NodeEvent      NodeType = "event"
	NodePreference NodeType = "preference"
	NodeAdvice     NodeType = "advice"
	NodeProblem    NodeType = "problem"
	NodeConcept    NodeType = "concept"
	NodeQuestion   NodeType = "question"
	NodeSession    NodeType = "session"
)

// IsValid checks if the NodeType is a valid enum value.
func (nt NodeType) IsValid() bool {
	switch nt {
	case NodePerson, NodeAgent, NodeProject, NodeTopic, NodeDecision,
		NodeFact, NodeEvent, NodePreference, NodeAdvice, NodeProblem,
		NodeConcept, NodeQuestion, NodeSession:
		return true
	default:
		return false
	}
}

// AllNodeTypes returns all valid node types.
func AllNodeTypes() []NodeType {
	return []NodeType{
		NodePerson, NodeAgent, NodeProject, NodeTopic, NodeDecision,
		NodeFact, NodeEvent, NodePreference, NodeAdvice, NodeProblem,
		NodeConcept, NodeQuestion, NodeSession,
	}
}

// ValidationStatus tracks the empirical lifecycle of a memory node.
type ValidationStatus string

const (
	StatusOpen         ValidationStatus = "open"
	StatusSupported    ValidationStatus = "supported"
	StatusRefuted      ValidationStatus = "refuted"
	StatusInconclusive ValidationStatus = "inconclusive"
	StatusBlocked      ValidationStatus = "blocked"
)

// IsValid checks if the ValidationStatus is a valid enum value.
func (vs ValidationStatus) IsValid() bool {
	switch vs {
	case StatusOpen, StatusSupported, StatusRefuted, StatusInconclusive, StatusBlocked:
		return true
	default:
		return false
	}
}

// Node is a vertex in the mindmap graph with temporal validity.
type Node struct {
	ID                 string           `json:"id"`
	WorkspaceName      string           `json:"workspace_name"`
	Namespace          string           `json:"namespace"`
	Label              string           `json:"label"`
	EpistemicLabel     string           `json:"epistemic_label,omitempty" db:"epistemic_label"`
	Status             ValidationStatus `json:"status,omitempty"`
	ConfirmationCount  int              `json:"confirmation_count" db:"confirmation_count"`
	Source             string           `json:"source,omitempty"`
	Blocker            string           `json:"blocker,omitempty"`
	NodeType           NodeType         `json:"node_type"`
	Content            string           `json:"content"`
	Summary            string           `json:"summary"`
	Topic              string           `json:"topic,omitempty" db:"topic"`
	Metadata           json.RawMessage  `json:"metadata"`
	Importance         float32          `json:"importance"`
	AccessCount        int              `json:"access_count"`
	LastAccessed       *time.Time       `json:"last_accessed,omitempty"`
	ValidFrom          time.Time        `json:"valid_from"`
	ValidTo            *time.Time       `json:"valid_to,omitempty"`
	Version            int              `json:"version"`
	PredecessorID      *string          `json:"predecessor_id,omitempty"`
	Deduplicated       bool             `json:"deduplicated,omitempty"`
	ExpiresAt          *time.Time       `json:"expires_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

// NodeCreate is the request body for creating a node.
type NodeCreate struct {
	WorkspaceName    string           `json:"workspace_name"`
	Namespace        string           `json:"namespace,omitempty"`
	Label            string           `json:"label"`
	NodeType         NodeType         `json:"node_type"`
	Content          string           `json:"content,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Metadata         json.RawMessage  `json:"metadata,omitempty"`
	Importance       *float32         `json:"importance,omitempty"`
	ExpiresAt        *time.Time       `json:"expires_at,omitempty"`
	EpistemicLabel   string           `json:"epistemic_label,omitempty"`
	Status           ValidationStatus `json:"status,omitempty"`
	Source           string           `json:"source,omitempty"`
	Blocker          string           `json:"blocker,omitempty"`
}

// NodeUpdate is the request body for updating a node.
type NodeUpdate struct {
	Label             *string          `json:"label,omitempty"`
	Content           *string          `json:"content,omitempty"`
	Summary           *string          `json:"summary,omitempty"`
	Metadata          json.RawMessage  `json:"metadata,omitempty"`
	Importance        *float32         `json:"importance,omitempty"`
	ExpiresAt         *time.Time       `json:"expires_at,omitempty"`
	EpistemicLabel    *string          `json:"epistemic_label,omitempty"`
	Status            *ValidationStatus `json:"status,omitempty"`
	Source            *string           `json:"source,omitempty"`
	Blocker           *string           `json:"blocker,omitempty"`
}

// NodeWithEdge is a node returned from graph traversal, including edge info.
type NodeWithEdge struct {
	Node
	EdgeType   string  `json:"edge_type"`
	EdgeWeight float32 `json:"edge_weight"`
	Salience   float32 `json:"salience"`
	Depth      int     `json:"depth"`
	Direction  string  `json:"direction,omitempty"` // "incoming" or "outgoing" relative to the anchor node
}

// IsCurrent returns true if the node is currently valid.
func (n *Node) IsCurrent() bool {
	return n.ValidTo == nil
}

// NodeTypeWeights maps node types to importance weights for scoring.
var NodeTypeWeights = map[NodeType]float32{
	NodeDecision:   1.0,
	NodePreference: 0.9,
	NodeProblem:    0.9,
	NodeAdvice:     0.8,
	NodeFact:       0.7,
	NodePerson:     0.7,
	NodeProject:    0.7,
	NodeEvent:      0.5,
	NodeTopic:      0.4,
	NodeConcept:    0.3,
	NodeAgent:      0.7,
}
