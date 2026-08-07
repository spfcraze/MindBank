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

	// Query all versions (predecessor chain) — includes current node + all predecessors
	// A node may have multiple predecessors in a linear chain: v3 -> v2 -> v1
	// We walk backward from current node through predecessor_id links
	rows, err := h.pool.Query(ctx, `
		WITH RECURSIVE version_chain AS (
			-- Anchor: current node
			SELECT id, label, content, importance, access_count, created_at, valid_to, predecessor_id
			FROM nodes
			WHERE id = $1
			UNION ALL
			-- Recursive: walk backward through predecessors
			SELECT n.id, n.label, n.content, n.importance, n.access_count, n.created_at, n.valid_to, n.predecessor_id
			FROM nodes n
			INNER JOIN version_chain vc ON n.id = vc.predecessor_id
		)
		SELECT id, label, content, importance, access_count, created_at, valid_to
		FROM version_chain
		ORDER BY created_at ASC
		LIMIT 50
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
		IsCurrent   bool    `json:"is_current"`
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
		if err := rows.Scan(&v.ID, &v.Label, &vContent, &v.Importance, &v.AccessCount, &vCreatedAt, &vValidTo); err != nil {
			respondError(w, 500, "version scan failed: "+err.Error())
			return
		}
		v.ContentLen = len(vContent)
		v.AgeDays = int(time.Since(vCreatedAt).Hours() / 24)
		v.IsCurrent = (vValidTo == nil)
		versions = append(versions, v)
		totalAccessCount += v.AccessCount
		if versionCount == 0 {
			firstCreatedAt = vCreatedAt
		}
		versionCount++
	}
	if err := rows.Err(); err != nil {
		respondError(w, 500, "version row iteration failed: "+err.Error())
		return
	}

	// Compute evolution metrics
	ageDays := int(time.Since(firstCreatedAt).Hours()/24) + 1
	if ageDays < 1 {
		ageDays = 1
	}
	successRate := float64(totalAccessCount) / float64(ageDays)

	// Maturity level based on age, access patterns, and version stability
	// stable = old + many versions + still accessed
	// evolving = moderate age or some versions
	// volatile = new or single version
	maturityLevel := "volatile"
	if versionCount >= 3 && ageDays > 30 && totalAccessCount > 0 {
		maturityLevel = "stable"
	} else if (versionCount >= 2 && ageDays > 7) || (ageDays > 14 && totalAccessCount > 0) {
		maturityLevel = "evolving"
	}

	// Evolution score: combines importance, access rate, and version stability
	// Formula: importance * success_rate * stability_bonus
	// stability_bonus rewards nodes that have versions but are still accessed (not abandoned)
	stabilityBonus := 1.0
	if versionCount > 1 {
		// More versions with continued access = more stable knowledge
		stabilityBonus = 1.0 + (float64(totalAccessCount) / float64(versionCount) / 10.0)
		if stabilityBonus > 2.0 {
			stabilityBonus = 2.0
		}
	}
	evolutionScore := importance * successRate * stabilityBonus

	// Convergence delta: measures how much the node has stabilized
	// 0.0 = perfectly stable (no changes, high access)
	// 1.0+ = still converging (frequent changes or low access)
	convergenceDelta := 1.0
	if versionCount > 1 {
		// Fewer versions per day = more converged
		convergenceDelta = float64(versionCount) / float64(ageDays)
		if convergenceDelta > 1.0 {
			convergenceDelta = 1.0 // cap at 1.0 (max convergence)
		}
	} else if totalAccessCount > 0 {
		// Single version with access = partially converged
		convergenceDelta = 0.5
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
