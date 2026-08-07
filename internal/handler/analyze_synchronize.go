package handler

import (
	"encoding/json"
	"net/http"

	"mindbank/internal/repository"
)

func (h *AnalyzeHandler) Synchronize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID                string `json:"node_id"`
		Namespace             string `json:"namespace"`
		PropagateDepth        int    `json:"propagate_depth"`
		ResolveContradictions bool   `json:"resolve_contradictions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.NodeID == "" {
		respondError(w, 400, "node_id required")
		return
	}
	if req.PropagateDepth <= 0 || req.PropagateDepth > 5 {
		req.PropagateDepth = 3
	}

	ctx := r.Context()
	repo := repository.NewDependenceRepo(h.pool)

	affected, confUpdates, resolved, nodes, edges, err := repo.SynchronizeNode(ctx, req.NodeID, req.PropagateDepth, req.ResolveContradictions)
	if err != nil {
		respondError(w, 500, "synchronization failed")
		return
	}

	respondJSON(w, 200, map[string]any{
		"node_id":                 req.NodeID,
		"affected_nodes":          affected,
		"confidence_updates":      confUpdates,
		"resolved_contradictions": resolved,
		"propagation_graph": map[string]any{
			"nodes": nodes,
			"edges": edges,
		},
	})
}
