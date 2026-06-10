package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// vectorToString formats a float32 slice as a pgvector literal: "[0.1,0.2,...]"
func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, f := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%g", f)
	}
	s += "]"
	return s
}

// RefineConnectivity handles POST /api/v1/analyze/refine-connectivity
// Feedback-driven topological editing: expand missing links, prune noise.
func (h *AnalyzeHandler) RefineConnectivity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID   string `json:"node_id"`
		Feedback string `json:"feedback"` // "missing_context", "too_much_noise", "granularity_mismatch"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.NodeID == "" {
		respondError(w, 400, "node_id required")
		return
	}
	feedback := strings.ToLower(strings.TrimSpace(req.Feedback))
	if feedback != "missing_context" && feedback != "too_much_noise" && feedback != "granularity_mismatch" {
		respondError(w, 400, "feedback must be 'missing_context', 'too_much_noise', or 'granularity_mismatch'")
		return
	}

	ctx := r.Context()

	switch feedback {
	case "missing_context":
		h.refineMissingContext(ctx, w, req.NodeID)
	case "too_much_noise":
		h.refineTooMuchNoise(ctx, w, req.NodeID)
	case "granularity_mismatch":
		h.refineGranularityMismatch(ctx, w, req.NodeID)
	}
}

// refineMissingContext finds semantically proximate unconnected nodes and creates relates_to edges.
func (h *AnalyzeHandler) refineMissingContext(ctx context.Context, w http.ResponseWriter, nodeID string) {
	// Get the source node's embedding and namespace
	var sourceEmbeddingStr string
	var sourceNS string
	err := h.pool.QueryRow(ctx, `
		SELECT ne.embedding::text, n.namespace
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.id = $1 AND n.valid_to IS NULL
	`, nodeID).Scan(&sourceEmbeddingStr, &sourceNS)
	if err != nil {
		respondError(w, 404, "node not found or has no embedding")
		return
	}

	// Find top-5 unconnected nodes by cosine similarity using pgvector <=> operator
	// <=> is cosine distance (1 - cosine_similarity), so ORDER BY <=> ASC
	rows, err := h.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, n.namespace,
			ne.embedding <=> $1::vector AS distance
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.valid_to IS NULL
		  AND n.id != $2
		  AND n.namespace = $3
		  AND NOT EXISTS (
			SELECT 1 FROM edges
			WHERE (source_id = $2 AND target_id = n.id)
			   OR (source_id = n.id AND target_id = $2)
		  )
		ORDER BY distance ASC
		LIMIT 5
	`, sourceEmbeddingStr, nodeID, sourceNS)
	if err != nil {
		log.Printf("[refine] similarity query failed: %v", err)
		respondError(w, 500, "similarity search failed")
		return
	}
	defer rows.Close()

	type suggestion struct {
		NodeID    string  `json:"node_id"`
		Label     string  `json:"label"`
		NodeType  string  `json:"node_type"`
		Namespace string  `json:"namespace"`
		Distance  float64 `json:"distance"`
		Created   bool    `json:"created"`
	}

	var suggestions []suggestion
	var createdEdges []suggestion

	for rows.Next() {
		var s suggestion
		if err := rows.Scan(&s.NodeID, &s.Label, &s.NodeType, &s.Namespace, &s.Distance); err != nil {
			continue
		}
		suggestions = append(suggestions, s)

		// Create edge if similarity is strong enough (distance < 0.4, i.e., similarity > 0.6)
		if s.Distance < 0.4 {
			_, err := h.pool.Exec(ctx, `
				INSERT INTO edges (source_id, target_id, edge_type, weight, metadata)
				VALUES ($1, $2, 'relates_to', $3, '{"auto_connected":true,"reason":"embedding_similarity"}')
				ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
			`, nodeID, s.NodeID, 1.0-s.Distance)
			if err != nil {
				log.Printf("[refine] edge insert failed: %v", err)
			} else {
				s.Created = true
				createdEdges = append(createdEdges, s)
			}
		}
	}

	respondJSON(w, 200, map[string]any{
		"node_id":        nodeID,
		"feedback":       "missing_context",
		"suggestions":    suggestions,
		"created_edges":  createdEdges,
		"created_count":  len(createdEdges),
	})
}

// refineTooMuchNoise prunes low-weight edges connected to the node.
func (h *AnalyzeHandler) refineTooMuchNoise(ctx context.Context, w http.ResponseWriter, nodeID string) {
	// Find edges with weight < 0.3
	rows, err := h.pool.Query(ctx, `
		SELECT id, source_id, target_id, edge_type::text, weight
		FROM edges
		WHERE (source_id = $1 OR target_id = $1)
		  AND weight < 0.3
		ORDER BY weight ASC
		LIMIT 20
	`, nodeID)
	if err != nil {
		log.Printf("[refine] prune query failed: %v", err)
		respondError(w, 500, "prune query failed")
		return
	}
	defer rows.Close()

	type prunedEdge struct {
		EdgeID    string  `json:"edge_id"`
		SourceID  string  `json:"source_id"`
		TargetID  string  `json:"target_id"`
		EdgeType  string  `json:"edge_type"`
		Weight    float64 `json:"weight"`
	}

	var pruned []prunedEdge
	for rows.Next() {
		var e prunedEdge
		if err := rows.Scan(&e.EdgeID, &e.SourceID, &e.TargetID, &e.EdgeType, &e.Weight); err != nil {
			continue
		}
		// Soft-delete by marking weight as negative (or actually delete since edges don't version)
		_, delErr := h.pool.Exec(ctx, `DELETE FROM edges WHERE id = $1`, e.EdgeID)
		if delErr == nil {
			pruned = append(pruned, e)
		}
	}

	respondJSON(w, 200, map[string]any{
		"node_id":       nodeID,
		"feedback":      "too_much_noise",
		"pruned_edges":  pruned,
		"pruned_count":  len(pruned),
	})
}

// refineGranularityMismatch flags the node for content reshaping review.
func (h *AnalyzeHandler) refineGranularityMismatch(ctx context.Context, w http.ResponseWriter, nodeID string) {
	// Get node info
	var label, nodeType, content string
	var importance float32
	err := h.pool.QueryRow(ctx, `
		SELECT label, node_type::text, content, importance
		FROM nodes
		WHERE id = $1 AND valid_to IS NULL
	`, nodeID).Scan(&label, &nodeType, &content, &importance)
	if err != nil {
		respondError(w, 404, "node not found")
		return
	}

	// Simple heuristic: if content is very short, suggest expansion; if very long, suggest abstraction
	suggestion := "review content granularity"
	contentLen := len(strings.TrimSpace(content))
	if contentLen < 50 {
		suggestion = "content is very short (" + itoa(contentLen) + " chars) — consider expanding with execution details"
	} else if contentLen > 2000 {
		suggestion = "content is very long (" + itoa(contentLen) + " chars) — consider abstracting into high-level summary"
	}

	respondJSON(w, 200, map[string]any{
		"node_id":    nodeID,
		"feedback":   "granularity_mismatch",
		"label":      label,
		"node_type":  nodeType,
		"content_length": contentLen,
		"suggestion": suggestion,
	})
}