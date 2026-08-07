package models

import "time"

// SearchResult is a single result from hybrid search.
type SearchResult struct {
	NodeID    string  `json:"node_id"`
	Label     string  `json:"label"`
	NodeType  string  `json:"node_type"`
	Content   string  `json:"content"`
	Namespace string  `json:"namespace"`
	FTSScore  float32 `json:"fts_score,omitempty"`
	VecScore  float32 `json:"vec_score,omitempty"`
	RRFScore  float32 `json:"rrf_score"`
}

// NodeHistoryEntry is a single version in a node's temporal history.
type NodeHistoryEntry struct {
	ID            string     `json:"id"`
	Label         string     `json:"label"`
	Content       string     `json:"content"`
	Version       int        `json:"version"`
	ValidFrom     time.Time  `json:"valid_from"`
	ValidTo       *time.Time `json:"valid_to,omitempty"`
	PredecessorID *string    `json:"predecessor_id,omitempty"`
}

// AskResponse is the structured response from the Ask API.
type AskResponse struct {
	Context  string       `json:"context"`
	Nodes    []SearchResult `json:"nodes"`
	GraphPath string      `json:"graph_path,omitempty"`
	TokenCount int        `json:"token_count"`
}
