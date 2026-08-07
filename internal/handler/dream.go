package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/dream"
)

// DreamHandler provides HTTP endpoints for the neural consolidation engine.
type DreamHandler struct {
	engine *dream.Engine
}

// NewDreamHandler creates a new dream handler.
func NewDreamHandler(pool *pgxpool.Pool) *DreamHandler {
	return &DreamHandler{engine: dream.NewEngine(pool)}
}

// Trigger handles POST /api/v1/dream/trigger
// Manually triggers a dream consolidation cycle.
func (h *DreamHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	result, err := h.engine.Cycle(r.Context())
	if err != nil {
		respondError(w, 500, "dream cycle failed: "+err.Error())
		return
	}

	respondJSON(w, 200, map[string]interface{}{
		"status":             "completed",
		"cycle_id":           result.CycleID,
		"edges_strengthened": result.NREM.Strengthened,
		"edges_pruned":       result.NREM.Pruned,
		"edges_weakened":     result.NREM.Weakened,
		"edges_bridged":      result.REM.Bridged,
		"isolated_checked":   result.REM.IsolatedChecked,
		"clusters_found":     result.Insight.ClustersFound,
		"cluster_nodes_created": result.Insight.ClusterNodesCreated,
	})
}

// Status handles GET /api/v1/dream/status
// Returns the status of the most recent dream cycle.
func (h *DreamHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.engine.GetCycleStatus(r.Context())
	if err != nil {
		respondError(w, 500, "get status failed: "+err.Error())
		return
	}
	respondJSON(w, 200, status)
}

// History handles GET /api/v1/dream/history
// Returns the history of dream cycles.
func (h *DreamHandler) History(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	history, err := h.engine.GetCycleHistory(r.Context(), limit)
	if err != nil {
		respondError(w, 500, "get history failed: "+err.Error())
		return
	}
	respondJSON(w, 200, map[string]interface{}{
		"cycles": history,
		"count":  len(history),
	})
}
