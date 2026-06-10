package handler

import (
	"log"
	"net/http"
)

func (h *AnalyzeHandler) Gaps(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")

	type gap struct {
		Type        string `json:"type"` // orphan, question, unsolved, stale
		NodeID      string `json:"node_id"`
		Label       string `json:"label"`
		Namespace   string `json:"namespace"`
		NodeType    string `json:"node_type"`
		AgeDays     int    `json:"age_days"`
		AccessCount int    `json:"access_count"`
		EdgeCount   int    `json:"edge_count"`
		Suggestion  string `json:"suggestion"`
	}

	var results []gap
	nsFilter := ""
	args := []any{}
	if ns != "" {
		nsFilter = " AND n.namespace = $1"
		args = append(args, ns)
	}

	// 1. Orphan nodes (0 edges)
	orphanQuery := `
		SELECT n.id, n.label, n.namespace, n.node_type::text,
			EXTRACT(DAY FROM now() - n.created_at)::int AS age_days,
			n.access_count,
			(SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id) AS edge_count
		FROM nodes n
		WHERE n.valid_to IS NULL` + nsFilter + `
		AND NOT EXISTS (
			SELECT 1 FROM edges WHERE source_id = n.id OR target_id = n.id
		)
		ORDER BY n.created_at DESC
		LIMIT 20`

	rows, err := h.pool.Query(r.Context(), orphanQuery, args...)
	if err != nil {
		log.Printf("[gaps] orphan query failed: %v", err)
	} else {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount, &g.EdgeCount); err != nil {
				log.Printf("[gaps] orphan scan failed: %v", err)
			} else {
				g.Type = "orphan"
				g.Suggestion = "Consider linking to related nodes or deleting if obsolete"
				results = append(results, g)
			}
		}
		rows.Close()
	}

	// 2. Unanswered questions
	qQuery := `
		SELECT n.id, n.label, n.namespace, n.node_type::text,
			EXTRACT(DAY FROM now() - n.created_at)::int AS age_days,
			n.access_count
		FROM nodes n
		WHERE n.valid_to IS NULL AND n.node_type = 'question'` + nsFilter + `
		ORDER BY n.created_at DESC
		LIMIT 20`

	qArgs := []any{}
	if ns != "" {
		qArgs = append(qArgs, ns)
	}
	rows, err = h.pool.Query(r.Context(), qQuery, qArgs...)
	if err != nil {
		log.Printf("[gaps] unanswered question query failed: %v", err)
	} else {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount); err != nil {
				log.Printf("[gaps] unanswered question scan failed: %v", err)
			} else {
				g.Type = "unanswered_question"
				g.Suggestion = "Search for answers or store a resolution"
				results = append(results, g)
			}
		}
		rows.Close()
	}

	// 3. Unsolved problems (problems with no "supports" or "solved_by" edges)
	problemQuery := `
		SELECT n.id, n.label, n.namespace, n.node_type::text,
			EXTRACT(DAY FROM now() - n.created_at)::int AS age_days,
			n.access_count
		FROM nodes n
		WHERE n.valid_to IS NULL AND n.node_type = 'problem'` + nsFilter + `
		AND NOT EXISTS (
			SELECT 1 FROM edges WHERE (source_id = n.id OR target_id = n.id)
			AND edge_type IN ('supports', 'solved_by')
		)
		ORDER BY n.created_at DESC
		LIMIT 20`

	pArgs := []any{}
	if ns != "" {
		pArgs = append(pArgs, ns)
	}
	rows, err = h.pool.Query(r.Context(), problemQuery, pArgs...)
	if err != nil {
		log.Printf("[gaps] unsolved problem query failed: %v", err)
	} else {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount); err != nil {
				log.Printf("[gaps] unsolved problem scan failed: %v", err)
			} else {
				g.Type = "unsolved_problem"
				g.Suggestion = "Link to advice/decision that resolves this problem"
				results = append(results, g)
			}
		}
		rows.Close()
	}

	// 4. Stale nodes (0 access, created > 30 days ago)
	staleQuery := `
		SELECT n.id, n.label, n.namespace, n.node_type::text,
			EXTRACT(DAY FROM now() - n.created_at)::int AS age_days,
			n.access_count,
			(SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id) AS edge_count
		FROM nodes n
		WHERE n.valid_to IS NULL
		AND n.access_count = 0
		AND n.created_at < now() - INTERVAL '30 days'` + nsFilter + `
		ORDER BY n.created_at ASC
		LIMIT 20`

	sArgs := []any{}
	if ns != "" {
		sArgs = append(sArgs, ns)
	}
	rows, err = h.pool.Query(r.Context(), staleQuery, sArgs...)
	if err != nil {
		log.Printf("[gaps] stale query failed: %v", err)
	} else {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount, &g.EdgeCount); err != nil {
				log.Printf("[gaps] stale scan failed: %v", err)
			} else {
				g.Type = "stale"
				g.Suggestion = "Review for relevance — never accessed in " + itoa(g.AgeDays) + " days"
				results = append(results, g)
			}
		}
		rows.Close()
	}

	if results == nil {
		results = []gap{}
	}

	// Summarize
	typeCounts := map[string]int{}
	for _, g := range results {
		typeCounts[g.Type]++
	}

	respondJSON(w, 200, map[string]any{
		"gaps":      results,
		"count":     len(results),
		"summary":   typeCounts,
		"namespace": ns,
	})
}
