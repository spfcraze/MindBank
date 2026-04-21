package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mindbank/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NodeRepo struct {
	pool *pgxpool.Pool
}

func NewNodeRepo(pool *pgxpool.Pool) *NodeRepo {
	return &NodeRepo{pool: pool}
}

// Create inserts a new node and returns it.
func (r *NodeRepo) Create(ctx context.Context, req models.NodeCreate) (*models.Node, error) {
	ws := req.WorkspaceName
	if ws == "" {
		ws = "hermes"
	}
	ns := req.Namespace
	if ns == "" {
		ns = "global"
	}
	meta := req.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	imp := float32(0.5)
	if req.Importance != nil {
		imp = *req.Importance
	}

	n := &models.Node{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, metadata, importance, materialized_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '/' || gen_random_uuid()::text)
		RETURNING id, workspace_name, namespace, label, node_type, content, summary, metadata,
		          importance, access_count, last_accessed, valid_from, valid_to, version,
		          predecessor_id, created_at, updated_at
	`, ws, ns, req.Label, req.NodeType, req.Content, req.Summary, meta, imp,
	).Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
		&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
		&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
		&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert node: %w", err)
	}

	// Set materialized path to /{id}
	_, _ = r.pool.Exec(ctx, `UPDATE nodes SET materialized_path = '/' || $1 WHERE id = $1`, n.ID)

	return n, nil
}

// UpdatePath sets a node's materialized path based on its parent's path.
func (r *NodeRepo) UpdatePath(ctx context.Context, childID, parentPath string) error {
	newPath := parentPath + "/" + childID
	_, err := r.pool.Exec(ctx, `
		UPDATE nodes SET materialized_path = $1 WHERE id = $2 AND valid_to IS NULL
	`, newPath, childID)
	return err
}

// GetPath returns a node's materialized path.
func (r *NodeRepo) GetPath(ctx context.Context, nodeID string) (string, error) {
	var path string
	err := r.pool.QueryRow(ctx, `SELECT materialized_path FROM nodes WHERE id = $1 AND valid_to IS NULL`, nodeID).Scan(&path)
	return path, err
}

// GetAncestors returns all ancestor nodes using materialized path (O(1)).
func (r *NodeRepo) GetAncestors(ctx context.Context, nodeID string) ([]models.Node, error) {
	path, err := r.GetPath(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_name, namespace, label, node_type::text, content, summary, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, created_at, updated_at
		FROM nodes
		WHERE valid_to IS NULL
		  AND $1 LIKE materialized_path || '/%'
		  AND id != $2
		ORDER BY length(materialized_path) DESC
	`, path, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		if err := rows.Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
			&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
			&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
			&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// GetDescendants returns all descendant nodes using materialized path (O(1)).
func (r *NodeRepo) GetDescendants(ctx context.Context, nodeID string) ([]models.Node, error) {
	path, err := r.GetPath(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_name, namespace, label, node_type::text, content, summary, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, created_at, updated_at
		FROM nodes
		WHERE valid_to IS NULL
		  AND materialized_path LIKE $1 || '/%'
		ORDER BY materialized_path
	`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		if err := rows.Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
			&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
			&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
			&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// Get retrieves a current node by ID and bumps access count.
func (r *NodeRepo) Get(ctx context.Context, id string) (*models.Node, error) {
	n := &models.Node{}
	err := r.pool.QueryRow(ctx, `
		UPDATE nodes SET access_count = access_count + 1, last_accessed = now()
		WHERE id = $1 AND valid_to IS NULL
		RETURNING id, workspace_name, namespace, label, node_type, content, summary, metadata,
		          importance, access_count, last_accessed, valid_from, valid_to, version,
		          predecessor_id, created_at, updated_at
	`, id).Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
		&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
		&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
		&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get node: %w", err)
	}
	return n, nil
}

// Update creates a new version of a node (temporal update).
// The old node is invalidated, a new version is created with predecessor link.
func (r *NodeRepo) Update(ctx context.Context, id string, req models.NodeUpdate) (*models.Node, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Fetch current node
	old := &models.Node{}
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_name, namespace, label, node_type, content, summary, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, created_at, updated_at
		FROM nodes WHERE id = $1 AND valid_to IS NULL
		FOR UPDATE
	`, id).Scan(&old.ID, &old.WorkspaceName, &old.Namespace, &old.Label, &old.NodeType,
		&old.Content, &old.Summary, &old.Metadata, &old.Importance, &old.AccessCount,
		&old.LastAccessed, &old.ValidFrom, &old.ValidTo, &old.Version,
		&old.PredecessorID, &old.CreatedAt, &old.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch current node: %w", err)
	}

	// Apply updates to fields
	content := old.Content
	if req.Content != nil {
		content = *req.Content
	}
	summary := old.Summary
	if req.Summary != nil {
		summary = *req.Summary
	}
	meta := old.Metadata
	if req.Metadata != nil {
		meta = req.Metadata
	}
	imp := old.Importance
	if req.Importance != nil {
		imp = *req.Importance
	}

	// Invalidate old version
	now := time.Now()
	_, err = tx.Exec(ctx, `UPDATE nodes SET valid_to = $1 WHERE id = $2`, now, old.ID)
	if err != nil {
		return nil, fmt.Errorf("invalidate old node: %w", err)
	}

	// Insert new version
	n := &models.Node{}
	oldID := old.ID
	err = tx.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, metadata,
		                   importance, access_count, valid_from, version, predecessor_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, workspace_name, namespace, label, node_type, content, summary, metadata,
		          importance, access_count, last_accessed, valid_from, valid_to, version,
		          predecessor_id, created_at, updated_at
	`, old.WorkspaceName, old.Namespace, old.Label, old.NodeType,
		content, summary, meta, imp, old.AccessCount, now, old.Version+1, &oldID,
	).Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
		&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
		&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
		&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert new version: %w", err)
	}

	// Relink edges: update all edges referencing old node ID to new node ID
	// This prevents edges from becoming orphaned after temporal updates
	_, err = tx.Exec(ctx, `UPDATE edges SET source_id = $1 WHERE source_id = $2`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("relink source edges: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE edges SET target_id = $1 WHERE target_id = $2`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("relink target edges: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return n, nil
}

// Delete soft-deletes a node by setting valid_to (never hard deletes).
func (r *NodeRepo) Delete(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE nodes SET valid_to = now()
		WHERE id = $1 AND valid_to IS NULL
	`, id)
	if err != nil {
		return false, fmt.Errorf("delete node: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// PurgeOldVersions hard-deletes soft-deleted temporal versions older than N days.
func (r *NodeRepo) PurgeOldVersions(ctx context.Context, olderThanDays int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM nodes
		WHERE valid_to IS NOT NULL
		  AND valid_to < now() - make_interval(days => $1)
	`, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("purge old versions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CompactNodeVersions hard-deletes all old temporal versions of a node chain except the current one.
func (r *NodeRepo) CompactNodeVersions(ctx context.Context, currentID string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH RECURSIVE chain AS (
			SELECT id, predecessor_id, valid_to FROM nodes WHERE id = $1
			UNION ALL
			SELECT n.id, n.predecessor_id, n.valid_to
			FROM nodes n
			JOIN chain c ON n.id = c.predecessor_id
		)
		DELETE FROM nodes
		WHERE id IN (SELECT id FROM chain WHERE valid_to IS NOT NULL)
	`, currentID)
	if err != nil {
		return 0, fmt.Errorf("compact node versions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// List returns current nodes with optional filters.
func (r *NodeRepo) List(ctx context.Context, workspace, namespace string, nodeType models.NodeType, limit, offset int) ([]models.Node, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, workspace_name, namespace, label, node_type, content, summary, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, created_at, updated_at
		FROM nodes
		WHERE valid_to IS NULL
	`
	args := []any{}
	argN := 1

	if workspace != "" {
		query += fmt.Sprintf(" AND workspace_name = $%d", argN)
		args = append(args, workspace)
		argN++
	}
	if namespace != "" {
		query += fmt.Sprintf(" AND namespace = $%d", argN)
		args = append(args, namespace)
		argN++
	}
	if nodeType != "" {
		query += fmt.Sprintf(" AND node_type = $%d", argN)
		args = append(args, nodeType)
		argN++
	}

	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		err := rows.Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
			&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
			&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
			&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// GetHistory returns all versions of a node (temporal history) using recursive CTE.
func (r *NodeRepo) GetHistory(ctx context.Context, id string) ([]models.NodeHistoryEntry, error) {
	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE version_chain AS (
			-- Start from the given node
			SELECT id, label, content, version, valid_from, valid_to, predecessor_id
			FROM nodes WHERE id = $1

			UNION ALL

			-- Walk backward through predecessor chain
			SELECT n.id, n.label, n.content, n.version, n.valid_from, n.valid_to, n.predecessor_id
			FROM nodes n
			JOIN version_chain vc ON n.id = vc.predecessor_id
		),
		forward_chain AS (
			-- Also walk forward (nodes that point to our chain)
			SELECT id, label, content, version, valid_from, valid_to, predecessor_id
			FROM nodes WHERE id = $1

			UNION ALL

			SELECT n.id, n.label, n.content, n.version, n.valid_from, n.valid_to, n.predecessor_id
			FROM nodes n
			JOIN forward_chain fc ON n.predecessor_id = fc.id
		)
		SELECT DISTINCT id, label, content, version, valid_from, valid_to, predecessor_id
		FROM (
			SELECT * FROM version_chain
			UNION ALL
			SELECT * FROM forward_chain
		) all_versions
		ORDER BY version ASC
	`, id)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	defer rows.Close()

	var history []models.NodeHistoryEntry
	for rows.Next() {
		var e models.NodeHistoryEntry
		if err := rows.Scan(&e.ID, &e.Label, &e.Content, &e.Version, &e.ValidFrom, &e.ValidTo, &e.PredecessorID); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		history = append(history, e)
	}
	return history, nil
}

// RecalculateImportance updates the importance column for all current nodes
// using the same scoring formula as the snapshot: recency, frequency, connectivity, type weight.
func (r *NodeRepo) RecalculateImportance(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE nodes SET importance = sub.score
		FROM (
			SELECT n.id,
				(
					0.30 * COALESCE(1.0 - EXTRACT(EPOCH FROM (now() - n.last_accessed)) / 2592000.0, 0.5)::real
					+ 0.25 * LEAST(n.access_count::real / 100.0, 1.0)::real
					+ 0.20 * LEAST((SELECT COUNT(*)::real / 20.0 FROM edges WHERE source_id = n.id OR target_id = n.id), 1.0)::real
					+ 0.15 * n.importance
					+ 0.10 * CASE n.node_type
						WHEN 'decision' THEN 1.0
						WHEN 'preference' THEN 0.9
						WHEN 'problem' THEN 0.9
						WHEN 'advice' THEN 0.8
						WHEN 'fact' THEN 0.7
						WHEN 'person' THEN 0.7
						WHEN 'project' THEN 0.7
						WHEN 'event' THEN 0.5
						WHEN 'topic' THEN 0.4
						WHEN 'concept' THEN 0.3
						ELSE 0.5
					END::real
				) AS score
			FROM nodes n
			WHERE n.valid_to IS NULL
		) sub
		WHERE nodes.id = sub.id AND nodes.valid_to IS NULL
	`)
	if err != nil {
		return 0, fmt.Errorf("recalculate importance: %w", err)
	}
	return tag.RowsAffected(), nil
}
