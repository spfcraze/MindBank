package handler

import (
	"net/http"
)

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
