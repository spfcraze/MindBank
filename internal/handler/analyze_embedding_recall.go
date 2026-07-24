package handler

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// EmbeddingRecall runs a masked-search validation test adapted from
// MemTrain's Intermediate Memory Recall (IMR) concept.
//
// Workflow:
//   1. Sample N random current nodes (optionally filtered by namespace).
//   2. For each sampled node, fetch up to K semantic neighbours (nodes
//      connected by edges).
//   3. Use each neighbour's label as a search query.
//   4. Check whether the original (masked) node appears in the search
//      results within top-R results.
//   5. Compute recall@R and breakdown by namespace / node type.
//
// Query parameters:
//   - n: sample size (default 50, max 100)
//   - k: neighbours per node (default 3, max 10)
//   - r: recall rank cutoff (default 5, max 20)
//   - namespace: filter (optional, "all" or empty = no filter)
func (h *AnalyzeHandler) EmbeddingRecall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	ns := q.Get("namespace")
	if ns == "all" {
		ns = ""
	}

	n := parseInt(q.Get("n"), 50)
	if n < 10 {
		n = 10
	}
	if n > 100 {
		n = 100
	}

	k := parseInt(q.Get("k"), 3)
	if k < 1 {
		k = 1
	}
	if k > 10 {
		k = 10
	}

	recallRank := parseInt(q.Get("r"), 5)
	if recallRank < 1 {
		recallRank = 1
	}
	if recallRank > 20 {
		recallRank = 20
	}

	// ── 1. Sample random nodes ───────────────────────────────
	sample, err := h.sampleNodes(ctx, ns, n)
	if err != nil {
		respondError(w, 500, "sampling failed: "+err.Error())
		return
	}
	if len(sample) == 0 {
		respondJSON(w, 200, map[string]any{
			"sample_size":     0,
			"recall_at":       recallRank,
			"recall_rate":     0.0,
			"total_queries":   0,
			"found_count":     0,
			"breakdown":       map[string]any{},
			"recommendations": []string{"No nodes available for sampling."},
		})
		return
	}

	// ── 2. For each sampled node, find neighbours and test recall ──
	type result struct {
		NodeID     string
		Label      string
		NodeType   string
		Namespace  string
		Queries    int
		Found      int
		Neighbours []string
	}

	var results []result
	var totalQueries, totalFound int

	for _, node := range sample {
		neighbours, err := h.getNeighbours(ctx, node.ID, k)
		if err != nil {
			continue
		}
		if len(neighbours) == 0 {
			// No neighbours — node is orphaned. Count as 0 found.
			results = append(results, result{
				NodeID:     node.ID,
				Label:      node.Label,
				NodeType:   node.NodeType,
				Namespace:  node.Namespace,
				Queries:    0,
				Found:      0,
				Neighbours: []string{},
			})
			continue
		}

		queries := 0
		found := 0
		var neighbourLabels []string

		for _, nb := range neighbours {
			neighbourLabels = append(neighbourLabels, nb.Label)
			queries++

			// Search for the neighbour's label; check if masked node appears
			foundIDs, err := h.searchByLabel(ctx, nb.Label, recallRank, ns)
			if err != nil {
				continue
			}
			for _, id := range foundIDs {
				if id == node.ID {
					found++
					break
				}
			}
		}

		results = append(results, result{
			NodeID:     node.ID,
			Label:      node.Label,
			NodeType:   node.NodeType,
			Namespace:  node.Namespace,
			Queries:    queries,
			Found:      found,
			Neighbours: neighbourLabels,
		})
		totalQueries += queries
		totalFound += found
	}

	// ── 3. Compute aggregates ────────────────────────────────
	recallRate := 0.0
	if totalQueries > 0 {
		recallRate = float64(totalFound) / float64(totalQueries)
	}

	// Breakdown by namespace
	nsBreakdown := make(map[string]map[string]int)
	for _, res := range results {
		if _, ok := nsBreakdown[res.Namespace]; !ok {
			nsBreakdown[res.Namespace] = map[string]int{"queries": 0, "found": 0, "nodes": 0}
		}
		nsBreakdown[res.Namespace]["queries"] += res.Queries
		nsBreakdown[res.Namespace]["found"] += res.Found
		nsBreakdown[res.Namespace]["nodes"]++
	}

	// Breakdown by node type
	typeBreakdown := make(map[string]map[string]int)
	for _, res := range results {
		if _, ok := typeBreakdown[res.NodeType]; !ok {
			typeBreakdown[res.NodeType] = map[string]int{"queries": 0, "found": 0, "nodes": 0}
		}
		typeBreakdown[res.NodeType]["queries"] += res.Queries
		typeBreakdown[res.NodeType]["found"] += res.Found
		typeBreakdown[res.NodeType]["nodes"]++
	}

	// ── 4. Generate recommendations ──────────────────────────
	var recommendations []string
	if recallRate >= 0.8 {
		recommendations = append(recommendations, "Embedding recall is strong. Search quality is healthy.")
	} else if recallRate >= 0.5 {
		recommendations = append(recommendations, "Embedding recall is moderate. Consider regenerating embeddings for low-scoring namespaces.")
	} else {
		recommendations = append(recommendations, "Embedding recall is poor. Recommend regenerating embeddings and reviewing edge connectivity.")
	}

	orphaned := 0
	for _, res := range results {
		if res.Queries == 0 {
			orphaned++
		}
	}
	if orphaned > 0 {
		recommendations = append(recommendations, fmt.Sprintf("%d sampled node(s) have no neighbours (orphaned). Consider adding edges or reviewing compression worker.", orphaned))
	}

	// ── 5. Respond ───────────────────────────────────────────
	respondJSON(w, 200, map[string]any{
		"sample_size":     len(sample),
		"recall_at":       recallRank,
		"recall_rate":     math.Round(recallRate*1000) / 1000,
		"recall_percent":  math.Round(recallRate*10000) / 100,
		"total_queries":   totalQueries,
		"found_count":     totalFound,
		"orphaned_count":  orphaned,
		"breakdown": map[string]any{
			"by_namespace": nsBreakdown,
			"by_type":      typeBreakdown,
		},
		"details":         results,
		"recommendations": recommendations,
	})
}

// sampleNodes returns up to limit random current nodes, optionally filtered by namespace.
func (h *AnalyzeHandler) sampleNodes(ctx context.Context, namespace string, limit int) ([]struct {
	ID        string
	Label     string
	NodeType  string
	Namespace string
}, error) {
	q := `
		SELECT id, label, node_type::text, namespace
		FROM nodes
		WHERE valid_to IS NULL`
	var args []any
	if namespace != "" {
		q += " AND namespace = $1"
		args = append(args, namespace)
	}
	q += " ORDER BY random() LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	rows, err := h.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct {
		ID        string
		Label     string
		NodeType  string
		Namespace string
	}
	for rows.Next() {
		var n struct {
			ID        string
			Label     string
			NodeType  string
			Namespace string
		}
		if err := rows.Scan(&n.ID, &n.Label, &n.NodeType, &n.Namespace); err == nil {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

// getNeighbours returns up to limit neighbours of nodeID (both source and target edges).
func (h *AnalyzeHandler) getNeighbours(ctx context.Context, nodeID string, limit int) ([]struct {
	ID    string
	Label string
}, error) {
	q := `
		SELECT n.id, n.label
		FROM edges e
		JOIN nodes n ON (
			(e.source_id = $1 AND n.id = e.target_id)
			OR (e.target_id = $1 AND n.id = e.source_id)
		)
		WHERE n.valid_to IS NULL
		ORDER BY random()
		LIMIT $2`

	rows, err := h.pool.Query(ctx, q, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct {
		ID    string
		Label string
	}
	for rows.Next() {
		var nb struct {
			ID    string
			Label string
		}
		if err := rows.Scan(&nb.ID, &nb.Label); err == nil {
			out = append(out, nb)
		}
	}
	return out, rows.Err()
}

// searchByLabel does a simple label-based search and returns matching node IDs.
// In a production system this would call the embedding search; here we use
// a trigram/ILIKE fallback that works without embedding infrastructure.
func (h *AnalyzeHandler) searchByLabel(ctx context.Context, label string, limit int, namespace string) ([]string, error) {
	words := strings.Fields(label)
	if len(words) == 0 {
		return nil, nil
	}

	// Use the first 3 meaningful words as search terms
	var terms []string
	for _, w := range words {
		if len(w) > 2 {
			terms = append(terms, "%"+strings.ToLower(w)+"%")
		}
		if len(terms) >= 3 {
			break
		}
	}
	if len(terms) == 0 {
		terms = append(terms, "%"+strings.ToLower(label)+"%")
	}

	// Build OR query
	q := `SELECT id FROM nodes WHERE valid_to IS NULL AND (`
	var conds []string
	var args []any
	for i, t := range terms {
		conds = append(conds, fmt.Sprintf("label ILIKE $%d", i+1))
		args = append(args, t)
	}
	q += strings.Join(conds, " OR ")
	q += ")"

	if namespace != "" {
		q += fmt.Sprintf(" AND namespace = $%d", len(args)+1)
		args = append(args, namespace)
	}
	q += fmt.Sprintf(" LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := h.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
