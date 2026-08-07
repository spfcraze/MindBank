package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"mindbank/internal/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DependenceRepo struct {
	pool *pgxpool.Pool
}

func NewDependenceRepo(pool *pgxpool.Pool) *DependenceRepo {
	return &DependenceRepo{pool: pool}
}

type DependenceNode struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	NodeType   string  `json:"node_type"`
	Namespace  string  `json:"namespace"`
	Importance float32 `json:"importance"`
}

type DependenceEdge struct {
	SourceID string  `json:"source_id"`
	TargetID string  `json:"target_id"`
	EdgeType string  `json:"edge_type"`
	Weight   float32 `json:"weight"`
}

type InfluenceMode struct {
	NodeID         string  `json:"node_id"`
	Label          string  `json:"label"`
	NodeType       string  `json:"node_type"`
	InfluenceScore float64 `json:"influence_score"`
	Depth          int     `json:"depth"`
}

type BlindSpot struct {
	Description string `json:"description"`
	Severity    string `json:"severity"` // low, medium, high
}

func (r *DependenceRepo) GetDependenceGraph(ctx context.Context, seedID string, edgeTypes []string, maxDepth int, minWeight float32) ([]DependenceNode, []DependenceEdge, []InfluenceMode, int, float64, []BlindSpot, error) {
	// Validate edge_types to prevent injection
	validEdgeTypes := map[string]bool{
		"contains": true, "relates_to": true, "depends_on": true, "decided_by": true,
		"participated_in": true, "produced": true, "contradicts": true, "supports": true,
		"temporal_next": true, "mentions": true, "learned_from": true,
		"tested_by": true, "invalidated_by": true, "derived_from": true, "assumed": true,
		"superseded_by": true, "refined_by": true, "merged_into": true,
		"created_by": true, "reviewed_by": true, "executed_by": true,
		"failed_due_to": true, "incompatible_with": true, "precondition_for": true,
	}
	var cleanTypes []string
	for _, et := range edgeTypes {
		if validEdgeTypes[et] {
			cleanTypes = append(cleanTypes, et)
		}
	}
	if len(cleanTypes) == 0 {
		cleanTypes = []string{"depends_on", "learned_from", "decided_by", "produced", "supports",
			"tested_by", "invalidated_by", "derived_from", "assumed",
			"superseded_by", "refined_by", "failed_due_to", "precondition_for"}
	}

	edgeTypeStr := "'" + strings.Join(cleanTypes, "','") + "'"

	// BFS backward traversal using recursive CTE
	query := fmt.Sprintf(`
		WITH RECURSIVE backward AS (
			SELECT e.source_id, e.target_id, e.edge_type::text, e.weight, 1 AS depth
			FROM edges e
			WHERE e.target_id = $1 AND e.edge_type IN (%s) AND e.weight >= $2

			UNION

			SELECT e.source_id, e.target_id, e.edge_type::text, e.weight, b.depth + 1
			FROM edges e
			JOIN backward b ON e.target_id = b.source_id
			WHERE e.edge_type IN (%s) AND e.weight >= $2 AND b.depth < $3
		)
		SELECT source_id, target_id, edge_type, weight, depth FROM backward
	`, edgeTypeStr, edgeTypeStr)

	rows, err := r.pool.Query(ctx, query, seedID, minWeight, maxDepth)
	if err != nil {
		return nil, nil, nil, 0, 0, nil, err
	}
	defer rows.Close()

	edgeMap := make(map[string]DependenceEdge)
	nodeIDs := make(map[string]bool)
	nodeIDs[seedID] = true
	depthWeightSum := make(map[int]float64)
	nodeInfluence := make(map[string]float64)
	nodeDepth := make(map[string]int)

	for rows.Next() {
		var src, tgt, et string
		var weight float32
		var depth int
		if err := rows.Scan(&src, &tgt, &et, &weight, &depth); err != nil {
			continue
		}
		key := src + "->" + tgt
		edgeMap[key] = DependenceEdge{SourceID: src, TargetID: tgt, EdgeType: et, Weight: weight}
		nodeIDs[src] = true
		nodeIDs[tgt] = true
		decay := 0.7 // importance decay per depth level
		influence := float64(weight) * powFloat64(decay, depth)
		depthWeightSum[depth] += influence
		nodeInfluence[src] += influence
		if d, ok := nodeDepth[src]; !ok || depth < d {
			nodeDepth[src] = depth
		}
	}

	// Fetch node details
	var nodes []DependenceNode
	var influenceModes []InfluenceMode
	if len(nodeIDs) > 0 {
		idList := make([]string, 0, len(nodeIDs))
		for id := range nodeIDs {
			idList = append(idList, id)
		}
		nodeQuery := `
			SELECT id, label, node_type::text, namespace, importance
			FROM nodes
			WHERE id = ANY($1) AND valid_to IS NULL
		`
		nodeRows, err := r.pool.Query(ctx, nodeQuery, idList)
		if err == nil {
			defer nodeRows.Close()
			for nodeRows.Next() {
				var n DependenceNode
				if err := nodeRows.Scan(&n.ID, &n.Label, &n.NodeType, &n.Namespace, &n.Importance); err == nil {
					nodes = append(nodes, n)
					if inf, ok := nodeInfluence[n.ID]; ok && n.ID != seedID {
						influenceModes = append(influenceModes, InfluenceMode{
							NodeID:         n.ID,
							Label:          n.Label,
							NodeType:       n.NodeType,
							InfluenceScore: inf,
							Depth:          nodeDepth[n.ID],
						})
					}
				}
			}
		}
	}

	var edges []DependenceEdge
	for _, e := range edgeMap {
		edges = append(edges, e)
	}

	// Sort influence modes by score desc
	sort.Slice(influenceModes, func(i, j int) bool {
		return influenceModes[i].InfluenceScore > influenceModes[j].InfluenceScore
	})

	// Compute critical depth: depth at which 90% of total influence is captured
	totalInfluence := 0.0
	for _, inf := range nodeInfluence {
		totalInfluence += inf
	}
	cumulative := 0.0
	criticalDepth := maxDepth
	for d := 1; d <= maxDepth; d++ {
		cumulative += depthWeightSum[d]
		if totalInfluence > 0 && cumulative/totalInfluence >= 0.9 {
			criticalDepth = d
			break
		}
	}

	// Coverage: fraction of seed's immediate edges that have upstream precursors
	coverage := 0.0
	if len(edges) > 0 {
		seedEdgeCount := 0
		seedWithPrecursor := 0
		for _, e := range edges {
			if e.TargetID == seedID {
				seedEdgeCount++
				for _, e2 := range edges {
					if e2.TargetID == e.SourceID {
						seedWithPrecursor++
						break
					}
				}
			}
		}
		if seedEdgeCount > 0 {
			coverage = float64(seedWithPrecursor) / float64(seedEdgeCount)
		}
	}

	// Blind spots: missing edge types, unresolved contradictions
	var blindSpots []BlindSpot
	// Check for contradictions on seed without resolution
	contradictionQuery := `
		SELECT s.label, s.id
		FROM edges e
		JOIN nodes s ON e.source_id = s.id
		WHERE e.target_id = $1 AND e.edge_type = 'contradicts' AND s.valid_to IS NULL
	`
	crows, err := r.pool.Query(ctx, contradictionQuery, seedID)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var label, id string
			if err := crows.Scan(&label, &id); err == nil {
				// Check if there's a supporting edge that resolves it
				var resolved bool
				_ = r.pool.QueryRow(ctx, `
					SELECT EXISTS(SELECT 1 FROM edges WHERE target_id = $1 AND edge_type IN ('supports', 'tested_by', 'refined_by') AND source_id = $2)
				`, seedID, id).Scan(&resolved)
				if !resolved {
					blindSpots = append(blindSpots, BlindSpot{
						Description: fmt.Sprintf("Unresolved contradiction from '%s'", label),
						Severity:    "high",
					})
				}
			}
		}
	}

	return nodes, edges, influenceModes, criticalDepth, coverage, blindSpots, nil
}

func powFloat64(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

func (r *DependenceRepo) SynchronizeNode(ctx context.Context, nodeID string, propagateDepth int, resolveContradictions bool) (int, []map[string]any, []map[string]any, []DependenceNode, []DependenceEdge, error) {
	// Forward BFS
	query := `
		WITH RECURSIVE forward AS (
			SELECT e.source_id, e.target_id, e.edge_type::text, e.weight, 1 AS depth
			FROM edges e
			WHERE e.source_id = $1

			UNION

			SELECT e.source_id, e.target_id, e.edge_type::text, e.weight, f.depth + 1
			FROM edges e
			JOIN forward f ON e.source_id = f.target_id
			WHERE f.depth < $2
		)
		SELECT source_id, target_id, edge_type, weight, depth FROM forward
	`
	rows, err := r.pool.Query(ctx, query, nodeID, propagateDepth)
	if err != nil {
		return 0, nil, nil, nil, nil, err
	}
	defer rows.Close()

	edgeMap := make(map[string]DependenceEdge)
	nodeIDs := make(map[string]bool)
	nodeIDs[nodeID] = true

	for rows.Next() {
		var src, tgt, et string
		var weight float32
		var depth int
		if err := rows.Scan(&src, &tgt, &et, &weight, &depth); err != nil {
			continue
		}
		key := src + "->" + tgt
		edgeMap[key] = DependenceEdge{SourceID: src, TargetID: tgt, EdgeType: et, Weight: weight}
		nodeIDs[src] = true
		nodeIDs[tgt] = true
	}

	// Recompute confidence for affected nodes
	var confidenceUpdates []map[string]any
	for id := range nodeIDs {
		if id == nodeID {
			continue
		}
		var oldConf float32
		_ = r.pool.QueryRow(ctx, `
			SELECT (
				0.30 * LEAST(access_count/50.0, 1.0)
				+ 0.25 * LEAST((SELECT COUNT(*) FROM edges WHERE source_id = n.id OR target_id = n.id)/10.0, 1.0)
				+ 0.20 * LEAST(EXTRACT(DAY FROM now() - created_at)/90.0, 1.0)
				+ 0.15 * importance
			)
			FROM nodes n
			WHERE n.id = $1 AND n.valid_to IS NULL
		`, id).Scan(&oldConf)

		// New confidence = old + small boost from being connected to new node
		newConf := oldConf + 0.05
		if newConf > 1.0 {
			newConf = 1.0
		}

		confidenceUpdates = append(confidenceUpdates, map[string]any{
			"node_id":        id,
			"old_confidence": oldConf,
			"new_confidence": newConf,
		})
	}

	// Resolve contradictions
	var resolved []map[string]any
	if resolveContradictions {
		crows, err := r.pool.Query(ctx, `
			SELECT s.id, s.label
			FROM edges e
			JOIN nodes s ON e.source_id = s.id
			WHERE e.target_id = $1 AND e.edge_type = 'contradicts' AND s.valid_to IS NULL
		`, nodeID)
		if err == nil {
			defer crows.Close()
			for crows.Next() {
				var cid, clabel string
				if err := crows.Scan(&cid, &clabel); err == nil {
					resolved = append(resolved, map[string]any{
						"node_id":    cid,
						"label":      clabel,
						"resolution": "superseded_by_new_evidence",
					})
				}
			}
		}
	}

	var nodes []DependenceNode
	var edges []DependenceEdge
	if len(nodeIDs) > 0 {
		idList := make([]string, 0, len(nodeIDs))
		for id := range nodeIDs {
			idList = append(idList, id)
		}
		nodeRows, err := r.pool.Query(ctx, `
			SELECT id, label, node_type::text, namespace, importance
			FROM nodes WHERE id = ANY($1) AND valid_to IS NULL
		`, idList)
		if err == nil {
			defer nodeRows.Close()
			for nodeRows.Next() {
				var n DependenceNode
				if err := nodeRows.Scan(&n.ID, &n.Label, &n.NodeType, &n.Namespace, &n.Importance); err == nil {
					nodes = append(nodes, n)
				}
			}
		}
	}
	for _, e := range edgeMap {
		edges = append(edges, e)
	}

	return len(nodeIDs) - 1, confidenceUpdates, resolved, nodes, edges, nil
}

func (r *DependenceRepo) GetObservability(ctx context.Context, namespace string, seedNodeIDs []string) (int, int, float64, map[string]float64, error) {
	// Get total nodes in namespace
	var totalNodes int
	_ = r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL AND ($1 = '' OR namespace = $1)
	`, namespace).Scan(&totalNodes)

	if len(seedNodeIDs) == 0 || totalNodes == 0 {
		return 0, totalNodes, 0.0, map[string]float64{}, nil
	}

	// Forward BFS from all seeds simultaneously
	query := `
		WITH RECURSIVE forward AS (
			SELECT DISTINCT e.target_id AS node_id, 1 AS depth
			FROM edges e
			WHERE e.source_id = ANY($1)

			UNION

			SELECT DISTINCT e.target_id, f.depth + 1
			FROM edges e
			JOIN forward f ON e.source_id = f.node_id
			WHERE f.depth < 5
		)
		SELECT node_id FROM forward
	`
	rows, err := r.pool.Query(ctx, query, seedNodeIDs)
	if err != nil {
		return 0, totalNodes, 0.0, nil, err
	}
	defer rows.Close()

	observable := make(map[string]bool)
	for _, id := range seedNodeIDs {
		observable[id] = true
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			observable[id] = true
		}
	}

	// Coverage by node type
	typeCounts := make(map[string]int)
	observableCounts := make(map[string]int)

	nodeRows, err := r.pool.Query(ctx, `
		SELECT id, node_type::text FROM nodes
		WHERE valid_to IS NULL AND ($1 = '' OR namespace = $1)
	`, namespace)
	if err == nil {
		defer nodeRows.Close()
		for nodeRows.Next() {
			var id, nt string
			if err := nodeRows.Scan(&id, &nt); err == nil {
				typeCounts[nt]++
				if observable[id] {
					observableCounts[nt]++
				}
			}
		}
	}

	coverageByType := make(map[string]float64)
	for nt, total := range typeCounts {
		if total > 0 {
			coverageByType[nt] = float64(observableCounts[nt]) / float64(total)
		}
	}

	return len(observable), totalNodes, float64(len(observable)) / float64(totalNodes), coverageByType, nil
}

// DependenceExpand returns precursor nodes for a seed node, suitable for search expansion.
// It runs a shallow backward BFS and returns influence-ranked precursors.
func (r *DependenceRepo) DependenceExpand(ctx context.Context, seedID string, limit int) ([]models.SearchResult, error) {
	_, _, modes, _, _, _, err := r.GetDependenceGraph(ctx, seedID,
		[]string{"depends_on", "learned_from", "decided_by", "produced", "supports",
			"tested_by", "invalidated_by", "derived_from", "assumed",
			"superseded_by", "refined_by", "failed_due_to", "precondition_for"},
		2, // shallow: 2 hops max
		0.1,
	)
	if err != nil {
		return nil, err
	}
	if len(modes) == 0 {
		return nil, nil
	}
	if len(modes) > limit {
		modes = modes[:limit]
	}

	// Fetch full node details for each precursor
	ids := make([]string, len(modes))
	for i, m := range modes {
		ids[i] = m.NodeID
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, label, node_type::text, content, namespace
		FROM nodes
		WHERE id = ANY($1) AND valid_to IS NULL
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodeMap := make(map[string]models.SearchResult)
	for rows.Next() {
		var sr models.SearchResult
		if err := rows.Scan(&sr.NodeID, &sr.Label, &sr.NodeType, &sr.Content, &sr.Namespace); err == nil {
			nodeMap[sr.NodeID] = sr
		}
	}

	// Preserve influence score ordering
	var results []models.SearchResult
	for _, m := range modes {
		if sr, ok := nodeMap[m.NodeID]; ok {
			sr.RRFScore = float32(m.InfluenceScore)
			results = append(results, sr)
		}
	}
	return results, nil
}
