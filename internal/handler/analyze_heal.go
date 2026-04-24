package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
						INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight)
						VALUES ('hermes', $1, $2, 'relates_to', 0.5) ON CONFLICT DO NOTHING
					`, id, matchID)
					if insertErr == nil {
						ol.Status = "linked"
						linkedCount++
					} else {
						ol.Status = "error"
						slog.Error("link-orphans insert failed", "orphan_id", id, "target_id", matchID, "error", insertErr)
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

// ConnectComponents handles POST /api/v1/analyze/connect-components
// Finds nodes in small disconnected components and links them to the main component.
func (h *AnalyzeHandler) ConnectComponents(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	dryRun := r.URL.Query().Get("dry_run") != "false"
	ctx := r.Context()

	// Fetch all current nodes and edges for the namespace
	nodeRows, err := h.pool.Query(ctx, `
		SELECT id, label, namespace, node_type::text, COALESCE(content, '') AS content
		FROM nodes WHERE valid_to IS NULL
		AND ($1 = '' OR namespace = $1)
	`, ns)
	if err != nil {
		respondError(w, 500, "node query failed")
		return
	}
	defer nodeRows.Close()

	type nodeInfo struct {
		ID        string
		Label     string
		Namespace string
		NodeType  string
		Content   string
	}

	nodes := make(map[string]nodeInfo)
	nodeIDs := []string{}
	for nodeRows.Next() {
		var n nodeInfo
		if err := nodeRows.Scan(&n.ID, &n.Label, &n.Namespace, &n.NodeType, &n.Content); err != nil {
			continue
		}
		nodes[n.ID] = n
		nodeIDs = append(nodeIDs, n.ID)
	}

	if len(nodes) == 0 {
		respondJSON(w, 200, map[string]any{"edges_created": 0, "nodes": 0, "message": "no nodes found"})
		return
	}

	// Fetch edges
	edgeRows, err := h.pool.Query(ctx, `
		SELECT source_id, target_id FROM edges
		WHERE source_id = ANY($1) AND target_id = ANY($1)
	`, nodeIDs)
	if err != nil {
		respondError(w, 500, "edge query failed")
		return
	}
	defer edgeRows.Close()

	adj := make(map[string][]string)
	for _, id := range nodeIDs {
		adj[id] = []string{}
	}
	for edgeRows.Next() {
		var src, tgt string
		if err := edgeRows.Scan(&src, &tgt); err != nil {
			continue
		}
		adj[src] = append(adj[src], tgt)
		adj[tgt] = append(adj[tgt], src)
	}

	// Find connected components via BFS
	visited := make(map[string]bool)
	var components [][]string
	for _, id := range nodeIDs {
		if visited[id] {
			continue
		}
		comp := []string{}
		queue := []string{id}
		visited[id] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			for _, nb := range adj[cur] {
				if !visited[nb] {
					visited[nb] = true
					queue = append(queue, nb)
				}
			}
		}
		components = append(components, comp)
	}

	if len(components) <= 1 {
		respondJSON(w, 200, map[string]any{"edges_created": 0, "components": 1, "message": "graph is fully connected"})
		return
	}

	// Largest component is the "main" graph
	largestIdx := 0
	largestSize := len(components[0])
	for i, comp := range components {
		if len(comp) > largestSize {
			largestSize = len(comp)
			largestIdx = i
		}
	}
	mainComponent := components[largestIdx]
	mainSet := make(map[string]bool)
	for _, id := range mainComponent {
		mainSet[id] = true
	}

	// Collect all nodes in smaller components
	var orphanNodes []nodeInfo
	for i, comp := range components {
		if i == largestIdx {
			continue
		}
		for _, id := range comp {
			if n, ok := nodes[id]; ok {
				orphanNodes = append(orphanNodes, n)
			}
		}
	}

	created := 0
	for _, on := range orphanNodes {
		searchQuery := on.Label
		if len(on.Content) > 50 {
			searchQuery += " " + on.Content[:50]
		}

		// Find most similar node in main component by label similarity
		var bestID string
		var bestSim float32
		for _, mainID := range mainComponent {
			mn := nodes[mainID]
			// Simple trigram-like similarity: count common substrings of length 3
			sim := similarity(searchQuery, mn.Label)
			if sim > bestSim {
				bestSim = sim
				bestID = mainID
			}
		}

		if bestID != "" && bestSim > 0.05 {
			if !dryRun {
				_, err := h.pool.Exec(ctx, `
					INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight)
					VALUES ('hermes', $1, $2, 'relates_to', 0.5) ON CONFLICT DO NOTHING
				`, on.ID, bestID)
				if err == nil {
					created++
				}
			} else {
				created++
			}
		}
	}

	respondJSON(w, 200, map[string]any{
		"edges_created":   created,
		"components":      len(components),
		"main_component":  largestSize,
		"orphan_nodes":    len(orphanNodes),
		"namespace":       ns,
	})
}

// similarity returns a rough trigram similarity between two strings (0-1 scale).
func similarity(a, b string) float32 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return 1.0
	}
	if len(a) < 3 || len(b) < 3 {
		return 0.0
	}
	// Count common trigrams
	setA := make(map[string]bool)
	for i := 0; i <= len(a)-3; i++ {
		setA[a[i:i+3]] = true
	}
	common := 0
	for i := 0; i <= len(b)-3; i++ {
		if setA[b[i:i+3]] {
			common++
		}
	}
	maxTrigrams := max(len(a)-2, len(b)-2)
	if maxTrigrams <= 0 {
		return 0
	}
	return float32(common) / float32(maxTrigrams)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
