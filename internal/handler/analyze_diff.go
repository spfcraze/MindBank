package handler

import (
	"fmt"
	"net/http"
	"time"
)

func (h *AnalyzeHandler) Diff(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since == "" {
		since = r.URL.Query().Get("after")
	}
	if since == "" {
		// Default: last 24 hours
		since = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	}

	var sinceTime time.Time
	var err error

	// Parse "last-session" as 7 days ago (approximate)
	if since == "last-session" {
		sinceTime = time.Now().Add(-7 * 24 * time.Hour)
	} else {
		sinceTime, err = time.Parse(time.RFC3339, since)
		if err != nil {
			// Try just date
			sinceTime, err = time.Parse("2006-01-02", since)
			if err != nil {
				respondError(w, 400, "invalid 'since' parameter — use ISO 8601 (2026-04-18T00:00:00Z) or 'last-session'")
				return
			}
		}
	}

	ns := r.URL.Query().Get("namespace")

	// Build namespace filter and args dynamically to avoid hardcoded parameter positions
	buildQuery := func(baseSQL string, baseArgs []any) (string, []any) {
		if ns == "" {
			return baseSQL, baseArgs
		}
		pos := len(baseArgs) + 1
		return baseSQL + fmt.Sprintf(" AND namespace = $%d", pos), append(baseArgs, ns)
	}

	// New nodes
	newNodesQuery, newArgs := buildQuery(`
		SELECT id, label, namespace, node_type::text, created_at
		FROM nodes
		WHERE valid_to IS NULL AND created_at > $1
		ORDER BY created_at DESC
		LIMIT 50`, []any{sinceTime})

	type nodeRef struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Namespace string `json:"namespace"`
		NodeType  string `json:"node_type"`
		CreatedAt string `json:"created_at"`
	}

	var newNodes []nodeRef
	rows, err := h.pool.Query(r.Context(), newNodesQuery, newArgs...)
	if err == nil {
		for rows.Next() {
			var n nodeRef
			var ts time.Time
			if err := rows.Scan(&n.ID, &n.Label, &n.Namespace, &n.NodeType, &ts); err == nil {
				n.CreatedAt = ts.Format(time.RFC3339)
				newNodes = append(newNodes, n)
			}
		}
		rows.Close()
	}
	if newNodes == nil {
		newNodes = []nodeRef{}
	}

	// Updated nodes (newer versions superseded old ones)
	updateQuery, updateArgs := buildQuery(`
		SELECT id, label, namespace, node_type::text, updated_at, version
		FROM nodes
		WHERE valid_to IS NULL
		AND updated_at > created_at
		AND updated_at > $1
		ORDER BY updated_at DESC
		LIMIT 50`, []any{sinceTime})

	var updatedNodes []nodeRef
	rows, err = h.pool.Query(r.Context(), updateQuery, updateArgs...)
	if err == nil {
		for rows.Next() {
			var n nodeRef
			var ts time.Time
			if err := rows.Scan(&n.ID, &n.Label, &n.Namespace, &n.NodeType, &ts, new(int)); err == nil {
				n.CreatedAt = ts.Format(time.RFC3339)
				updatedNodes = append(updatedNodes, n)
			}
		}
		rows.Close()
	}
	if updatedNodes == nil {
		updatedNodes = []nodeRef{}
	}

	// New edges
	edgeQuery := `
		SELECT e.id, e.edge_type::text, e.source_id, e.target_id,
			s.label AS source_label, t.label AS target_label
		FROM edges e
		JOIN nodes s ON e.source_id = s.id AND s.valid_to IS NULL
		JOIN nodes t ON e.target_id = t.id AND t.valid_to IS NULL
		WHERE e.created_at > $1`
	edgeArgs := []any{sinceTime}
	if ns != "" {
		edgeQuery += " AND (s.namespace = $2 OR t.namespace = $2)"
		edgeArgs = append(edgeArgs, ns)
	}
	edgeQuery += " ORDER BY e.created_at DESC LIMIT 50"

	type edgeRef struct {
		ID          string `json:"id"`
		EdgeType    string `json:"edge_type"`
		SourceID    string `json:"source_id"`
		TargetID    string `json:"target_id"`
		SourceLabel string `json:"source_label"`
		TargetLabel string `json:"target_label"`
	}

	var newEdges []edgeRef
	rows, err = h.pool.Query(r.Context(), edgeQuery, edgeArgs...)
	if err == nil {
		for rows.Next() {
			var e edgeRef
			if err := rows.Scan(&e.ID, &e.EdgeType, &e.SourceID, &e.TargetID, &e.SourceLabel, &e.TargetLabel); err == nil {
				newEdges = append(newEdges, e)
			}
		}
		rows.Close()
	}
	if newEdges == nil {
		newEdges = []edgeRef{}
	}

	// Deleted nodes (valid_to set since sinceTime)
	deletedQuery, deletedArgs := buildQuery(`
		SELECT id, label, namespace, node_type::text, valid_to
		FROM nodes
		WHERE valid_to IS NOT NULL AND valid_to > $1
		ORDER BY valid_to DESC
		LIMIT 50`, []any{sinceTime})

	var deletedNodes []nodeRef
	rows, err = h.pool.Query(r.Context(), deletedQuery, deletedArgs...)
	if err == nil {
		for rows.Next() {
			var n nodeRef
			var ts time.Time
			if err := rows.Scan(&n.ID, &n.Label, &n.Namespace, &n.NodeType, &ts); err == nil {
				n.CreatedAt = ts.Format(time.RFC3339)
				deletedNodes = append(deletedNodes, n)
			}
		}
		rows.Close()
	}
	if deletedNodes == nil {
		deletedNodes = []nodeRef{}
	}

	respondJSON(w, 200, map[string]any{
		"since":    sinceTime.Format(time.RFC3339),
		"new_nodes":    newNodes,
		"updated_nodes": updatedNodes,
		"new_edges":     newEdges,
		"deleted_nodes": deletedNodes,
		"summary": map[string]int{
			"new_nodes":     len(newNodes),
			"updated_nodes": len(updatedNodes),
			"new_edges":     len(newEdges),
			"deleted_nodes": len(deletedNodes),
		},
	})
}

