package handler

import (
	"net/http"
)

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
