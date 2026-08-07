// Package digest provides periodic memory summary reports for MindBank.
// Inspired by gbrain's briefing skill.
package digest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Digest represents a periodic summary of knowledge graph activity.
type Digest struct {
	Period          string            `json:"period"`
	GeneratedAt     time.Time         `json:"generated_at"`
	NewNodes        int               `json:"new_nodes"`
	NewEdges        int               `json:"new_edges"`
	TopTopics       []TopicSummary    `json:"top_topics"`
	HotNodes        []NodeSummary     `json:"hot_nodes"`
	OrphanCount     int               `json:"orphan_count"`
	QualityScore    float64           `json:"quality_score"`
	Recommendations []string          `json:"recommendations"`
}

// TopicSummary represents a topic with node count.
type TopicSummary struct {
	Topic     string `json:"topic"`
	NodeCount int    `json:"node_count"`
}

// NodeSummary represents a node in the digest.
type NodeSummary struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	NodeType  string    `json:"node_type"`
	Topic     string    `json:"topic"`
	CreatedAt time.Time `json:"created_at"`
}

// Generator creates memory digests.
type Generator struct {
	pool *pgxpool.Pool
}

// NewGenerator creates a new digest generator.
func NewGenerator(pool *pgxpool.Pool) *Generator {
	return &Generator{pool: pool}
}

// Generate creates a digest for the specified time period.
func (g *Generator) Generate(ctx context.Context, period string) (*Digest, error) {
	var since time.Time
	now := time.Now().UTC()

	switch period {
	case "daily":
		since = now.Add(-24 * time.Hour)
	case "weekly":
		since = now.Add(-7 * 24 * time.Hour)
	case "monthly":
		since = now.Add(-30 * 24 * time.Hour)
	default:
		since = now.Add(-24 * time.Hour)
		period = "daily"
	}

	d := &Digest{
		Period:      period,
		GeneratedAt: now,
	}

	// Count new nodes in period
	err := g.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM nodes
		WHERE valid_to IS NULL AND created_at >= $1
	`, since).Scan(&d.NewNodes)
	if err != nil {
		return nil, fmt.Errorf("count new nodes: %w", err)
	}

	// Count new edges in period
	err = g.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM edges
		WHERE created_at >= $1
	`, since).Scan(&d.NewEdges)
	if err != nil {
		return nil, fmt.Errorf("count new edges: %w", err)
	}

	// Top topics by node count (from metadata or content)
	rows, err := g.pool.Query(ctx, `
		SELECT COALESCE(topic, 'uncategorized') as topic, COUNT(*) as cnt
		FROM nodes
		WHERE valid_to IS NULL
		GROUP BY topic
		ORDER BY cnt DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("top topics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ts TopicSummary
		if err := rows.Scan(&ts.Topic, &ts.NodeCount); err != nil {
			continue
		}
		d.TopTopics = append(d.TopTopics, ts)
	}

	// Hot nodes (most recently created, limited to period)
	rows, err = g.pool.Query(ctx, `
		SELECT id, label, node_type, COALESCE(topic, '') as topic, created_at
		FROM nodes
		WHERE valid_to IS NULL AND created_at >= $1
		ORDER BY created_at DESC
		LIMIT 20
	`, since)
	if err != nil {
		return nil, fmt.Errorf("hot nodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ns NodeSummary
		if err := rows.Scan(&ns.ID, &ns.Label, &ns.NodeType, &ns.Topic, &ns.CreatedAt); err != nil {
			continue
		}
		d.HotNodes = append(d.HotNodes, ns)
	}

	// Orphan count
	err = g.pool.QueryRow(ctx, `
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
	`).Scan(&d.OrphanCount)
	if err != nil {
		d.OrphanCount = 0
	}

	// Quality score (simplified)
	var totalNodes int
	err = g.pool.QueryRow(ctx, `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`).Scan(&totalNodes)
	if err == nil && totalNodes > 0 {
		d.QualityScore = 100.0 * (1.0 - float64(d.OrphanCount)/float64(totalNodes))
		if d.QualityScore < 0 {
			d.QualityScore = 0
		}
	}

	// Generate recommendations
	d.Recommendations = g.generateRecommendations(d, totalNodes)

	return d, nil
}

func (g *Generator) generateRecommendations(d *Digest, totalNodes int) []string {
	var recs []string

	if d.NewNodes == 0 {
		recs = append(recs, "No new nodes in this period. Consider capturing more sessions or adding external sources.")
	}

	if d.NewEdges == 0 && d.NewNodes > 0 {
		recs = append(recs, fmt.Sprintf("%d new nodes but no new edges. Use /api/v1/taxonomy/suggest-edges to connect related nodes.", d.NewNodes))
	}

	if d.OrphanCount > totalNodes/2 {
		recs = append(recs, fmt.Sprintf("High orphan ratio: %d nodes (%.1f%%) have no connections. Run edge suggestions to improve connectivity.",
			d.OrphanCount, float64(d.OrphanCount)*100/float64(totalNodes)))
	}

	if len(d.TopTopics) > 0 {
		recs = append(recs, fmt.Sprintf("Top topic: '%s' with %d nodes. Consider exploring related concepts.",
			d.TopTopics[0].Topic, d.TopTopics[0].NodeCount))
	}

	if len(recs) == 0 {
		recs = append(recs, "Graph is healthy. Continue capturing sessions to grow your knowledge base.")
	}

	return recs
}

// GetTopicTrends returns topic counts over time.
func (g *Generator) GetTopicTrends(ctx context.Context, days int) (map[string][]int, error) {
	if days <= 0 {
		days = 7
	}

	trends := make(map[string][]int)
	now := time.Now().UTC()

	for i := days - 1; i >= 0; i-- {
		day := now.Add(-time.Duration(i) * 24 * time.Hour)
		dayStart := day.Truncate(24 * time.Hour)
		dayEnd := dayStart.Add(24 * time.Hour)

		rows, err := g.pool.Query(ctx, `
			SELECT COALESCE(topic, 'uncategorized') as topic, COUNT(*) as cnt
			FROM nodes
			WHERE valid_to IS NULL AND created_at >= $1 AND created_at < $2
			GROUP BY topic
		`, dayStart, dayEnd)
		if err != nil {
			continue
		}

		for rows.Next() {
			var topic string
			var count int
			if err := rows.Scan(&topic, &count); err != nil {
				continue
			}
			if trends[topic] == nil {
				trends[topic] = make([]int, days)
			}
			trends[topic][days-1-i] = count
		}
		rows.Close()
	}

	return trends, nil
}

// FormatDigest creates a human-readable summary.
func FormatDigest(d *Digest) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# MindBank %s Digest\n\n", strings.Title(d.Period))
	fmt.Fprintf(&b, "Generated: %s\n\n", d.GeneratedAt.Format("2006-01-02 15:04 UTC"))

	fmt.Fprintf(&b, "## Activity\n")
	fmt.Fprintf(&b, "- New nodes: %d\n", d.NewNodes)
	fmt.Fprintf(&b, "- New edges: %d\n", d.NewEdges)
	fmt.Fprintf(&b, "- Orphan nodes: %d\n", d.OrphanCount)
	fmt.Fprintf(&b, "- Quality score: %.1f/100\n\n", d.QualityScore)

	if len(d.TopTopics) > 0 {
		fmt.Fprintf(&b, "## Top Topics\n")
		for _, t := range d.TopTopics {
			fmt.Fprintf(&b, "- %s: %d nodes\n", t.Topic, t.NodeCount)
		}
		b.WriteString("\n")
	}

	if len(d.HotNodes) > 0 {
		fmt.Fprintf(&b, "## Recent Nodes\n")
		for i, n := range d.HotNodes {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s (%s)\n", n.NodeType, n.Label, n.Topic)
		}
		b.WriteString("\n")
	}

	if len(d.Recommendations) > 0 {
		fmt.Fprintf(&b, "## Recommendations\n")
		for _, r := range d.Recommendations {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}

	return b.String()
}
