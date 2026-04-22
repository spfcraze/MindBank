# Observer Perspective — Domain of Dependence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: skill_view('subagent-driven-development') (recommended) or skill_view('executing-plans') to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `/analyze/dependence`, `/analyze/synchronize`, and `/analyze/observability` endpoints that implement turbulence-inspired observer-perspective graph analytics.

**Architecture:** Backward BFS traversal from a seed node/query to compute causal support sets, forward BFS for synchronization propagation, and coverage metrics for observability. All logic lives in new handler files + one repository file. No schema changes to existing tables.

**Tech Stack:** Go 1.22+, pgx/v5, chi router, existing MindBank node/edge schema.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/repository/dependence.go` | DB queries for backward/forward traversal, influence scoring, observability metrics |
| `internal/handler/analyze_dependence.go` | HTTP handler for `/analyze/dependence` |
| `internal/handler/analyze_synchronize.go` | HTTP handler for `/analyze/synchronize` |
| `internal/handler/analyze_observability.go` | HTTP handler for `/analyze/observability` |
| `internal/handler/router.go` | Register new routes |
| `tests/integration/dependence_test.go` | Integration tests with synthetic graph |
| `web/dashboard/observer-tab.js` | Frontend Observer/Causal Trace tab |
| `web/dashboard/index.html` | Add Observer tab to nav |

---

## Task 1: Dependence Repository Layer

**Files:**
- Create: `internal/repository/dependence.go`
- Test: `tests/integration/dependence_test.go`

- [ ] **Step 1: Write the repository interface and struct**

```go
package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DependenceRepo struct {
	pool *pgxpool.Pool
}

func NewDependenceRepo(pool *pgxpool.Pool) *DependenceRepo {
	return &DependenceRepo{pool: pool}
}

type DependenceNode struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	NodeType string  `json:"node_type"`
	Namespace string `json:"namespace"`
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
```

- [ ] **Step 2: Write `GetDependenceGraph` method**

```go
func (r *DependenceRepo) GetDependenceGraph(ctx context.Context, seedID string, edgeTypes []string, maxDepth int, minWeight float32) ([]DependenceNode, []DependenceEdge, []InfluenceMode, int, float64, []BlindSpot, error) {
	// Validate edge_types to prevent injection
	validEdgeTypes := map[string]bool{
		"contains": true, "relates_to": true, "depends_on": true, "decided_by": true,
		"participated_in": true, "produced": true, "contradicts": true, "supports": true,
		"temporal_next": true, "mentions": true, "learned_from": true,
	}
	var cleanTypes []string
	for _, et := range edgeTypes {
		if validEdgeTypes[et] {
			cleanTypes = append(cleanTypes, et)
		}
	}
	if len(cleanTypes) == 0 {
		cleanTypes = []string{"depends_on", "learned_from", "decided_by", "produced", "supports"}
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
					SELECT EXISTS(SELECT 1 FROM edges WHERE target_id = $1 AND edge_type = 'supports' AND source_id = $2)
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
```

- [ ] **Step 3: Write `SynchronizeNode` method**

```go
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
			SELECT (0.30 * LEAST(access_count/50.0, 1.0) + 0.25 * LEAST(edge_count/10.0, 1.0) + 0.20 * LEAST(EXTRACT(DAY FROM now() - created_at)/90.0, 1.0) + 0.15 * importance)
			FROM nodes
			WHERE id = $1 AND valid_to IS NULL
		`, id).Scan(&oldConf)

		// New confidence = old + small boost from being connected to new node
		newConf := oldConf + 0.05
		if newConf > 1.0 {
			newConf = 1.0
		}

		confidenceUpdates = append(confidenceUpdates, map[string]any{
			"node_id":         id,
			"old_confidence":  oldConf,
			"new_confidence":  newConf,
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
```

- [ ] **Step 4: Write `GetObservability` method**

```go
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
```

- [ ] **Step 5: Commit**

```bash
git add internal/repository/dependence.go
git commit -m "feat: dependence repository layer for observer perspective"
```

---

## Task 2: `/analyze/dependence` Handler

**Files:**
- Create: `internal/handler/analyze_dependence.go`

- [ ] **Step 1: Write handler**

```go
package handler

import (
	"encoding/json"
	"net/http"

	"mindbank/internal/repository"
)

func (h *AnalyzeHandler) Dependence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID     string   `json:"node_id"`
		Query      string   `json:"query"`
		Namespace  string   `json:"namespace"`
		MaxDepth   int      `json:"max_depth"`
		EdgeTypes  []string `json:"edge_types"`
		MinWeight  float32  `json:"min_weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.MaxDepth <= 0 || req.MaxDepth > 5 {
		req.MaxDepth = 3
	}
	if req.MinWeight <= 0 {
		req.MinWeight = 0.1
	}

	ctx := r.Context()
	repo := repository.NewDependenceRepo(h.pool)

	seedID := req.NodeID
	if seedID == "" && req.Query != "" {
		// Run hybrid search to find seed
		searchRepo := repository.NewSearchRepo(h.pool)
		results, err := searchRepo.HybridSearch(ctx, req.Query, req.Namespace, 1)
		if err != nil || len(results) == 0 {
			respondError(w, 404, "no seed found for query")
			return
		}
		seedID = results[0].ID
	}
	if seedID == "" {
		respondError(w, 400, "node_id or query required")
		return
	}

	// Verify seed exists
	var seedLabel, seedType, seedNS string
	row := h.pool.QueryRow(ctx, `SELECT label, node_type::text, namespace FROM nodes WHERE id = $1 AND valid_to IS NULL`, seedID)
	if err := row.Scan(&seedLabel, &seedType, &seedNS); err != nil {
		respondError(w, 404, "seed node not found")
		return
	}

	nodes, edges, influenceModes, criticalDepth, coverage, blindSpots, err := repo.GetDependenceGraph(ctx, seedID, req.EdgeTypes, req.MaxDepth, req.MinWeight)
	if err != nil {
		respondError(w, 500, "dependence computation failed")
		return
	}

	respondJSON(w, 200, map[string]any{
		"seed": map[string]string{
			"id":        seedID,
			"label":     seedLabel,
			"node_type": seedType,
			"namespace": seedNS,
		},
		"dependence_graph": map[string]any{
			"nodes": nodes,
			"edges": edges,
		},
		"critical_depth":   criticalDepth,
		"coverage":         coverage,
		"influence_modes":  influenceModes,
		"blind_spots":      blindSpots,
		"max_depth_used":   req.MaxDepth,
		"min_weight_used":  req.MinWeight,
	})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/handler/analyze_dependence.go
git commit -m "feat: /analyze/dependence endpoint"
```

---

## Task 3: `/analyze/synchronize` Handler

**Files:**
- Create: `internal/handler/analyze_synchronize.go`

- [ ] **Step 1: Write handler**

```go
package handler

import (
	"encoding/json"
	"net/http"

	"mindbank/internal/repository"
)

func (h *AnalyzeHandler) Synchronize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID                 string `json:"node_id"`
		Namespace              string `json:"namespace"`
		PropagateDepth         int    `json:"propagate_depth"`
		ResolveContradictions  bool   `json:"resolve_contradictions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.NodeID == "" {
		respondError(w, 400, "node_id required")
		return
	}
	if req.PropagateDepth <= 0 || req.PropagateDepth > 5 {
		req.PropagateDepth = 3
	}

	ctx := r.Context()
	repo := repository.NewDependenceRepo(h.pool)

	affected, confUpdates, resolved, nodes, edges, err := repo.SynchronizeNode(ctx, req.NodeID, req.PropagateDepth, req.ResolveContradictions)
	if err != nil {
		respondError(w, 500, "synchronization failed")
		return
	}

	respondJSON(w, 200, map[string]any{
		"node_id":                  req.NodeID,
		"affected_nodes":           affected,
		"confidence_updates":       confUpdates,
		"resolved_contradictions":  resolved,
		"propagation_graph": map[string]any{
			"nodes": nodes,
			"edges": edges,
		},
	})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/handler/analyze_synchronize.go
git commit -m "feat: /analyze/synchronize endpoint"
```

---

## Task 4: `/analyze/observability` Handler

**Files:**
- Create: `internal/handler/analyze_observability.go`

- [ ] **Step 1: Write handler**

```go
package handler

import (
	"net/http"
	"strings"

	"mindbank/internal/repository"
)

func (h *AnalyzeHandler) Observability(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	seedParam := r.URL.Query().Get("seed_node_ids")

	var seedIDs []string
	if seedParam != "" {
		seedIDs = strings.Split(seedParam, ",")
	}

	ctx := r.Context()
	repo := repository.NewDependenceRepo(h.pool)

	observable, total, ratio, coverageByType, err := repo.GetObservability(ctx, ns, seedIDs)
	if err != nil {
		respondError(w, 500, "observability computation failed")
		return
	}

	respondJSON(w, 200, map[string]any{
		"namespace":         ns,
		"seed_count":        len(seedIDs),
		"total_nodes":       total,
		"observable_nodes":  observable,
		"observability_ratio": ratio,
		"coverage_by_type":  coverageByType,
	})
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/handler/analyze_observability.go
git commit -m "feat: /analyze/observability endpoint"
```

---

## Task 5: Wire Routes

**Files:**
- Modify: `internal/handler/router.go`

- [ ] **Step 1: Add routes inside `/analyze` group**

Find the existing `/analyze` route group and add three new lines:

```go
r.Post("/dependence", ah.Dependence)
r.Post("/synchronize", ah.Synchronize)
r.Get("/observability", ah.Observability)
```

Place them after `r.Get("/confidence", ah.Confidence)`.

- [ ] **Step 2: Commit**

```bash
git add internal/handler/router.go
git commit -m "feat: wire observer perspective routes"
```

---

## Task 6: Integration Tests

**Files:**
- Create: `tests/integration/dependence_test.go`

- [ ] **Step 1: Write test setup**

```go
package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/repository"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	var err error
	testPool, err = pgxpool.New(context.Background(), os.Getenv("MB_DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	code := m.Run()
	testPool.Close()
	os.Exit(code)
}

func createTestNode(t *testing.T, pool *pgxpool.Pool, label, nodeType, ns string) string {
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance)
		VALUES ('test', $1, $2, $3, 'test content', 'test summary', 0.5)
		RETURNING id
	`, ns, label, nodeType).Scan(&id)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	return id
}

func createTestEdge(t *testing.T, pool *pgxpool.Pool, src, tgt, edgeType string, weight float32) {
	_, err := pool.Exec(context.Background(), `
		INSERT INTO edges (source_id, target_id, edge_type, weight)
		VALUES ($1, $2, $3, $4)
	`, src, tgt, edgeType, weight)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
}

func cleanupTestData(t *testing.T, pool *pgxpool.Pool, ids []string) {
	if len(ids) == 0 {
		return
	}
	_, _ = pool.Exec(context.Background(), `DELETE FROM edges WHERE source_id = ANY($1) OR target_id = ANY($1)`, ids)
	_, _ = pool.Exec(context.Background(), `DELETE FROM node_embeddings WHERE node_id = ANY($1)`, ids)
	_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE id = ANY($1)`, ids)
}
```

- [ ] **Step 2: Write `TestGetDependenceGraph`**

```go
func TestGetDependenceGraph(t *testing.T) {
	repo := repository.NewDependenceRepo(testPool)
	ctx := context.Background()

	ns := "test-dependence"
	// A depends_on B, B depends_on C
	nodeA := createTestNode(t, testPool, "Node A", "decision", ns)
	nodeB := createTestNode(t, testPool, "Node B", "fact", ns)
	nodeC := createTestNode(t, testPool, "Node C", "fact", ns)
	createTestEdge(t, testPool, nodeB, nodeA, "depends_on", 1.0)
	createTestEdge(t, testPool, nodeC, nodeB, "depends_on", 0.8)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC})

	nodes, edges, modes, criticalDepth, coverage, blindSpots, err := repo.GetDependenceGraph(ctx, nodeA, []string{"depends_on"}, 3, 0.1)
	if err != nil {
		t.Fatalf("GetDependenceGraph: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
	if criticalDepth != 2 {
		t.Errorf("expected critical_depth=2, got %d", criticalDepth)
	}
	if coverage != 1.0 {
		t.Errorf("expected coverage=1.0, got %f", coverage)
	}
	if len(blindSpots) != 0 {
		t.Errorf("expected 0 blind spots, got %d", len(blindSpots))
	}
	if len(modes) != 2 {
		t.Errorf("expected 2 influence modes, got %d", len(modes))
	}
	// B should have higher influence than C (closer to seed)
	if modes[0].NodeID != nodeB {
		t.Errorf("expected top influence to be node B (%s), got %s", nodeB, modes[0].NodeID)
	}
}
```

- [ ] **Step 3: Write `TestSynchronizeNode`**

```go
func TestSynchronizeNode(t *testing.T) {
	repo := repository.NewDependenceRepo(testPool)
	ctx := context.Background()

	ns := "test-sync"
	nodeA := createTestNode(t, testPool, "Node A", "fact", ns)
	nodeB := createTestNode(t, testPool, "Node B", "decision", ns)
	nodeC := createTestNode(t, testPool, "Node C", "advice", ns)
	createTestEdge(t, testPool, nodeA, nodeB, "supports", 1.0)
	createTestEdge(t, testPool, nodeB, nodeC, "depends_on", 0.9)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC})

	affected, confUpdates, resolved, nodes, edges, err := repo.SynchronizeNode(ctx, nodeA, 3, false)
	if err != nil {
		t.Fatalf("SynchronizeNode: %v", err)
	}

	if affected != 2 {
		t.Errorf("expected 2 affected nodes, got %d", affected)
	}
	if len(confUpdates) != 2 {
		t.Errorf("expected 2 confidence updates, got %d", len(confUpdates))
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved contradictions, got %d", len(resolved))
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes in propagation graph, got %d", len(nodes))
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges in propagation graph, got %d", len(edges))
	}
}
```

- [ ] **Step 4: Write `TestGetObservability`**

```go
func TestGetObservability(t *testing.T) {
	repo := repository.NewDependenceRepo(testPool)
	ctx := context.Background()

	ns := "test-obs"
	nodeA := createTestNode(t, testPool, "Node A", "fact", ns)
	nodeB := createTestNode(t, testPool, "Node B", "decision", ns)
	nodeC := createTestNode(t, testPool, "Node C", "advice", ns)
	nodeD := createTestNode(t, testPool, "Node D", "fact", ns) // isolated
	createTestEdge(t, testPool, nodeA, nodeB, "supports", 1.0)
	createTestEdge(t, testPool, nodeB, nodeC, "depends_on", 0.9)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC, nodeD})

	observable, total, ratio, coverage, err := repo.GetObservability(ctx, ns, []string{nodeA})
	if err != nil {
		t.Fatalf("GetObservability: %v", err)
	}

	if total != 4 {
		t.Errorf("expected total=4, got %d", total)
	}
	if observable != 3 {
		t.Errorf("expected observable=3 (A,B,C), got %d", observable)
	}
	expectedRatio := 3.0 / 4.0
	if ratio != expectedRatio {
		t.Errorf("expected ratio=%f, got %f", expectedRatio, ratio)
	}
	if coverage["decision"] != 1.0 {
		t.Errorf("expected decision coverage=1.0, got %f", coverage["decision"])
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd /home/rat/mindbank
MB_DATABASE_URL="postgres://mindbank:mindbank@localhost:5432/mindbank?sslmode=disable" go test ./tests/integration/... -v
```

Expected: 3 PASS

- [ ] **Step 6: Commit**

```bash
git add tests/integration/dependence_test.go
git commit -m "test: integration tests for observer perspective"
```

---

## Task 7: Frontend Observer Tab

**Files:**
- Modify: `web/dashboard/index.html` (add nav tab)
- Create: `web/dashboard/observer-tab.js`

- [ ] **Step 1: Add Observer tab to navigation**

In `index.html`, find the tab navigation and add:

```html
<button class="tab-btn" data-tab="observer">Observer</button>
```

Add the panel:

```html
<div id="observer-panel" class="tab-panel">
  <h2>Causal Trace</h2>
  <div id="observer-controls">
    <input type="text" id="observer-seed" placeholder="Node ID or search query...">
    <button id="observer-trace-btn">Trace Dependence</button>
  </div>
  <div id="observer-stats"></div>
  <div id="observer-graph" style="width:100%;height:500px;"></div>
  <div id="observer-blindspots"></div>
</div>
```

- [ ] **Step 2: Create `observer-tab.js`**

```javascript
// observer-tab.js
class ObserverTab {
  constructor() {
    this.container = document.getElementById('observer-graph');
    this.statsEl = document.getElementById('observer-stats');
    this.blindspotsEl = document.getElementById('observer-blindspots');
    document.getElementById('observer-trace-btn').addEventListener('click', () => this.trace());
  }

  async trace() {
    const seed = document.getElementById('observer-seed').value.trim();
    if (!seed) return;

    const isUUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(seed);
    const body = isUUID ? { node_id: seed, max_depth: 3 } : { query: seed, max_depth: 3 };

    const res = await fetch('/api/v1/analyze/dependence', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = await res.json();
    if (!res.ok) {
      this.statsEl.innerHTML = `<div class="error">Error: ${data.error || 'unknown'}</div>`;
      return;
    }

    this.renderStats(data);
    this.renderGraph(data.dependence_graph);
    this.renderBlindSpots(data.blind_spots);
  }

  renderStats(data) {
    this.statsEl.innerHTML = `
      <div class="stats-grid">
        <div>Seed: <strong>${data.seed.label}</strong> (${data.seed.node_type})</div>
        <div>Critical Depth: <strong>${data.critical_depth}</strong></div>
        <div>Coverage: <strong>${(data.coverage * 100).toFixed(1)}%</strong></div>
        <div>Nodes: ${data.dependence_graph.nodes.length} | Edges: ${data.dependence_graph.edges.length}</div>
      </div>
    `;
  }

  renderGraph(graph) {
    this.container.innerHTML = '';
    const canvas = document.createElement('canvas');
    canvas.width = this.container.clientWidth;
    canvas.height = this.container.clientHeight;
    this.container.appendChild(canvas);
    const ctx = canvas.getContext('2d');

    // Simple force-directed layout
    const nodes = graph.nodes.map(n => ({ ...n, x: Math.random() * canvas.width, y: Math.random() * canvas.height }));
    const nodeMap = {};
    nodes.forEach(n => nodeMap[n.id] = n);

    // Run a few iterations
    for (let iter = 0; iter < 100; iter++) {
      // Repulsion
      for (let i = 0; i < nodes.length; i++) {
        for (let j = i + 1; j < nodes.length; j++) {
          const dx = nodes[j].x - nodes[i].x;
          const dy = nodes[j].y - nodes[i].y;
          const dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const force = 500 / (dist * dist);
          const fx = (dx / dist) * force;
          const fy = (dy / dist) * force;
          nodes[i].x -= fx; nodes[i].y -= fy;
          nodes[j].x += fx; nodes[j].y += fy;
        }
      }
      // Attraction along edges
      graph.edges.forEach(e => {
        const s = nodeMap[e.source_id];
        const t = nodeMap[e.target_id];
        if (!s || !t) return;
        const dx = t.x - s.x;
        const dy = t.y - s.y;
        const dist = Math.sqrt(dx * dx + dy * dy) || 1;
        const force = dist * 0.01;
        const fx = (dx / dist) * force;
        const fy = (dy / dist) * force;
        s.x += fx; s.y += fy;
        t.x -= fx; t.y -= fy;
      });
      // Center gravity
      nodes.forEach(n => {
        n.x += (canvas.width / 2 - n.x) * 0.01;
        n.y += (canvas.height / 2 - n.y) * 0.01;
      });
    }

    // Draw edges
    ctx.strokeStyle = '#888';
    ctx.lineWidth = 1;
    graph.edges.forEach(e => {
      const s = nodeMap[e.source_id];
      const t = nodeMap[e.target_id];
      if (!s || !t) return;
      ctx.beginPath();
      ctx.moveTo(s.x, s.y);
      ctx.lineTo(t.x, t.y);
      ctx.stroke();
    });

    // Draw nodes
    nodes.forEach(n => {
      ctx.beginPath();
      ctx.arc(n.x, n.y, 8, 0, Math.PI * 2);
      ctx.fillStyle = n.id === graph.nodes[0]?.id ? '#e74c3c' : '#3498db';
      ctx.fill();
      ctx.fillStyle = '#333';
      ctx.font = '12px sans-serif';
      ctx.fillText(n.label, n.x + 10, n.y + 4);
    });
  }

  renderBlindSpots(spots) {
    if (!spots || spots.length === 0) {
      this.blindspotsEl.innerHTML = '<div class="success">✓ No blind spots detected</div>';
      return;
    }
    this.blindspotsEl.innerHTML = '<h3>Blind Spots</h3>' + spots.map(s =>
      `<div class="blindspot blindspot-${s.severity}">${s.description}</div>`
    ).join('');
  }
}

window.observerTab = new ObserverTab();
```

- [ ] **Step 3: Wire JS in index.html**

Add `<script src="observer-tab.js"></script>` before the closing `</body>`.

- [ ] **Step 4: Commit**

```bash
git add web/dashboard/index.html web/dashboard/observer-tab.js
git commit -m "feat: frontend Observer/Causal Trace tab"
```

---

## Task 8: Darwin Benchmark

**Files:**
- Create: `scripts/darwin-benchmark.js`

- [ ] **Step 1: Write benchmark script**

```javascript
// scripts/darwin-benchmark.js
const QUERIES = [
  "database configuration",
  "authentication strategy",
  "deployment workflow",
  "graph visualization bug",
  "rate limiting"
];

async function benchmark(name, useDependence) {
  let totalRelevance = 0;
  for (const q of QUERIES) {
    const body = { query: q, namespace: "", limit: 5 };
    if (useDependence) {
      // Run dependence-aware search: get seed, then expand
      const depRes = await fetch('http://localhost:8095/api/v1/analyze/dependence', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: q, max_depth: 2 })
      });
      const dep = await depRes.json();
      if (dep.dependence_graph) {
        // Score = count of relevant node types in dependence graph
        const relevantTypes = ['fact', 'decision', 'advice', 'concept'];
        const relevant = dep.dependence_graph.nodes.filter(n => relevantTypes.includes(n.node_type));
        totalRelevance += relevant.length / dep.dependence_graph.nodes.length;
      }
    } else {
      // Baseline: hybrid search only
      const res = await fetch('http://localhost:8095/api/v1/search/hybrid', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });
      const data = await res.json();
      if (data.results) {
        const relevantTypes = ['fact', 'decision', 'advice', 'concept'];
        const relevant = data.results.filter(r => relevantTypes.includes(r.node_type));
        totalRelevance += relevant.length / data.results.length;
      }
    }
  }
  const mean = totalRelevance / QUERIES.length;
  console.log(`${name}: mean relevance = ${mean.toFixed(3)}`);
  return mean;
}

async function main() {
  console.log('=== Darwin Benchmark: Observer Perspective ===');
  const baseline = await benchmark('Baseline (hybrid search)', false);
  const withDependence = await benchmark('With dependence expansion', true);
  const improvement = ((withDependence - baseline) / baseline) * 100;
  console.log(`Improvement: ${improvement.toFixed(1)}%`);
  if (improvement >= 10) {
    console.log('✓ PASS: meets 10% improvement threshold');
    process.exit(0);
  } else {
    console.log('✗ FAIL: below 10% improvement threshold');
    process.exit(1);
  }
}

main().catch(e => { console.error(e); process.exit(1); });
```

- [ ] **Step 2: Run benchmark**

```bash
# Ensure server is running and has data
cd /home/rat/mindbank
node scripts/darwin-benchmark.js
```

- [ ] **Step 3: Commit**

```bash
git add scripts/darwin-benchmark.js
git commit -m "test: Darwin benchmark for observer perspective"
```

---

## Spec Coverage Check

| Spec Requirement | Task |
|---|---|
| `/analyze/dependence` endpoint | Task 2 |
| `/analyze/synchronize` endpoint | Task 3 |
| `/analyze/observability` endpoint | Task 4 |
| Repository layer with BFS traversal | Task 1 |
| Route wiring | Task 5 |
| Integration tests | Task 6 |
| Frontend tab | Task 7 |
| Darwin benchmark | Task 8 |

**No gaps. All requirements covered.**
