package handler

import (
	"context"
	"log/slog"
	"net/http"

	"mindbank/internal/models"
	"mindbank/internal/taxonomy"
)

// TaxonomyHandler provides auto-classification endpoints.
type TaxonomyHandler struct {
	nodeRepo NodeRepoInterface
}

// NodeRepoInterface is the subset of repository.NodeRepo we need.
type NodeRepoInterface interface {
	List(ctx context.Context, workspace, namespace string, nodeType models.NodeType, skill string, sortField, sortOrder string, limit, offset int) ([]models.Node, error)
	UpdateTopic(ctx context.Context, id string, topic string) error
}

// NewTaxonomyHandler creates a new taxonomy handler.
func NewTaxonomyHandler(nodeRepo NodeRepoInterface) *TaxonomyHandler {
	return &TaxonomyHandler{nodeRepo: nodeRepo}
}

// ClassifyAll handles POST /api/v1/taxonomy/classify-all
// Classifies all nodes and returns the distribution.
func (h *TaxonomyHandler) ClassifyAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fetch all nodes
	nodes, err := h.nodeRepo.List(ctx, "", "", "", "", "", "", 10000, 0)
	if err != nil {
		respondError(w, 500, "fetch nodes: "+err.Error())
		return
	}

	// Classify each node and persist topics
	dist := make(map[string]int)
	samples := make(map[string][]string)
	for _, n := range nodes {
		topic := taxonomy.ClassifyNode(&n)
		if topic == "" {
			topic = "uncategorized"
		}
		dist[topic]++

		// Persist topic to database
		if n.Topic != topic {
			if err := h.nodeRepo.UpdateTopic(ctx, n.ID, topic); err != nil {
				slog.Warn("failed to update topic", "node_id", n.ID, "topic", topic, "error", err)
			}
		}

		if len(samples[topic]) < 3 {
			samples[topic] = append(samples[topic], n.Label)
		}
	}

	respondJSON(w, 200, map[string]interface{}{
		"total_nodes":   len(nodes),
		"distribution":  dist,
		"samples":       samples,
	})
}

// GetTopicDistribution handles GET /api/v1/taxonomy/distribution
func (h *TaxonomyHandler) GetTopicDistribution(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodes, err := h.nodeRepo.List(ctx, "", "", "", "", "", "", 10000, 0)
	if err != nil {
		respondError(w, 500, "fetch nodes: "+err.Error())
		return
	}

	// Use stored topics if available, otherwise classify on-the-fly
	dist := make(map[string]int)
	for _, n := range nodes {
		topic := n.Topic
		if topic == "" {
			topic = taxonomy.ClassifyNode(&n)
		}
		if topic == "" {
			topic = "uncategorized"
		}
		dist[topic]++
	}

	respondJSON(w, 200, map[string]interface{}{
		"total":        len(nodes),
		"distribution": dist,
	})
}

// SuggestEdges handles GET /api/v1/taxonomy/suggest-edges
// Suggests potential edges based on topic similarity.
func (h *TaxonomyHandler) SuggestEdges(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fetch nodes and edges
	nodes, err := h.nodeRepo.List(ctx, "", "", "", "", "", "", 10000, 0)
	if err != nil {
		respondError(w, 500, "fetch nodes: "+err.Error())
		return
	}

	// For now, return empty edges (we don't have edge repo access here)
	// In production, inject edge repo
	suggestions := taxonomy.SuggestConnections(nodes, nil)

	// Limit to top 50
	if len(suggestions) > 50 {
		suggestions = suggestions[:50]
	}

	respondJSON(w, 200, map[string]interface{}{
		"suggestions": suggestions,
		"total":       len(suggestions),
	})
}
