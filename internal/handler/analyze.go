package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AnalyzeHandler provides intelligence endpoints for graph analysis.
type AnalyzeHandler struct {
	pool *pgxpool.Pool
}

// NewAnalyzeHandler creates a new analyze handler.
func NewAnalyzeHandler(pool *pgxpool.Pool) *AnalyzeHandler {
	return &AnalyzeHandler{pool: pool}
}

// Contradictions handles GET /api/v1/analyze/contradictions
// Finds node pairs connected by "contradicts" edges with analysis.
func (h *AnalyzeHandler) Contradictions(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")

	query := `
		SELECT
			e.id AS edge_id,
			s.id AS source_id, s.label AS source_label, s.node_type::text AS source_type,
			s.namespace AS source_ns, s.content AS source_content,
			t.id AS target_id, t.label AS target_label, t.node_type::text AS target_type,
			t.namespace AS target_ns, t.content AS target_content,
			e.weight
		FROM edges e
		JOIN nodes s ON e.source_id = s.id AND s.valid_to IS NULL
		JOIN nodes t ON e.target_id = t.id AND t.valid_to IS NULL
		WHERE e.edge_type = 'contradicts'`
	args := []any{}

	if ns != "" {
		query += " AND (s.namespace = $1 OR t.namespace = $1)"
		args = append(args, ns)
	}
	query += " ORDER BY e.weight DESC LIMIT 50"

	rows, err := h.pool.Query(r.Context(), query, args...)
	if err != nil {
		respondError(w, 500, "contradictions query failed")
		return
	}
	defer rows.Close()

	type contradiction struct {
		EdgeID      string  `json:"edge_id"`
		SourceID    string  `json:"source_id"`
		SourceLabel string  `json:"source_label"`
		SourceType  string  `json:"source_type"`
		SourceNS    string  `json:"source_namespace"`
		SourceShort string  `json:"source_summary"`
		TargetID    string  `json:"target_id"`
		TargetLabel string  `json:"target_label"`
		TargetType  string  `json:"target_type"`
		TargetNS    string  `json:"target_namespace"`
		TargetShort string  `json:"target_summary"`
		Weight      float32 `json:"weight"`
		CrossNS     bool    `json:"cross_namespace"`
	}

	var results []contradiction
	for rows.Next() {
		var c contradiction
		var srcContent, tgtContent string
		if err := rows.Scan(&c.EdgeID, &c.SourceID, &c.SourceLabel, &c.SourceType, &c.SourceNS, &srcContent,
			&c.TargetID, &c.TargetLabel, &c.TargetType, &c.TargetNS, &tgtContent, &c.Weight); err != nil {
			continue
		}
		c.SourceShort = truncate(srcContent, 100)
		c.TargetShort = truncate(tgtContent, 100)
		c.CrossNS = c.SourceNS != c.TargetNS
		results = append(results, c)
	}
	if results == nil {
		results = []contradiction{}
	}

	respondJSON(w, 200, map[string]any{
		"contradictions": results,
		"count":          len(results),
	})
}

// Gaps handles GET /api/v1/analyze/gaps
// Finds knowledge gaps: orphan nodes, unanswered questions, unsolved problems, stale nodes.
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
	if err == nil {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount, &g.EdgeCount); err == nil {
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
	if err == nil {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount); err == nil {
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
	if err == nil {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount); err == nil {
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
	if err == nil {
		for rows.Next() {
			var g gap
			if err := rows.Scan(&g.NodeID, &g.Label, &g.Namespace, &g.NodeType, &g.AgeDays, &g.AccessCount, &g.EdgeCount); err == nil {
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

// Diff handles GET /api/v1/analyze/diff
// Shows what changed since a given time. Supports: since=ISO8601 or since=last-session
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
	nsFilter := ""
	args := []any{sinceTime}
	if ns != "" {
		nsFilter = " AND namespace = $2"
		args = append(args, ns)
	}

	// New nodes
	newNodesQuery := `
		SELECT id, label, namespace, node_type::text, created_at
		FROM nodes
		WHERE valid_to IS NULL AND created_at > $1` + nsFilter + `
		ORDER BY created_at DESC
		LIMIT 50`

	type nodeRef struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Namespace string `json:"namespace"`
		NodeType  string `json:"node_type"`
		CreatedAt string `json:"created_at"`
	}

	var newNodes []nodeRef
	rows, err := h.pool.Query(r.Context(), newNodesQuery, args...)
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
	updateQuery := `
		SELECT id, label, namespace, node_type::text, updated_at, version
		FROM nodes
		WHERE valid_to IS NULL
		AND updated_at > created_at
		AND updated_at > $1` + nsFilter + `
		ORDER BY updated_at DESC
		LIMIT 50`

	var updatedNodes []nodeRef
	uArgs := []any{sinceTime}
	if ns != "" {
		uArgs = append(uArgs, ns)
	}
	rows, err = h.pool.Query(r.Context(), updateQuery, uArgs...)
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
	eArgs := []any{sinceTime}
	if ns != "" {
		edgeQuery += " AND (s.namespace = $2 OR t.namespace = $2)"
		eArgs = append(eArgs, ns)
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
	rows, err = h.pool.Query(r.Context(), edgeQuery, eArgs...)
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
	deletedQuery := `
		SELECT id, label, namespace, node_type::text, valid_to
		FROM nodes
		WHERE valid_to IS NOT NULL AND valid_to > $1` + nsFilter + `
		ORDER BY valid_to DESC
		LIMIT 50`

	dArgs := []any{sinceTime}
	if ns != "" {
		dArgs = append(dArgs, ns)
	}
	var deletedNodes []nodeRef
	rows, err = h.pool.Query(r.Context(), deletedQuery, dArgs...)
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

// truncate returns the first n characters of s with "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}

// Patterns handles GET /api/v1/analyze/patterns
// Finds groups of semantically similar nodes across namespaces by analyzing
// shared edge connections, matching labels, and same node types.
func (h *AnalyzeHandler) Patterns(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")

	// Strategy 1: Same-label nodes across namespaces (same concept, different projects)
	labelQuery := `
		SELECT ARRAY_AGG(DISTINCT n.namespace) AS namespaces,
			ARRAY_AGG(n.id) AS node_ids,
			n.label,
			n.node_type::text AS node_type,
			COUNT(*) AS count
		FROM nodes n
		WHERE n.valid_to IS NULL
		GROUP BY LOWER(TRIM(n.label)), n.node_type
		HAVING COUNT(DISTINCT n.namespace) > 1
		ORDER BY COUNT(*) DESC
		LIMIT 20`

	rows, err := h.pool.Query(r.Context(), labelQuery)
	type pattern struct {
		Type        string   `json:"type"` // "shared-label", "connected", "type-cluster"
		Label       string   `json:"label"`
		NodeType    string   `json:"node_type"`
		Namespaces  []string `json:"namespaces"`
		NodeIDs     []string `json:"node_ids"`
		NodeCount   int      `json:"node_count"`
		Description string   `json:"description"`
	}
	var results []pattern

	if err == nil {
		for rows.Next() {
			var nsArr, idArr []string
			var label, nodeType string
			var count int
			if err := rows.Scan(&nsArr, &idArr, &label, &nodeType, &count); err == nil {
				results = append(results, pattern{
					Type:        "shared-label",
					Label:       label,
					NodeType:    nodeType,
					Namespaces:  nsArr,
					NodeIDs:     idArr,
					NodeCount:   count,
					Description: "\"" + label + "\" exists in " + itoa(len(nsArr)) + " namespaces as " + nodeType,
				})
			}
		}
		rows.Close()
	}

	// Strategy 2: Nodes connected across namespaces (cross-project dependencies)
	crossQuery := `
		SELECT s.namespace AS src_ns, t.namespace AS tgt_ns,
			e.edge_type::text,
			COUNT(*) AS edge_count,
			ARRAY_AGG(DISTINCT s.label || ' → ' || t.label ORDER BY s.label LIMIT 3) AS examples
		FROM edges e
		JOIN nodes s ON e.source_id = s.id AND s.valid_to IS NULL
		JOIN nodes t ON e.target_id = t.id AND t.valid_to IS NULL
		WHERE s.namespace != t.namespace`
	if ns != "" && ns != "all" {
		crossQuery += " AND (s.namespace = $1 OR t.namespace = $1)"
	}
	crossQuery += `
		GROUP BY s.namespace, t.namespace, e.edge_type
		HAVING COUNT(*) >= 2
		ORDER BY edge_count DESC
		LIMIT 20`

	cArgs := []any{}
	if ns != "" && ns != "all" {
		cArgs = append(cArgs, ns)
	}
	rows, err = h.pool.Query(r.Context(), crossQuery, cArgs...)
	if err == nil {
		for rows.Next() {
			var srcNS, tgtNS, edgeType string
			var edgeCount int
			var examples []string
			if err := rows.Scan(&srcNS, &tgtNS, &edgeType, &edgeCount, &examples); err == nil {
				results = append(results, pattern{
					Type:       "cross-project",
					Label:      srcNS + " " + edgeType + " " + tgtNS,
					NodeType:   edgeType,
					Namespaces: []string{srcNS, tgtNS},
					NodeCount:  edgeCount,
					Description: itoa(edgeCount) + " " + edgeType + " edges between " + srcNS + " and " + tgtNS,
				})
			}
		}
		rows.Close()
	}

	// Strategy 3: Type clusters — node types concentrated in specific namespaces
	typeQuery := `
		SELECT n.node_type::text, n.namespace, COUNT(*) AS cnt
		FROM nodes n
		WHERE n.valid_to IS NULL`
	if ns != "" && ns != "all" {
		typeQuery += " AND n.namespace = $1"
	}
	typeQuery += `
		GROUP BY n.node_type, n.namespace
		HAVING COUNT(*) >= 5
		ORDER BY cnt DESC
		LIMIT 15`

	tArgs := []any{}
	if ns != "" && ns != "all" {
		tArgs = append(tArgs, ns)
	}
	rows, err = h.pool.Query(r.Context(), typeQuery, tArgs...)
	if err == nil {
		for rows.Next() {
			var nodeType, namespace string
			var cnt int
			if err := rows.Scan(&nodeType, &namespace, &cnt); err == nil {
				results = append(results, pattern{
					Type:        "type-cluster",
					Label:       nodeType + " cluster in " + namespace,
					NodeType:    nodeType,
					Namespaces:  []string{namespace},
					NodeCount:   cnt,
					Description: itoa(cnt) + " " + nodeType + " nodes in " + namespace + " namespace",
				})
			}
		}
		rows.Close()
	}

	if results == nil {
		results = []pattern{}
	}

	respondJSON(w, 200, map[string]any{
		"patterns": results,
		"count":    len(results),
	})
}

// Confidence handles GET /api/v1/analyze/confidence?node_id=X or ?namespace=X
// Scores trust/confidence for nodes based on access, connectivity, age, contradictions.
func (h *AnalyzeHandler) Confidence(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	ns := r.URL.Query().Get("namespace")

	if nodeID != "" {
		// Single node confidence
		row := h.pool.QueryRow(r.Context(), `
			SELECT n.id, n.label, n.node_type::text, n.namespace,
				n.access_count, n.importance,
				n.created_at, n.last_accessed,
				(SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id) AS edge_count,
				(SELECT COUNT(*) FROM edges WHERE (source_id = n.id OR target_id = n.id) AND edge_type = 'contradicts') AS contradiction_count
			FROM nodes n
			WHERE n.id = $1 AND n.valid_to IS NULL
		`, nodeID)

		var id, label, nodeType, namespace string
		var accessCount int
		var importance float32
		var createdAt time.Time
		var lastAccessed *time.Time
		var edgeCount, contradictionCount int

		if err := row.Scan(&id, &label, &nodeType, &namespace,
			&accessCount, &importance, &createdAt, &lastAccessed,
			&edgeCount, &contradictionCount); err != nil {
			respondError(w, 404, "node not found")
			return
		}

		// Compute confidence score
		// 30% frequency (access_count/50, capped 1.0)
		// 25% connectivity (edge_count/10, capped 1.0)
		// 20% age stability (min(age_days/90, 1.0))
		// 15% importance
		// 10% negative: contradictions reduce confidence
		ageDays := time.Since(createdAt).Hours() / 24
		frequency := min(float32(accessCount)/50.0, 1.0)
		connectivity := min(float32(edgeCount)/10.0, 1.0)
		ageStability := min(float32(ageDays)/90.0, 1.0)
		contradictionPenalty := min(float32(contradictionCount)*0.15, 0.5)

		confidence := 0.30*frequency + 0.25*connectivity + 0.20*ageStability + 0.15*importance - contradictionPenalty
		if confidence < 0 {
			confidence = 0
		}

		// Determine trust level
		trustLevel := "low"
		if confidence >= 0.6 {
			trustLevel = "high"
		} else if confidence >= 0.35 {
			trustLevel = "medium"
		}

		lastAcc := ""
		if lastAccessed != nil {
			lastAcc = lastAccessed.Format(time.RFC3339)
		}

		respondJSON(w, 200, map[string]any{
			"node_id":              id,
			"label":                label,
			"node_type":            nodeType,
			"namespace":            namespace,
			"confidence":           float64(int(confidence*1000)) / 1000,
			"trust_level":          trustLevel,
			"access_count":         accessCount,
			"edge_count":           edgeCount,
			"contradiction_count":  contradictionCount,
			"age_days":             int(ageDays),
			"importance":           importance,
			"last_accessed":        lastAcc,
			"breakdown": map[string]float64{
				"frequency":     float64(int(frequency*1000)) / 1000,
				"connectivity":  float64(int(connectivity*1000)) / 1000,
				"age_stability": float64(int(ageStability*1000)) / 1000,
				"importance":    float64(importance),
				"contradiction_penalty": float64(int(contradictionPenalty*1000)) / 1000,
			},
		})
		return
	}

	// Namespace-wide confidence report
	nsFilter := ""
	args := []any{}
	if ns != "" {
		nsFilter = " AND n.namespace = $1"
		args = append(args, ns)
	}

	query := `
		SELECT n.id, n.label, n.node_type::text, n.namespace,
			n.access_count, n.importance,
			n.created_at,
			(SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id) AS edge_count,
			(SELECT COUNT(*) FROM edges WHERE (source_id = n.id OR target_id = n.id) AND edge_type = 'contradicts') AS contradiction_count
		FROM nodes n
		WHERE n.valid_to IS NULL` + nsFilter + `
		ORDER BY n.access_count DESC, n.importance DESC
		LIMIT 50`

	rows, err := h.pool.Query(r.Context(), query, args...)
	if err != nil {
		respondError(w, 500, "confidence query failed")
		return
	}
	defer rows.Close()

	type nodeConfidence struct {
		NodeID             string  `json:"node_id"`
		Label              string  `json:"label"`
		NodeType           string  `json:"node_type"`
		Namespace          string  `json:"namespace"`
		Confidence         float64 `json:"confidence"`
		TrustLevel         string  `json:"trust_level"`
		AccessCount        int     `json:"access_count"`
		EdgeCount          int     `json:"edge_count"`
		ContradictionCount int     `json:"contradiction_count"`
		AgeDays            int     `json:"age_days"`
	}

	var results []nodeConfidence
	for rows.Next() {
		var id, label, nodeType, namespace string
		var accessCount int
		var importance float32
		var createdAt time.Time
		var edgeCount, contradictionCount int

		if err := rows.Scan(&id, &label, &nodeType, &namespace,
			&accessCount, &importance, &createdAt,
			&edgeCount, &contradictionCount); err != nil {
			continue
		}

		ageDays := time.Since(createdAt).Hours() / 24
		frequency := min(float32(accessCount)/50.0, 1.0)
		connectivity := min(float32(edgeCount)/10.0, 1.0)
		ageStability := min(float32(ageDays)/90.0, 1.0)
		contradictionPenalty := min(float32(contradictionCount)*0.15, 0.5)

		confidence := 0.30*frequency + 0.25*connectivity + 0.20*ageStability + 0.15*importance - contradictionPenalty
		if confidence < 0 {
			confidence = 0
		}

		trustLevel := "low"
		if confidence >= 0.6 {
			trustLevel = "high"
		} else if confidence >= 0.35 {
			trustLevel = "medium"
		}

		results = append(results, nodeConfidence{
			NodeID:             id,
			Label:              label,
			NodeType:           nodeType,
			Namespace:          namespace,
			Confidence:         float64(int(confidence*1000)) / 1000,
			TrustLevel:         trustLevel,
			AccessCount:        accessCount,
			EdgeCount:          edgeCount,
			ContradictionCount: contradictionCount,
			AgeDays:            int(ageDays),
		})
	}
	if results == nil {
		results = []nodeConfidence{}
	}

	// Summary stats
	highCount, medCount, lowCount := 0, 0, 0
	for _, r := range results {
		switch r.TrustLevel {
		case "high":
			highCount++
		case "medium":
			medCount++
		default:
			lowCount++
		}
	}

	respondJSON(w, 200, map[string]any{
		"nodes":    results,
		"count":    len(results),
		"summary": map[string]int{
			"high":   highCount,
			"medium": medCount,
			"low":    lowCount,
		},
		"namespace": ns,
	})
}

// LinkOrphans handles POST /api/v1/analyze/link-orphans
// Finds orphan nodes (0 edges) and links them to the most similar node via search.
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
			edgesMoved := 0
			for _, oldID := range toMerge {
				tag, _ := h.pool.Exec(r.Context(), `UPDATE edges SET source_id=$1 WHERE source_id=$2 AND target_id!=$1`, keepID, oldID)
				edgesMoved += int(tag.RowsAffected())
				tag, _ = h.pool.Exec(r.Context(), `UPDATE edges SET target_id=$1 WHERE target_id=$2 AND source_id!=$1`, keepID, oldID)
				edgesMoved += int(tag.RowsAffected())
				h.pool.Exec(r.Context(), `UPDATE nodes SET valid_to=now() WHERE id=$1 AND valid_to IS NULL`, oldID)
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
