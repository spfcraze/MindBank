package handler

import (
	"net/http"
	"strings"

	"mindbank/internal/repository"
)

func (h *AnalyzeHandler) Observability(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	seedParam := r.URL.Query().Get("seed_node_ids")

	var seedIDs []string
	if seedParam != "" {
		seedIDs = strings.Split(seedParam, ",")
	}

	ctx := r.Context()
	repo := repository.NewDependenceRepo(h.pool)

	observable, total, ratio, coverageByType, err := repo.GetObservability(ctx, ns, seedIDs)
	if err != nil {
		respondError(w, 500, "observability computation failed")
		return
	}

	respondJSON(w, 200, map[string]any{
		"namespace":           ns,
		"seed_count":          len(seedIDs),
		"total_nodes":         total,
		"observable_nodes":    observable,
		"observability_ratio": ratio,
		"coverage_by_type":    coverageByType,
	})
}
