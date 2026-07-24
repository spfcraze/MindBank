package forgetting

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/models"
)

// Service handles automatic forgetting of expired nodes.
type Service struct {
	pool *pgxpool.Pool
}

// RecentAccessGracePeriod is how long after last_accessed a node is protected
// from expiry even if its TTL has passed. This covers project gaps (vacations,
// context switching) without letting truly abandoned knowledge accumulate.
const RecentAccessGracePeriod = 14 * 24 * time.Hour // 14 days

// NewService creates a new forgetting service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ExpireNodes marks expired nodes as superseded.
// Returns the count of nodes expired.
func (s *Service) ExpireNodes(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Find expired nodes that are still current, not pinned, and NOT recently accessed
	// A node is "recently accessed" if last_accessed > (now() - grace_period)
	// Grace period = 14 days (covers project gaps, vacations, context switching)
	// The "important" check compares text, not ::boolean: one node with a
	// non-boolean value ("yes", "very") would make the cast error and halt
	// ALL TTL expiry permanently.
	rows, err := tx.Query(ctx, `
		SELECT id, node_type
		FROM nodes
		WHERE expires_at IS NOT NULL
		  AND expires_at < now()
		  AND valid_to IS NULL
		  AND lower(coalesce(metadata->>'important', 'false')) NOT IN ('true', 't', 'yes', '1')
		  AND (last_accessed IS NULL OR last_accessed < now() - interval '14 days')
	`)
	if err != nil {
		return 0, fmt.Errorf("query expired: %w", err)
	}

	// Collect all expired nodes first (can't exec while rows are open)
	var expiredNodes []struct {
		id       string
		nodeType models.NodeType
	}
	for rows.Next() {
		var id string
		var nodeType models.NodeType
		if err := rows.Scan(&id, &nodeType); err != nil {
			continue
		}
		expiredNodes = append(expiredNodes, struct {
			id       string
			nodeType models.NodeType
		}{id, nodeType})
	}
	rows.Close()

	var count int64
	for _, node := range expiredNodes {
		// Mark as superseded (set valid_to)
		_, err := tx.Exec(ctx, `
			UPDATE nodes
			SET valid_to = now(), updated_at = now()
			WHERE id = $1
		`, node.id)
		if err != nil {
			// First error aborts the tx; bail out instead of spamming
			// failed statements against an aborted transaction.
			return 0, fmt.Errorf("expire node %s: %w", node.id, err)
		}

		// Clean up like NodeRepo.Delete does: live edges to an expired node
		// skew isolation detection (dream bridging never rescues genuinely
		// isolated memories) and its embedding keeps occupying HNSW
		// candidate slots in vector search.
		_, _ = tx.Exec(ctx, `UPDATE edges SET valid_to = now() WHERE (source_id = $1 OR target_id = $1) AND valid_to IS NULL`, node.id)
		_, _ = tx.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, node.id)

		// Log the action
		_, _ = tx.Exec(ctx, `
			INSERT INTO forgetting_log (node_id, action, reason)
			VALUES ($1, 'expired', $2)
		`, node.id, fmt.Sprintf("TTL expired for %s", node.nodeType))

		count++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return count, nil
}

// Stats holds forgetting run statistics.
type Stats struct {
	ExpiredCount int64     `json:"expired_count"`
	RunAt        time.Time `json:"run_at"`
}

// RunDaily runs the expiry job and returns stats.
func (s *Service) RunDaily(ctx context.Context) (*Stats, error) {
	count, err := s.ExpireNodes(ctx)
	if err != nil {
		return nil, err
	}

	return &Stats{
		ExpiredCount: count,
		RunAt:        time.Now(),
	}, nil
}

// GetExpiringSoon returns nodes expiring within the given days.
func (s *Service) GetExpiringSoon(ctx context.Context, days int) ([]models.Node, error) {
	if days <= 0 {
		days = 7
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_name, namespace, label, node_type, content, summary, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, expires_at, created_at, updated_at
		FROM nodes
		WHERE expires_at IS NOT NULL
		  AND expires_at > now()
		  AND expires_at < now() + make_interval(days => $1)
		  AND valid_to IS NULL
		ORDER BY expires_at ASC
	`, days)
	if err != nil {
		return nil, fmt.Errorf("query expiring: %w", err)
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		err := rows.Scan(
			&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
			&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
			&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
			&n.PredecessorID, &n.ExpiresAt, &n.CreatedAt, &n.UpdatedAt,
		)
		if err != nil {
			continue
		}
		nodes = append(nodes, n)
	}

	return nodes, nil
}
