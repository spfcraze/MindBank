package handler

import (
	"net/http"
	"time"
)

// Evolution handles GET /api/v1/analyze/evolution
// Computes a PEMS-inspired evolution maturity score for a node.
func (h *AnalyzeHandler) Evolution(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		respondError(w, 400, "node_id required")
		return
	}

	ctx := r.Context()

	// Get node + all versions via predecessor chain
	var currentID, label, nodeType string
	var content string
	var importance float64
	var accessCount int
	var createdAt time.Time
	var validTo *time.Time

	err := h.pool.QueryRow(ctx, `
		SELECT id, label, node_type::text, content, importance, access_count, created_at, valid_to
		FROM nodes
		WHERE id = $1
	`, nodeID).Scan(&currentID, &label, &nodeType, &content, &importance, &accessCount, &createdAt, &validTo)
	if err != nil {
		respondError(w, 404, "node not found")
		return
	}

	// Query all versions (predecessor chain)
	rows, err := h.pool.Query(ctx, `
		SELECT id, label, content, importance, access_count, created_at, valid_to
		FROM nodes
		WHERE id = $1 OR predecessor_id = $1
		ORDER BY created_at ASC
	`, nodeID)
	if err != nil {
		respondError(w, 500, "version query failed")
		return
	}
	defer rows.Close()

	type versionInfo struct {
		ID          string  `json:"id"`
		Label       string  `json:"label"`
		ContentLen  int     `json:"content_length"`
		Importance  float64 `json:"importance"`
		AccessCount int     `json:"access_count"`
		AgeDays     int     `json:"age_days"`
	}

	var versions []versionInfo
	var totalAccessCount int
	var firstCreatedAt time.Time
	var versionCount int

	for rows.Next() {
		var v versionInfo
		var vCreatedAt time.Time
		var vValidTo *time.Time
		var vContent string
		err := rows.Scan(&v.ID, &v.Label, &vContent, &v.Importance, &v.AccessCount, &vCreatedAt, &vValidTo)
		if err != nil {
			continue
		}
		v.ContentLen = len(vContent)
		v.AgeDays = int(time.Since(vCreatedAt).Hours() / 24)
		versions = append(versions, v)
		totalAccessCount += v.AccessCount
		if versionCount == 0 {
			firstCreatedAt = vCreatedAt
		}
		versionCount++
	}

	// Compute evolution metrics
	ageDays := int(time.Since(firstCreatedAt).Hours()/24) + 1
	successRate := float64(totalAccessCount) / float64(ageDays)

	// Maturity level based on age and access patterns
	maturityLevel := "volatile"
	if versionCount >= 3 && ageDays > 30 {
		maturityLevel = "stable"
	} else if versionCount >= 2 || ageDays > 7 {
		maturityLevel = "evolving"
	}

	// Evolution score: combines importance, access rate, and version stability
	// Higher score = more mature and valuable node
	evolutionScore := importance * float64(successRate)
	if versionCount > 1 {
		// Penalize high version count (volatility)
		evolutionScore = evolutionScore / float64(versionCount)
	}

	// Convergence delta: how much the node has changed over versions
	// (simplified: based on version count and age)
	convergenceDelta := 1.0
	if versionCount > 1 {
		convergenceDelta = float64(versionCount) / float64(ageDays)
	}

	respondJSON(w, 200, map[string]any{
		"node_id":           nodeID,
		"label":             label,
		"node_type":         nodeType,
		"evolution_score":   evolutionScore,
		"maturity_level":    maturityLevel,
		"version_count":     versionCount,
		"total_access":      totalAccessCount,
		"age_days":          ageDays,
		"success_rate":      successRate,
		"convergence_delta": convergenceDelta,
		"versions":          versions,
		"is_current":        validTo == nil,
	})
}
