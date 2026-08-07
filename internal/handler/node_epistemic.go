package handler

import (
	"net/http"
)

// validEpistemicLabels are the allowed epistemic label values.
var validEpistemicLabels = map[string]bool{
	"observed":    true,
	"inferred":    true,
	"assumed":     true,
	"recommended": true,
	"unknown":     true,
}

// UpdateEpistemicLabel handles PUT /api/v1/nodes/epistemic?label=X&node_id=Y
// It updates the epistemic_label column for a given node.
func (h *NodeHandler) UpdateEpistemicLabel(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")
	nodeID := r.URL.Query().Get("node_id")

	if nodeID == "" {
		respondError(w, 400, "node_id is required")
		return
	}
	if label == "" {
		respondError(w, 400, "label is required")
		return
	}
	if !validEpistemicLabels[label] {
		respondError(w, 400, "invalid label: must be one of observed, inferred, assumed, recommended, unknown")
		return
	}

	// Update the nodes table directly (non-temporal, single field update)
	res, err := h.pool.Exec(r.Context(),
		`UPDATE nodes SET epistemic_label = $1 WHERE id = $2 AND valid_to IS NULL`,
		label, nodeID)
	if err != nil {
		respondError(w, 500, "failed to update epistemic label")
		return
	}

	// Check if any row was actually updated
	if res.RowsAffected() == 0 {
		respondError(w, 404, "node not found")
		return
	}

	respondJSON(w, 200, map[string]string{
		"node_id":         nodeID,
		"epistemic_label": label,
	})
}
