package handler

import (
	"log/slog"
	"net/http"
	"strconv"
)

func (h *AnalyzeHandler) LinkOrphans(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") != "false"
	ns := r.URL.Query().Get("namespace")
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	// Find orphan nodes
	nsFilter := ""
	args := []any{}
	if ns != "" {
		nsFilter = " AND n.namespace = $1"
		args = append(args, ns)
	}
	args = append(args, limit)

	orphanQuery := `
		SELECT n.id, n.label, n.namespace, n.node_type::text, COALESCE(n.content, '') AS content
		FROM nodes n
		WHERE n.valid_to IS NULL` + nsFilter + `
		AND NOT EXISTS (
			SELECT 1 FROM edges WHERE source_id = n.id OR target_id = n.id
		)
		ORDER BY n.created_at DESC
		LIMIT $` + strconv.Itoa(len(args))

	rows, err := h.pool.Query(r.Context(), orphanQuery, args...)
	if err != nil {
		respondError(w, 500, "orphan query failed")
		return
	}
	defer rows.Close()

	type orphanLink struct {
		OrphanID       string `json:"orphan_id"`
		OrphanLabel    string `json:"orphan_label"`
		OrphanNS       string `json:"orphan_namespace"`
		LinkedTo       string `json:"linked_to_id,omitempty"`
		LinkedToLabel  string `json:"linked_to_label,omitempty"`
		EdgeType       string `json:"edge_type"`
		Status         string `json:"status"`
	}

	var results []orphanLink
	linkedCount := 0

	for rows.Next() {
		var id, label, namespace, nodeType, content string
		if err := rows.Scan(&id, &label, &namespace, &nodeType, &content); err != nil {
			continue
		}

		ol := orphanLink{
			OrphanID:    id,
			OrphanLabel: label,
			OrphanNS:    namespace,
			EdgeType:    "relates_to",
			Status:      "no_match",
		}

		searchQuery := label
		if len(content) > 50 {
			searchQuery += " " + content[:50]
		}

		searchRows, searchErr := h.pool.Query(r.Context(), `
			SELECT id, label, node_type::text, similarity(label, $1) AS sim
			FROM nodes WHERE valid_to IS NULL AND id != $2
			ORDER BY similarity(label, $1) DESC LIMIT 1
		`, searchQuery, id)

		if searchErr == nil && searchRows.Next() {
			var matchID, matchLabel, matchType string
			var sim float32
			if err := searchRows.Scan(&matchID, &matchLabel, &matchType, &sim); err == nil && sim > 0.1 {
				ol.LinkedTo = matchID
				ol.LinkedToLabel = matchLabel
				if !dryRun {
					_, insertErr := h.pool.Exec(r.Context(), `
						INSERT INTO edges (source_id, target_id, edge_type, weight)
						VALUES ($1, $2, 'relates_to', 0.5) ON CONFLICT DO NOTHING
					`, id, matchID)
					if insertErr == nil {
						ol.Status = "linked"
						linkedCount++
					} else {
						ol.Status = "error"
					}
				} else {
					ol.Status = "would_link"
					linkedCount++
				}
			}
			searchRows.Close()
		}
		results = append(results, ol)
	}

	if results == nil {
		results = []orphanLink{}
	}

	respondJSON(w, 200, map[string]any{
		"orphans": results, "count": len(results),
		"linked": linkedCount, "dry_run": dryRun, "namespace": ns,
	})
}

// MergeDuplicates handles POST /api/v1/analyze/merge-duplicates
// Finds near-duplicate nodes and merges them (keeps newer, transfers edges).
func (h *AnalyzeHandler) MergeDuplicates(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") != "false"
	ns := r.URL.Query().Get("namespace")
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	nsFilter := ""
	args := []any{limit}
	if ns != "" {
		nsFilter = " AND a.namespace = $2"
		args = append(args, ns)
	}

	dupQuery := `
		SELECT a.label, a.node_type::text, a.namespace, COUNT(*) AS dup_count
		FROM nodes a
		WHERE a.valid_to IS NULL` + nsFilter + `
		GROUP BY a.namespace, a.label, a.node_type
		HAVING COUNT(*) > 1
		ORDER BY COUNT(*) DESC
		LIMIT $1`

	rows, err := h.pool.Query(r.Context(), dupQuery, args...)
	if err != nil {
		respondError(w, 500, "duplicate query failed")
		return
	}
	defer rows.Close()

	type mergeResult struct {
		Label      string   `json:"label"`
		Namespace  string   `json:"namespace"`
		NodeType   string   `json:"node_type"`
		KeepID     string   `json:"kept_id"`
		MergedIDs  []string `json:"merged_ids"`
		DupCount   int      `json:"duplicate_count"`
		EdgesMoved int      `json:"edges_moved"`
		Status     string   `json:"status"`
	}

	var results []mergeResult
	mergedCount := 0

	for rows.Next() {
		var label, nodeType, namespace string
		var dupCount int
		if err := rows.Scan(&label, &nodeType, &namespace, &dupCount); err != nil {
			continue
		}

		// Fetch the actual duplicate node IDs
		idRows, idErr := h.pool.Query(r.Context(), `
			SELECT id FROM nodes
			WHERE valid_to IS NULL AND namespace = $1
			AND label = $2 AND node_type::text = $3
			ORDER BY created_at DESC
		`, namespace, label, nodeType)

		if idErr != nil {
			continue
		}

		var ids []string
		for idRows.Next() {
			var id string
			if idRows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		idRows.Close()

		if len(ids) < 2 {
			continue
		}

		keepID := ids[0]
		toMerge := ids[1:]

		mr := mergeResult{
			Label: label, Namespace: namespace, NodeType: nodeType,
			KeepID: keepID, MergedIDs: toMerge, DupCount: dupCount, Status: "would_merge",
		}

		if !dryRun {
			// Wrap entire merge operation in a transaction for atomicity
			tx, txErr := h.pool.Begin(r.Context())
			if txErr != nil {
				slog.Error("merge: begin transaction", "label", label, "error", txErr)
				mr.Status = "error: " + txErr.Error()
				results = append(results, mr)
				continue
			}

			edgesMoved := 0
			txSuccess := true

			for _, oldID := range toMerge {
				// Transfer edges where old node is source
				tag, execErr := tx.Exec(r.Context(),
					`UPDATE edges SET source_id=$1 WHERE source_id=$2 AND target_id!=$1`,
					keepID, oldID)
				if execErr != nil {
					slog.Error("merge: transfer source edges", "old_id", oldID, "error", execErr)
					tx.Rollback(r.Context())
					txSuccess = false
					break
				}
				edgesMoved += int(tag.RowsAffected())

				// Transfer edges where old node is target
				tag, execErr = tx.Exec(r.Context(),
					`UPDATE edges SET target_id=$1 WHERE target_id=$2 AND source_id!=$1`,
					keepID, oldID)
				if execErr != nil {
					slog.Error("merge: transfer target edges", "old_id", oldID, "error", execErr)
					tx.Rollback(r.Context())
					txSuccess = false
					break
				}
				edgesMoved += int(tag.RowsAffected())

				// Soft-delete duplicate node
				_, execErr = tx.Exec(r.Context(),
					`UPDATE nodes SET valid_to=now() WHERE id=$1 AND valid_to IS NULL`,
					oldID)
				if execErr != nil {
					slog.Error("merge: soft-delete node", "old_id", oldID, "error", execErr)
					tx.Rollback(r.Context())
					txSuccess = false
					break
				}
			}

			if !txSuccess {
				mr.Status = "error: rolled back"
				results = append(results, mr)
				continue
			}

			// Commit transaction
			if commitErr := tx.Commit(r.Context()); commitErr != nil {
				slog.Error("merge: commit", "label", label, "error", commitErr)
				mr.Status = "error: " + commitErr.Error()
				results = append(results, mr)
				continue
			}

			mr.EdgesMoved = edgesMoved
			mr.Status = "merged"
			mergedCount++
		} else {
			mergedCount++
		}
		results = append(results, mr)
	}

	if results == nil {
		results = []mergeResult{}
	}

	respondJSON(w, 200, map[string]any{
		"merges": results, "count": len(results),
		"merged": mergedCount, "dry_run": dryRun, "namespace": ns,
	})
}
