// Package enrichment provides auto-summarization for MindBank nodes.
// Inspired by gbrain's enrichment pipeline.
package enrichment

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/models"
)

// Enricher provides node content enrichment capabilities.
type Enricher struct {
	pool *pgxpool.Pool
}

// NewEnricher creates a new enrichment service.
func NewEnricher(pool *pgxpool.Pool) *Enricher {
	return &Enricher{pool: pool}
}

// EnrichNode generates a summary for a single node by analyzing its content.
// Returns true if the node was updated.
func (e *Enricher) EnrichNode(ctx context.Context, nodeID string) (bool, error) {
	// Fetch the node
	var node models.Node
	err := e.pool.QueryRow(ctx, `
		SELECT id, label, content, summary, topic, node_type
		FROM nodes WHERE id = $1 AND valid_to IS NULL
	`, nodeID).Scan(&node.ID, &node.Label, &node.Content, &node.Summary, &node.Topic, &node.NodeType)
	if err != nil {
		return false, fmt.Errorf("fetch node: %w", err)
	}

	// Skip if already has a meaningful summary
	if node.Summary != "" && len(node.Summary) > 20 {
		return false, nil
	}

	// Skip if no content to summarize
	if node.Content == "" {
		return false, nil
	}

	// Generate summary
	summary := generateSummary(node.Label, node.Content, node.NodeType)

	// Update the node summary
	_, err = e.pool.Exec(ctx, `
		UPDATE nodes SET summary = $1, updated_at = now()
		WHERE id = $2 AND valid_to IS NULL
	`, summary, nodeID)
	if err != nil {
		return false, fmt.Errorf("update summary: %w", err)
	}

	return true, nil
}

// BatchEnrich processes up to limit nodes that need enrichment.
// Returns (enriched_count, error).
func (e *Enricher) BatchEnrich(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 50
	}

	// Find nodes with empty or very short summaries
	rows, err := e.pool.Query(ctx, `
		SELECT id FROM nodes
		WHERE valid_to IS NULL
		  AND (summary = '' OR summary IS NULL OR length(summary) < 20)
		  AND content != ''
		ORDER BY importance DESC, access_count DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("find nodes to enrich: %w", err)
	}
	defer rows.Close()

	var nodeIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		nodeIDs = append(nodeIDs, id)
	}

	enriched := 0
	for _, id := range nodeIDs {
		updated, err := e.EnrichNode(ctx, id)
		if err != nil {
			continue
		}
		if updated {
			enriched++
		}
	}

	return enriched, nil
}

// GetEnrichmentStats returns statistics about node enrichment status.
func (e *Enricher) GetEnrichmentStats(ctx context.Context) (map[string]interface{}, error) {
	var total, enriched, empty int
	err := e.pool.QueryRow(ctx, `
		SELECT 
			COUNT(*),
			COUNT(CASE WHEN summary != '' AND length(summary) >= 20 THEN 1 END),
			COUNT(CASE WHEN content = '' OR content IS NULL THEN 1 END)
		FROM nodes WHERE valid_to IS NULL
	`).Scan(&total, &enriched, &empty)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	needsEnrichment := total - enriched - empty
	if needsEnrichment < 0 {
		needsEnrichment = 0
	}

	return map[string]interface{}{
		"total_nodes":      total,
		"enriched_nodes":   enriched,
		"empty_content":    empty,
		"needs_enrichment": needsEnrichment,
		"enrichment_rate":  float64(enriched) / float64(total) * 100,
	}, nil
}

// generateSummary creates a summary from node content using rule-based extraction.
// This is a lightweight alternative to LLM-based summarization.
func generateSummary(label, content string, nodeType models.NodeType) string {
	if content == "" {
		return label
	}

	// Clean up content
	text := strings.TrimSpace(content)

	// Extract first sentence or first 200 chars
	var summary string
	if idx := strings.IndexAny(text, ".!?\n"); idx > 10 && idx < 200 {
		summary = strings.TrimSpace(text[:idx+1])
	} else if len(text) > 200 {
		summary = strings.TrimSpace(text[:200]) + "..."
	} else {
		summary = text
	}

	// Add node type context
	switch nodeType {
	case models.NodeDecision:
		return "Decision: " + summary
	case models.NodeProblem:
		return "Problem: " + summary
	case models.NodeAdvice:
		return "Advice: " + summary
	case models.NodeFact:
		return "Fact: " + summary
	default:
		return summary
	}
}
