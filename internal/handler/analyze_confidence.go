package handler

import (
	"net/http"
	"time"
)

// Confidence handles GET /api/v1/analyze/confidence
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
