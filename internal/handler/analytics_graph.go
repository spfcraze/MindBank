package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GraphAnalyticsHandler computes topology metrics for the graph.
type GraphAnalyticsHandler struct {
	pool *pgxpool.Pool
}

// NewGraphAnalyticsHandler creates a new analytics handler.
func NewGraphAnalyticsHandler(pool *pgxpool.Pool) *GraphAnalyticsHandler {
	return &GraphAnalyticsHandler{pool: pool}
}

// GraphMetrics handles GET /api/v1/analytics/graph.
func (h *GraphAnalyticsHandler) GraphMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Basic counts
	var nodeCount, edgeCount int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`).Scan(&nodeCount)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM edges`).Scan(&edgeCount)

	// 2. Hub nodes (top 10 by degree)
	hubRows, err := h.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, n.namespace, COUNT(*) AS degree
		FROM nodes n
		JOIN edges e ON e.source_id = n.id OR e.target_id = n.id
		WHERE n.valid_to IS NULL
		GROUP BY n.id
		ORDER BY degree DESC
		LIMIT 10
	`)
	if err != nil {
		respondError(w, 500, "hub query failed")
		return
	}
	defer hubRows.Close()

	type hubNode struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		NodeType  string `json:"node_type"`
		Namespace string `json:"namespace"`
		Degree    int    `json:"degree"`
	}
	var hubs []hubNode
	for hubRows.Next() {
		var h hubNode
		if err := hubRows.Scan(&h.ID, &h.Label, &h.NodeType, &h.Namespace, &h.Degree); err != nil {
			continue
		}
		hubs = append(hubs, h)
	}

	// 3. Fetch all edges for graph algorithms
	edgeRows, err := h.pool.Query(ctx, `SELECT source_id, target_id FROM edges`)
	if err != nil {
		respondError(w, 500, "edge query failed")
		return
	}
	defer edgeRows.Close()

	adj := make(map[string][]string)
	nodeSet := make(map[string]bool)
	for edgeRows.Next() {
		var src, tgt string
		if err := edgeRows.Scan(&src, &tgt); err != nil {
			continue
		}
		adj[src] = append(adj[src], tgt)
		adj[tgt] = append(adj[tgt], src)
		nodeSet[src] = true
		nodeSet[tgt] = true
	}

	// 4. Connected components
	components := findConnectedComponents(adj, nodeSet)

	// 5. Articulation points (bridge nodes) - Tarjan's algorithm
	bridges := h.findArticulationPoints(adj, nodeSet, ctx)

	// 6. Density
	density := 0.0
	if nodeCount > 1 {
		density = float64(edgeCount*2) / float64(nodeCount*(nodeCount-1))
	}

	// 7. Avg connections
	avgConnections := 0.0
	if nodeCount > 0 {
		avgConnections = float64(edgeCount*2) / float64(nodeCount)
	}

	respondJSON(w, 200, map[string]any{
		"node_count":        nodeCount,
		"edge_count":        edgeCount,
		"density":           round3(density),
		"avg_connections":   round3(avgConnections),
		"components":        len(components),
		"hub_nodes":         hubs,
		"bridge_nodes":      bridges,
		"largest_component": largestComponentSize(components),
	})
}

func findConnectedComponents(adj map[string][]string, nodeSet map[string]bool) [][]string {
	visited := make(map[string]bool)
	var components [][]string
	for node := range nodeSet {
		if visited[node] {
			continue
		}
		comp := []string{}
		queue := []string{node}
		visited[node] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			for _, nb := range adj[cur] {
				if !visited[nb] {
					visited[nb] = true
					queue = append(queue, nb)
				}
			}
		}
		components = append(components, comp)
	}
	return components
}

func largestComponentSize(components [][]string) int {
	maxSize := 0
	for _, c := range components {
		if len(c) > maxSize {
			maxSize = len(c)
		}
	}
	return maxSize
}

func (h *GraphAnalyticsHandler) findArticulationPoints(adj map[string][]string, nodeSet map[string]bool, ctx context.Context) []map[string]any {
	// Tarjan's articulation point algorithm
	if len(nodeSet) == 0 {
		return nil
	}

	disc := make(map[string]int)
	low := make(map[string]int)
	visited := make(map[string]bool)
	ap := make(map[string]bool)
	time := 0

	var dfs func(string, string)
	dfs = func(u, parent string) {
		children := 0
		time++
		disc[u] = time
		low[u] = time
		visited[u] = true

		for _, v := range adj[u] {
			if !visited[v] {
				children++
				dfs(v, u)
				if low[v] < low[u] {
					low[u] = low[v]
				}
				if parent == "" && children > 1 {
					ap[u] = true
				}
				if parent != "" && low[v] >= disc[u] {
					ap[u] = true
				}
			} else if v != parent {
				if disc[v] < low[u] {
					low[u] = disc[v]
				}
			}
		}
	}

	for node := range nodeSet {
		if !visited[node] {
			dfs(node, "")
		}
	}

	// Resolve IDs to labels
	var result []map[string]any
	if len(ap) > 0 {
		ids := make([]string, 0, len(ap))
		for id := range ap {
			ids = append(ids, id)
		}
		// Fetch labels in batch
		labelMap := make(map[string]string)
		nsMap := make(map[string]string)
		rows, err := h.pool.Query(ctx, `
			SELECT id, label, namespace FROM nodes
			WHERE id = ANY($1) AND valid_to IS NULL
		`, ids)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, label, ns string
				if err := rows.Scan(&id, &label, &ns); err == nil {
					labelMap[id] = label
					nsMap[id] = ns
				}
			}
		}
		for id := range ap {
			result = append(result, map[string]any{
				"id":        id,
				"label":     labelMap[id],
				"namespace": nsMap[id],
			})
		}
	}

	return result
}

func round3(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 3, 64), 64)
	return f
}
