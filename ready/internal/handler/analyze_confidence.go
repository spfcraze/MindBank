package handler

import (
	"log"
	"net/http"
	"time"
)

// calculateConfidence computes a V3 confidence score and trust level.
// It is a pure function with no side effects.
func calculateConfidence(accessCount, edgeCount, ageDays int, importance float64, evidenceCount int, epistemicLabel string, contradictionCount int) (float64, string) {
	frequency := min(float64(accessCount)/50.0, 1.0)
	connectivity := min(float64(edgeCount)/10.0, 1.0)
	ageStability := 1.0 - min(float64(ageDays)/365.0, 1.0)

	importanceClamped := importance
	if importanceClamped < 0.0 {
		importanceClamped = 0.0
	}
	if importanceClamped > 1.0 {
		importanceClamped = 1.0
	}

	groundingScore := min(float64(evidenceCount)/5.0, 1.0)

	epistemicBonus := 0.0
	switch epistemicLabel {
	case "observed":
		epistemicBonus = 0.15
	case "inferred":
		epistemicBonus = 0.05
	case "assumed":
		epistemicBonus = -0.15
	default:
		epistemicBonus = 0.0
	}

	contradictionPenalty := min(float64(contradictionCount)*0.10, 0.30)

	score := 0.25*frequency + 0.20*connectivity + 0.15*ageStability + 0.10*importanceClamped + 0.15*groundingScore + 0.15*epistemicBonus - 0.05*contradictionPenalty
	if score < 0.0 {
		score = 0.0
	}
	if score > 1.0 {
		score = 1.0
	}

	trust := "low"
	if score >= 0.60 {
		trust = "high"
	} else if score >= 0.35 {
		trust = "medium"
	}

	return score, trust
}

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
				n.epistemic_label,
				(SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id) AS edge_count,
				(SELECT COUNT(*) FROM edges WHERE (source_id = n.id OR target_id = n.id) AND edge_type = 'contradicts') AS contradiction_count,
				COUNT(CASE WHEN e.edge_type IN ('supports', 'derived_from', 'tested_by') THEN 1 END) as evidence_count
			FROM nodes n
			LEFT JOIN edges e ON (e.source_id = n.id OR e.target_id = n.id)
			WHERE n.id = $1 AND n.valid_to IS NULL
			GROUP BY n.id
		`, nodeID)

		var id, label, nodeType, namespace string
		var accessCount int
		var importance float32
		var createdAt time.Time
		var lastAccessed *time.Time
		var edgeCount, contradictionCount int
		var evidenceCount int
		var epistemicLabel string

		if err := row.Scan(&id, &label, &nodeType, &namespace,
			&accessCount, &importance, &createdAt, &lastAccessed,
			&epistemicLabel, &edgeCount, &contradictionCount, &evidenceCount); err != nil {
			respondError(w, 404, "node not found")
			return
		}

		ageDays := int(time.Since(createdAt).Hours() / 24)
		confidence, trustLevel := calculateConfidence(accessCount, edgeCount, ageDays, float64(importance), evidenceCount, epistemicLabel, contradictionCount)

		lastAcc := ""
		if lastAccessed != nil {
			lastAcc = lastAccessed.Format(time.RFC3339)
		}

		frequency := min(float64(accessCount)/50.0, 1.0)
		connectivity := min(float64(edgeCount)/10.0, 1.0)
		ageStability := 1.0 - min(float64(ageDays)/365.0, 1.0)
		groundingScore := min(float64(evidenceCount)/5.0, 1.0)
		contradictionPenalty := min(float64(contradictionCount)*0.10, 0.30)
		epistemicBonus := 0.0
		switch epistemicLabel {
		case "observed":
			epistemicBonus = 0.15
		case "inferred":
			epistemicBonus = 0.05
		case "assumed":
			epistemicBonus = -0.15
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
			"age_days":             ageDays,
			"importance":           importance,
			"last_accessed":        lastAcc,
			"breakdown": map[string]float64{
				"frequency":            float64(int(frequency*1000)) / 1000,
				"connectivity":         float64(int(connectivity*1000)) / 1000,
				"age_stability":        float64(int(ageStability*1000)) / 1000,
				"importance":           float64(importance),
				"grounding_score":      float64(int(groundingScore*1000)) / 1000,
				"epistemic_bonus":      float64(int(epistemicBonus*1000)) / 1000,
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
			n.created_at, n.epistemic_label,
			(SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id) AS edge_count,
			(SELECT COUNT(*) FROM edges WHERE (source_id = n.id OR target_id = n.id) AND edge_type = 'contradicts') AS contradiction_count,
			COUNT(CASE WHEN e.edge_type IN ('supports', 'derived_from', 'tested_by') THEN 1 END) as evidence_count
		FROM nodes n
		LEFT JOIN edges e ON (e.source_id = n.id OR e.target_id = n.id) AND e.edge_type IN ('supports', 'derived_from', 'tested_by')
		WHERE n.valid_to IS NULL` + nsFilter + `
		GROUP BY n.id
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
		var edgeCount, contradictionCount, evidenceCount int
		var epistemicLabel string

		if err := rows.Scan(&id, &label, &nodeType, &namespace,
			&accessCount, &importance, &createdAt, &epistemicLabel,
			&edgeCount, &contradictionCount, &evidenceCount); err != nil {
			log.Printf("confidence scan error: %v", err)
			continue
		}

		ageDays := int(time.Since(createdAt).Hours() / 24)
		confidence, trustLevel := calculateConfidence(accessCount, edgeCount, ageDays, float64(importance), evidenceCount, epistemicLabel, contradictionCount)

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
			AgeDays:            ageDays,
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
