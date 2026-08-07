package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/dream"
)

// ConflictHandler provides HTTP endpoints for conflict detection.
type ConflictHandler struct {
	detector *dream.ConflictDetector
}

// NewConflictHandler creates a conflict handler.
func NewConflictHandler(pool *pgxpool.Pool) *ConflictHandler {
	return &ConflictHandler{
		detector: dream.NewConflictDetector(pool),
	}
}

// Detect handles POST /conflicts/detect - runs conflict detection.
func (h *ConflictHandler) Detect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	conflicts, err := h.detector.DetectConflicts(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "conflict detection failed: "+err.Error())
		return
	}

	// Save detected conflicts
	for i := range conflicts {
		if err := h.detector.SaveConflict(ctx, &conflicts[i]); err != nil {
			// Log but continue
			continue
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"conflicts_found": len(conflicts),
		"conflicts":       conflicts,
		"status":          "completed",
	})
}

// List handles GET /conflicts - returns unresolved conflicts.
func (h *ConflictHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	conflicts, err := h.detector.GetOpenConflicts(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list conflicts: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"count":     len(conflicts),
		"conflicts": conflicts,
	})
}

// Resolve handles POST /conflicts/{id}/resolve - resolves a conflict.
func (h *ConflictHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse conflict ID from URL
	conflictIDStr := r.URL.Path[len("/api/v1/conflicts/"):]
	conflictIDStr = conflictIDStr[:len(conflictIDStr)-len("/resolve")]
	conflictID, err := strconv.ParseInt(conflictIDStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid conflict ID")
		return
	}

	var req struct {
		Resolution string `json:"resolution"`
		WinnerID   string `json:"winner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.detector.ResolveConflict(ctx, conflictID, req.Resolution, req.WinnerID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve conflict: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"conflict_id": conflictID,
		"resolution":  req.Resolution,
		"winner_id":   req.WinnerID,
		"status":      "resolved",
	})
}

// Supersede handles POST /conflicts/supersede - supersede a node.
func (h *ConflictHandler) Supersede(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		OldNodeID string `json:"old_node_id"`
		NewNodeID string `json:"new_node_id"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.detector.SupersedeNode(ctx, req.OldNodeID, req.NewNodeID, req.Reason); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to supersede: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"old_node_id": req.OldNodeID,
		"new_node_id": req.NewNodeID,
		"reason":      req.Reason,
		"status":      "superseded",
	})
}
