// Package quality provides graph health metrics for MindBank.
// Inspired by gbrain's eval framework.
package quality

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GraphMetrics represents the health of the knowledge graph.
type GraphMetrics struct {
	TotalNodes             int     `json:"total_nodes"`
	TotalEdges             int     `json:"total_edges"`
	OrphanNodes            int     `json:"orphan_nodes"`
	OrphanPercent          float64 `json:"orphan_percent"`
	DisconnectedComponents int     `json:"disconnected_components"`
	LargestComponent       int     `json:"largest_component"`
	LargestComponentPct    float64 `json:"largest_component_pct"`
	EdgeDensity            float64 `json:"edge_density"`
	AvgDegree              float64 `json:"avg_degree"`
	DuplicateLabels        int     `json:"duplicate_labels"`
	DuplicateInstances     int     `json:"duplicate_instances"`
	DuplicatePercent       float64 `json:"duplicate_percent"`
	TopicCoverage          int     `json:"topic_coverage"`
	TopicCoveragePct       float64 `json:"topic_coverage_pct"`
	UncategorizedNodes     int     `json:"uncategorized_nodes"`
	UncategorizedPercent   float64 `json:"uncategorized_percent"`
	QualityScore           float64 `json:"quality_score"`
	Grade                  string  `json:"grade"`
}

// ComputeMetrics calculates graph health metrics from the database.
func ComputeMetrics(ctx context.Context, pool *pgxpool.Pool) (*GraphMetrics, error) {
	m := &GraphMetrics{}

	// Total nodes (current)
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`).Scan(&m.TotalNodes)
	if err != nil {
		return nil, fmt.Errorf("count nodes: %w", err)
	}

	// Total edges
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM edges`).Scan(&m.TotalEdges)
	if err != nil {
		return nil, fmt.Errorf("count edges: %w", err)
	}

	// Orphan nodes (no edges) — use server-computed edge_count for accuracy
	var orphanCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT n.id FROM nodes n
			LEFT JOIN (
				SELECT source_id AS node_id FROM edges
				UNION ALL
				SELECT target_id FROM edges
			) e ON e.node_id = n.id
			WHERE n.valid_to IS NULL
			GROUP BY n.id
			HAVING COUNT(e.node_id) = 0
		) orphans
	`).Scan(&orphanCount)
	if err != nil {
		return nil, fmt.Errorf("count orphans: %w", err)
	}
	m.OrphanNodes = orphanCount
	if m.TotalNodes > 0 {
		m.OrphanPercent = float64(orphanCount) / float64(m.TotalNodes) * 100
	}

	// Connected components via BFS (simplified: count distinct nodes reachable)
	// For large graphs, this is expensive. Use approximation: nodes - edges + components = 1 (for connected)
	// Components ≈ nodes - edges (when edges < nodes)
	if m.TotalNodes > 0 {
		m.DisconnectedComponents = m.TotalNodes - m.TotalEdges
		if m.DisconnectedComponents < 1 {
			m.DisconnectedComponents = 1
		}
	}

	// Largest component (approximation: total - orphans)
	m.LargestComponent = m.TotalNodes - m.OrphanNodes
	if m.TotalNodes > 0 {
		m.LargestComponentPct = float64(m.LargestComponent) / float64(m.TotalNodes) * 100
	}

	// Edge density
	possible := float64(m.TotalNodes) * float64(m.TotalNodes-1) / 2
	if possible > 0 {
		m.EdgeDensity = float64(m.TotalEdges) / possible
	}

	// Average degree
	if m.TotalNodes > 0 {
		m.AvgDegree = float64(m.TotalEdges*2) / float64(m.TotalNodes)
	}

	// Duplicate labels
	var dupLabels, dupInstances int
	rows, err := pool.Query(ctx, `
		SELECT label, COUNT(*) as cnt
		FROM nodes
		WHERE valid_to IS NULL
		GROUP BY label
		HAVING COUNT(*) > 1
	`)
	if err != nil {
		return nil, fmt.Errorf("find duplicates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var label string
		var count int
		if err := rows.Scan(&label, &count); err != nil {
			continue
		}
		dupLabels++
		dupInstances += count - 1
	}
	m.DuplicateLabels = dupLabels
	m.DuplicateInstances = dupInstances
	if m.TotalNodes > 0 {
		m.DuplicatePercent = float64(dupInstances) / float64(m.TotalNodes) * 100
	}

	// Topic coverage (from metadata or content analysis)
	// For now, count distinct topics if column exists, else 0
	var topicCount int
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT topic) FROM nodes
		WHERE valid_to IS NULL AND topic IS NOT NULL AND topic != ''
	`).Scan(&topicCount)
	m.TopicCoverage = topicCount
	m.TopicCoveragePct = float64(topicCount) / 10.0 * 100 // 10 = total possible topics
	if m.TopicCoveragePct > 100 {
		m.TopicCoveragePct = 100
	}

	// Uncategorized nodes
	var uncategorized int
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM nodes
		WHERE valid_to IS NULL AND (topic IS NULL OR topic = '')
	`).Scan(&uncategorized)
	m.UncategorizedNodes = uncategorized
	if m.TotalNodes > 0 {
		m.UncategorizedPercent = float64(uncategorized) / float64(m.TotalNodes) * 100
	}

	// Quality score (0-100)
	m.QualityScore = calculateScore(m)
	m.Grade = gradeFromScore(m.QualityScore)

	return m, nil
}

func calculateScore(m *GraphMetrics) float64 {
	if m.TotalNodes == 0 {
		return 0
	}

	// Component weights (same as frontend DQA)
	// Priority: empty content > duplicates > orphans > disconnected > uncontained > low importance
	// Frontend uses: (clean / total) * 100 where clean = total - uniqueIssues
	// Map backend metrics to match frontend categories
	orphanScore := 25.0 * (1.0 - m.OrphanPercent/100.0)
	duplicateScore := 25.0 * (1.0 - m.DuplicatePercent/100.0)
	connectivityScore := 25.0 * (m.LargestComponentPct / 100.0)
	densityScore := 25.0 * math.Min(m.EdgeDensity*1000, 1.0)

	score := orphanScore + duplicateScore + connectivityScore + densityScore
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func gradeFromScore(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}
