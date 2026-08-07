package handler

import (
	"encoding/json"
	"net/http"

	"mindbank/internal/repository"
)

func (h *AnalyzeHandler) Dependence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID    string   `json:"node_id"`
		Query     string   `json:"query"`
		Namespace string   `json:"namespace"`
		MaxDepth  int      `json:"max_depth"`
		EdgeTypes []string `json:"edge_types"`
		MinWeight float32  `json:"min_weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.MaxDepth <= 0 || req.MaxDepth > 5 {
		req.MaxDepth = 3
	}
	if req.MinWeight <= 0 {
		req.MinWeight = 0.1
	}

	ctx := r.Context()
	repo := repository.NewDependenceRepo(h.pool)

	seedID := req.NodeID
	if seedID == "" && req.Query != "" {
		// Run text search to find seed
		searchRepo := repository.NewSearchRepo(h.pool)
		results, err := searchRepo.FullTextSearch(ctx, req.Query, "", req.Namespace, 1)
		if err != nil || len(results) == 0 {
			respondError(w, 404, "no seed found for query")
			return
		}
		seedID = results[0].NodeID
	}
	if seedID == "" {
		respondError(w, 400, "node_id or query required")
		return
	}

	// Verify seed exists
	var seedLabel, seedType, seedNS string
	row := h.pool.QueryRow(ctx, `SELECT label, node_type::text, namespace FROM nodes WHERE id = $1 AND valid_to IS NULL`, seedID)
	if err := row.Scan(&seedLabel, &seedType, &seedNS); err != nil {
		respondError(w, 404, "seed node not found")
		return
	}

	nodes, edges, influenceModes, criticalDepth, coverage, blindSpots, err := repo.GetDependenceGraph(ctx, seedID, req.EdgeTypes, req.MaxDepth, req.MinWeight)
	if err != nil {
		respondError(w, 500, "dependence computation failed")
		return
	}

	respondJSON(w, 200, map[string]any{
		"seed": map[string]string{
			"id":        seedID,
			"label":     seedLabel,
			"node_type": seedType,
			"namespace": seedNS,
		},
		"dependence_graph": map[string]any{
			"nodes": nodes,
			"edges": edges,
		},
		"critical_depth":  criticalDepth,
		"coverage":        coverage,
		"influence_modes": influenceModes,
		"blind_spots":     blindSpots,
		"max_depth_used":  req.MaxDepth,
		"min_weight_used": req.MinWeight,
	})
}
