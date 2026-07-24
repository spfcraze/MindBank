package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"mindbank/internal/dedup"
	"mindbank/internal/forgetting"
	"mindbank/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// withTx executes fn inside a database transaction.
func withTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return withTxTimeout(ctx, pool, 10*time.Second, fn)
}

func withTxTimeout(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration, fn func(pgx.Tx) error) error {
	if pool == nil {
		return fmt.Errorf("pool is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

type NodeRepo struct {
	Pool         *pgxpool.Pool
	searchRepo   *SearchRepo
	snapshotRepo *SnapshotRepo
}

func NewNodeRepo(pool *pgxpool.Pool) *NodeRepo {
	return &NodeRepo{Pool: pool}
}

// SetSearchRepo links the search repo for cache invalidation on writes.
func (r *NodeRepo) SetSearchRepo(sr *SearchRepo) {
	r.searchRepo = sr
}

// SetSnapshotRepo links the snapshot repo for cache invalidation on writes.
func (r *NodeRepo) SetSnapshotRepo(sr *SnapshotRepo) {
	r.snapshotRepo = sr
}

// invalidateCaches invalidates both search and snapshot caches for a workspace/namespace.
func (r *NodeRepo) invalidateCaches(workspace, ns string) {
	if r.searchRepo != nil {
		r.searchRepo.InvalidateResultCache()
	}
	if r.snapshotRepo != nil {
		if workspace != "" && ns != "" {
			r.snapshotRepo.InvalidateCache(workspace, ns, "default")
		} else if workspace != "" {
			r.snapshotRepo.InvalidateAllNamespaces(workspace, "default")
		} else {
			// No workspace specified — invalidate all cached snapshots
			r.snapshotRepo.InvalidateAllWorkspaces()
		}
	}
}

// SaveDQASnapshot stores a DQA score for trend tracking.
func (r *NodeRepo) SaveDQASnapshot(ctx context.Context, score, totalNodes, issuesCount int) error {
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO dqa_snapshots (score, total_nodes, issues_count)
		VALUES ($1, $2, $3)
	`, score, totalNodes, issuesCount)
	if err != nil {
		return fmt.Errorf("save dqa snapshot: %w", err)
	}
	return nil
}

// GetDQATrend returns recent DQA snapshots.
func (r *NodeRepo) GetDQATrend(ctx context.Context, days int) ([]struct {
	Score       int       `json:"score"`
	TotalNodes  int       `json:"total_nodes"`
	IssuesCount int       `json:"issues_count"`
	CreatedAt   time.Time `json:"created_at"`
}, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT score, total_nodes, issues_count, created_at
		FROM dqa_snapshots
		WHERE created_at > now() - make_interval(days => $1)
		ORDER BY created_at ASC
	`, days)
	if err != nil {
		return nil, fmt.Errorf("get dqa trend: %w", err)
	}
	defer rows.Close()

	var results []struct {
		Score       int       `json:"score"`
		TotalNodes  int       `json:"total_nodes"`
		IssuesCount int       `json:"issues_count"`
		CreatedAt   time.Time `json:"created_at"`
	}
	for rows.Next() {
		var r struct {
			Score       int       `json:"score"`
			TotalNodes  int       `json:"total_nodes"`
			IssuesCount int       `json:"issues_count"`
			CreatedAt   time.Time `json:"created_at"`
		}
		if err := rows.Scan(&r.Score, &r.TotalNodes, &r.IssuesCount, &r.CreatedAt); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// Create inserts a new node and returns it.
func (r *NodeRepo) Create(ctx context.Context, req models.NodeCreate) (*models.Node, error) {
	ws := req.WorkspaceName
	if ws == "" {
		// Must match the MCP/delete default ("hermes", the seeded workspace);
		// a different default here made HTTP-created memories invisible to
		// MCP recall and undeletable without an explicit ?workspace=.
		ws = "hermes"
	}
	ns := req.Namespace
	if ns == "" {
		ns = "global"
	}

	// Ensure the workspace row exists so the nodes FK can't reject the save.
	if _, err := r.Pool.Exec(ctx,
		`INSERT INTO workspaces (name) VALUES ($1) ON CONFLICT DO NOTHING`, ws); err != nil {
		return nil, fmt.Errorf("ensure workspace: %w", err)
	}
	meta := req.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	imp := float32(0.5)
	if req.Importance != nil {
		imp = *req.Importance
	}

	// Deduplication: compute hash and check for existing node
	hash := dedup.ComputeHash(req.Label, req.Content, req.Summary)
	existingID, err := dedup.CheckDuplicate(ctx, r.Pool, hash)
	if err != nil {
		return nil, fmt.Errorf("dedup check: %w", err)
	}
	if existingID != "" {
		existing, err := r.Get(ctx, existingID)
		if err != nil {
			return nil, fmt.Errorf("fetch duplicate: %w", err)
		}
		if existing == nil {
			// Stale hash reference — proceed with insert and update hash mapping
			slog.Warn("dedup stale hash", "hash", hash, "node_id", existingID)
		} else {
			// Re-observation reinforcement: the same memory was seen again, so
			// it's more trustworthy — bump confirmation + importance and promote
			// epistemic status. This is how a fact learned repeatedly becomes a
			// strong node instead of many weak duplicates.
			if err := r.ReinforceObservation(ctx, existing.ID); err == nil {
				// Reflect the reinforcement in the returned node (the Get above
				// read the pre-reinforcement snapshot).
				existing.ConfirmationCount++
				if existing.Importance+0.03 < 1.0 {
					existing.Importance += 0.03
				} else {
					existing.Importance = 1.0
				}
			}
			existing.Deduplicated = true
			return existing, nil
		}
	}

	// Auto-calculate expiry if not provided
	expiresAt := req.ExpiresAt
	if expiresAt == nil && req.NodeType != "" {
		tempNode := &models.Node{NodeType: req.NodeType, Metadata: meta}
		expiresAt = forgetting.CalculateExpiry(tempNode)
	}
	
	epistemicLabel := req.EpistemicLabel
	if epistemicLabel == "" {
		epistemicLabel = "unknown"
	}

	// Default status to 'open' if not provided or invalid
	status := req.Status
	if status == "" || !status.IsValid() {
		status = models.StatusOpen
	}

	// Default source to empty, blocker to empty
	source := req.Source
	blocker := req.Blocker

	n := &models.Node{}
	err = r.Pool.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, metadata, importance, expires_at, materialized_path, epistemic_label, status, confirmation_count, source, blocker)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '/' || gen_random_uuid()::text, $10, $11, $12, $13, $14)
		RETURNING id, workspace_name, namespace, label, node_type, content, summary, topic, metadata,
		          importance, access_count, last_accessed, valid_from, valid_to, version,
		          predecessor_id, expires_at, created_at, updated_at, epistemic_label, status, confirmation_count, source, blocker
	`, ws, ns, req.Label, req.NodeType, req.Content, req.Summary, meta, imp, expiresAt, epistemicLabel, status, 0, source, blocker,
	).Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
		&n.Content, &n.Summary, &n.Topic, &n.Metadata, &n.Importance, &n.AccessCount,
		&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
		&n.PredecessorID, &n.ExpiresAt, &n.CreatedAt, &n.UpdatedAt, &n.EpistemicLabel, &n.Status, &n.ConfirmationCount, &n.Source, &n.Blocker)
	if err != nil {
		return nil, fmt.Errorf("insert node: %w", err)
	}

	// Set materialized path to /{id}
	_, _ = r.Pool.Exec(ctx, `UPDATE nodes SET materialized_path = '/' || $1 WHERE id = $1`, n.ID)

	// Store dedup hash (best-effort)
	if err := dedup.StoreHash(ctx, r.Pool, hash, n.ID); err != nil {
		slog.Warn("failed to store dedup hash", "error", err, "node_id", n.ID)
	}

	// Invalidate caches on new node creation
	r.invalidateCaches(ws, ns)

	return n, nil
}

// UpdatePath sets a node's materialized path based on its parent's path.
func (r *NodeRepo) UpdatePath(ctx context.Context, childID, parentPath string) error {
	newPath := parentPath + "/" + childID
	_, err := r.Pool.Exec(ctx, `
		UPDATE nodes SET materialized_path = $1 WHERE id = $2 AND valid_to IS NULL
	`, newPath, childID)
	return err
}

// FindSimilarNodes finds nodes with embedding similarity > threshold, excluding the given node and its connected neighbors.
func (r *NodeRepo) FindSimilarNodes(ctx context.Context, nodeID string, threshold float32, limit int) ([]struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	NodeType   string  `json:"node_type"`
	Namespace  string  `json:"namespace"`
	Similarity float32 `json:"similarity"`
}, error) {
	if limit <= 0 {
		limit = 10
	}
	if threshold <= 0 {
		threshold = 0.70
	}

	rows, err := r.Pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, n.namespace, 1 - (ne.embedding <=> target.embedding) AS similarity
		FROM node_embeddings ne
		JOIN nodes n ON n.id = ne.node_id AND n.valid_to IS NULL
		CROSS JOIN LATERAL (
			SELECT embedding FROM node_embeddings WHERE node_id = $1 LIMIT 1
		) target
		WHERE ne.node_id != $1
		  AND 1 - (ne.embedding <=> target.embedding) >= $2
		  AND ne.node_id NOT IN (
			  SELECT source_id FROM edges WHERE target_id = $1
			  UNION
			  SELECT target_id FROM edges WHERE source_id = $1
		  )
		ORDER BY ne.embedding <=> target.embedding
		LIMIT $3
	`, nodeID, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("find similar nodes: %w", err)
	}
	defer rows.Close()

	var results []struct {
		ID         string  `json:"id"`
		Label      string  `json:"label"`
		NodeType   string  `json:"node_type"`
		Namespace  string  `json:"namespace"`
		Similarity float32 `json:"similarity"`
	}
	for rows.Next() {
		var r struct {
			ID         string  `json:"id"`
			Label      string  `json:"label"`
			NodeType   string  `json:"node_type"`
			Namespace  string  `json:"namespace"`
			Similarity float32 `json:"similarity"`
		}
		if err := rows.Scan(&r.ID, &r.Label, &r.NodeType, &r.Namespace, &r.Similarity); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// GetPath returns a node's materialized path.
func (r *NodeRepo) GetPath(ctx context.Context, nodeID string) (string, error) {
	var path string
	err := r.Pool.QueryRow(ctx, `SELECT materialized_path FROM nodes WHERE id = $1 AND valid_to IS NULL`, nodeID).Scan(&path)
	return path, err
}

// GetAncestors returns all ancestor nodes using materialized path (O(1)).
func (r *NodeRepo) GetAncestors(ctx context.Context, nodeID string) ([]models.Node, error) {
	path, err := r.GetPath(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	rows, err := r.Pool.Query(ctx, `
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

	rows, err := r.Pool.Query(ctx, `
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

// ReinforceObservation strengthens a memory that was observed again: +1
// confirmation, a small importance nudge, and epistemic promotion as evidence
// accumulates (unknown → inferred → observed at 3+ confirmations).
func (r *NodeRepo) ReinforceObservation(ctx context.Context, id string) error {
	_, err := r.Pool.Exec(ctx, `
		UPDATE nodes SET
			confirmation_count = confirmation_count + 1,
			importance = LEAST(importance + 0.03, 1.0),
			last_accessed = now(),
			epistemic_label = CASE
				WHEN confirmation_count + 1 >= 3 AND epistemic_label IN ('unknown','assumed','inferred') THEN 'observed'
				WHEN epistemic_label = 'unknown' THEN 'inferred'
				ELSE epistemic_label END
		WHERE id = $1 AND valid_to IS NULL
	`, id)
	return err
}

// ReinforceRecall records that memories were surfaced and (presumably) useful:
// a light access bump + tiny importance nudge for the recalled set. Batched and
// safe to call fire-and-forget from the search path.
func (r *NodeRepo) ReinforceRecall(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.Pool.Exec(ctx, `
		UPDATE nodes SET
			access_count = access_count + 1,
			last_accessed = now(),
			importance = LEAST(importance + 0.005, 1.0)
		WHERE id = ANY($1) AND valid_to IS NULL
	`, ids)
	return err
}

// Get retrieves a current node by ID and bumps access count.
func (r *NodeRepo) Get(ctx context.Context, id string) (*models.Node, error) {
	n := &models.Node{}
	err := r.Pool.QueryRow(ctx, `
		UPDATE nodes SET access_count = access_count + 1, last_accessed = now()
		WHERE id = $1 AND valid_to IS NULL
		RETURNING id, workspace_name, namespace, label, node_type, content, summary, topic, metadata,
		          importance, access_count, last_accessed, valid_from, valid_to, version,
		          predecessor_id, created_at, updated_at, epistemic_label, status, confirmation_count, source, blocker
	`, id).Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
		&n.Content, &n.Summary, &n.Topic, &n.Metadata, &n.Importance, &n.AccessCount,
		&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
		&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt, &n.EpistemicLabel, &n.Status, &n.ConfirmationCount, &n.Source, &n.Blocker)
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
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Fetch current node
	old := &models.Node{}
	var oldPath string
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_name, namespace, label, node_type, content, summary, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, created_at, updated_at, epistemic_label, status, confirmation_count, source, blocker,
		       coalesce(topic, ''), expires_at, coalesce(materialized_path, '/')
		FROM nodes WHERE id = $1 AND valid_to IS NULL
		FOR UPDATE
	`, id).Scan(&old.ID, &old.WorkspaceName, &old.Namespace, &old.Label, &old.NodeType,
		&old.Content, &old.Summary, &old.Metadata, &old.Importance, &old.AccessCount,
		&old.LastAccessed, &old.ValidFrom, &old.ValidTo, &old.Version,
		&old.PredecessorID, &old.CreatedAt, &old.UpdatedAt, &old.EpistemicLabel, &old.Status, &old.ConfirmationCount, &old.Source, &old.Blocker,
		&old.Topic, &old.ExpiresAt, &oldPath)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch current node: %w", err)
	}

	// Apply updates to fields
	label := old.Label
	if req.Label != nil {
		label = *req.Label
	}
	expiresAt := old.ExpiresAt
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt
	}
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
	epistemicLabel := old.EpistemicLabel
	if req.EpistemicLabel != nil {
		epistemicLabel = *req.EpistemicLabel
	}
	status := old.Status
	if req.Status != nil {
		status = *req.Status
	}
	source := old.Source
	if req.Source != nil {
		source = *req.Source
	}
	blocker := old.Blocker
	if req.Blocker != nil {
		blocker = *req.Blocker
	}

	// Invalidate old version (with version check for optimistic locking)
	now := time.Now()
	tag, err := tx.Exec(ctx, `
		UPDATE nodes SET valid_to = $1 
		WHERE id = $2 AND valid_to IS NULL AND version = $3
	`, now, old.ID, old.Version)
	if err != nil {
		return nil, fmt.Errorf("invalidate old node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("version conflict: node was modified by another transaction")
	}

	// Insert new version, carrying over topic/expires_at/materialized_path —
	// omitting them here silently reset topic grouping, TTL, and hierarchy
	// on every version bump.
	n := &models.Node{}
	oldID := old.ID
	err = tx.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, metadata,
		                   importance, access_count, valid_from, version, predecessor_id, epistemic_label, status, confirmation_count, source, blocker,
		                   topic, expires_at, materialized_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, nullif($18, ''), $19, $20)
		RETURNING id, workspace_name, namespace, label, node_type, content, summary, metadata,
		          importance, access_count, last_accessed, valid_from, valid_to, version,
		          predecessor_id, created_at, updated_at, epistemic_label, status, confirmation_count, source, blocker
	`, old.WorkspaceName, old.Namespace, label, old.NodeType,
		content, summary, meta, imp, old.AccessCount, now, old.Version+1, &oldID,
		epistemicLabel, status, old.ConfirmationCount, source, blocker,
		old.Topic, expiresAt, oldPath,
	).Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
		&n.Content, &n.Summary, &n.Metadata, &n.Importance, &n.AccessCount,
		&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
		&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt, &n.EpistemicLabel, &n.Status, &n.ConfirmationCount, &n.Source, &n.Blocker)
	if err != nil {
		return nil, fmt.Errorf("insert new version: %w", err)
	}

	// Materialized paths embed node IDs; swap the old ID for the new one in
	// this node's own path (last segment) and in every descendant's path.
	_, err = tx.Exec(ctx, `
		UPDATE nodes SET materialized_path =
			CASE
				WHEN materialized_path IS NULL OR materialized_path IN ('', '/') THEN '/' || $1::text
				WHEN right(materialized_path, length($2::text) + 1) = '/' || $2::text
					THEN left(materialized_path, length(materialized_path) - length($2::text)) || $1::text
				ELSE materialized_path
			END
		WHERE id = $1
	`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("update own path: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE nodes SET materialized_path = replace(materialized_path, '/' || $2::text || '/', '/' || $1::text || '/')
		WHERE materialized_path LIKE '%/' || $2::text || '/%'
	`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("update descendant paths: %w", err)
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

	// Relink membership and the embedding row to the new version ID.
	// Without this, session/collection membership dies with the old row when
	// PurgeOldVersions cascades, and the current version has no embedding
	// (invisible to vector search) until re-embedded.
	_, err = tx.Exec(ctx, `UPDATE session_nodes SET node_id = $1 WHERE node_id = $2`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("relink session nodes: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE collection_nodes SET node_id = $1 WHERE node_id = $2`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("relink collection nodes: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE node_embeddings SET node_id = $1 WHERE node_id = $2`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("relink embedding: %w", err)
	}
	// Repoint dedup hashes so saves of this content dedup against the
	// current version; hashes left on the old ID die with it at purge time
	// (FK cascade), silently disabling dedup for that content.
	_, err = tx.Exec(ctx, `UPDATE node_hashes SET node_id = $1 WHERE node_id = $2`, n.ID, old.ID)
	if err != nil {
		return nil, fmt.Errorf("relink hashes: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Invalidate caches on node update
	r.invalidateCaches(old.WorkspaceName, old.Namespace)

	return n, nil
}

// UpdateTopic updates the topic field for a node (in-place, no versioning).
func (r *NodeRepo) UpdateTopic(ctx context.Context, id string, topic string) error {
	_, err := r.Pool.Exec(ctx, `
		UPDATE nodes SET topic = $1, updated_at = now()
		WHERE id = $2 AND valid_to IS NULL
	`, topic, id)
	if err != nil {
		return fmt.Errorf("update topic: %w", err)
	}
	return nil
}

// Delete soft-deletes a node and cleans up all connected records in a transaction.
func (r *NodeRepo) Delete(ctx context.Context, id, workspace string) (bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin delete tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Soft-delete the node (with workspace verification)
	tag, err := tx.Exec(ctx, `
		UPDATE nodes SET valid_to = now()
		WHERE id = $1 AND valid_to IS NULL AND workspace_name = $2
	`, id, workspace)
	if err != nil {
		return false, fmt.Errorf("soft-delete node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	// 2. Delete connected edges
	_, _ = tx.Exec(ctx, `DELETE FROM edges WHERE source_id = $1 OR target_id = $1`, id)

	// 3. Clear predecessor references
	_, _ = tx.Exec(ctx, `UPDATE nodes SET predecessor_id = NULL WHERE predecessor_id = $1`, id)

	// 4. Delete embedding
	_, _ = tx.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, id)

	// 5. Delete session associations
	_, _ = tx.Exec(ctx, `DELETE FROM session_nodes WHERE node_id = $1`, id)

	// 6. Delete collection associations
	_, _ = tx.Exec(ctx, `DELETE FROM collection_nodes WHERE node_id = $1`, id)

	// 7. Regenerate snapshot to remove deleted node from context
	// We do this inside the transaction to ensure consistency
	_, _ = tx.Exec(ctx, `
		DELETE FROM snapshots 
		WHERE workspace_name IN (
			SELECT DISTINCT workspace_name FROM nodes WHERE id = $1
		)
	`, id)

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit delete tx: %w", err)
	}

	// Invalidate caches on node deletion — invalidate all namespaces for this workspace
	// since we don't have the namespace without an extra query
	r.invalidateCaches("", "")

	return true, nil
}

// PurgeOldVersions hard-deletes soft-deleted temporal versions older than N days.
func (r *NodeRepo) PurgeOldVersions(ctx context.Context, olderThanDays int) (int64, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin purge tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Clear predecessor references pointing to nodes being purged
	_, err = tx.Exec(ctx, `
		UPDATE nodes SET predecessor_id = NULL
		WHERE predecessor_id IN (
			SELECT id FROM nodes
			WHERE valid_to IS NOT NULL
			  AND valid_to < now() - make_interval(days => $1)
		)
	`, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("clear predecessor refs: %w", err)
	}

	// Clear forgetting_log entries for nodes being purged
	_, err = tx.Exec(ctx, `
		DELETE FROM forgetting_log
		WHERE node_id IN (
			SELECT id FROM nodes
			WHERE valid_to IS NOT NULL
			  AND valid_to < now() - make_interval(days => $1)
		)
	`, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("clear forgetting_log refs: %w", err)
	}

	// Now safe to delete old versions
	tag, err := tx.Exec(ctx, `
		DELETE FROM nodes
		WHERE valid_to IS NOT NULL
		  AND valid_to < now() - make_interval(days => $1)
	`, olderThanDays)
	if err != nil {
		return 0, fmt.Errorf("purge old versions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit purge tx: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CompactNodeVersions hard-deletes all old temporal versions of a node chain except the current one.
func (r *NodeRepo) CompactNodeVersions(ctx context.Context, currentID string) (int64, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin compact tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Clear predecessor references pointing to versions being compacted
	_, err = tx.Exec(ctx, `
		UPDATE nodes SET predecessor_id = NULL
		WHERE predecessor_id IN (
			WITH RECURSIVE chain AS (
				SELECT id, predecessor_id, valid_to FROM nodes WHERE id = $1
				UNION ALL
				SELECT n.id, n.predecessor_id, n.valid_to
				FROM nodes n
				JOIN chain c ON n.id = c.predecessor_id
			)
			SELECT id FROM chain WHERE valid_to IS NOT NULL
		)
	`, currentID)
	if err != nil {
		return 0, fmt.Errorf("clear predecessor refs: %w", err)
	}

	tag, err := tx.Exec(ctx, `
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

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit compact tx: %w", err)
	}
	return tag.RowsAffected(), nil
}

// FindSimilarAcrossNamespaces finds nodes in targetNS that are semantically similar to nodes in sourceNS.
// Uses pgvector cosine similarity on embeddings. Returns pairs with similarity > threshold.
func (r *NodeRepo) FindSimilarAcrossNamespaces(ctx context.Context, sourceNS, targetNS string, limitPerNode int, threshold float32) ([]struct {
	SourceID   string  `json:"source_id"`
	SourceLabel string `json:"source_label"`
	TargetID   string  `json:"target_id"`
	TargetLabel string `json:"target_label"`
	Similarity float32 `json:"similarity"`
}, error) {
	if limitPerNode <= 0 {
		limitPerNode = 3
	}
	if threshold <= 0 {
		threshold = 0.70
	}

	rows, err := r.Pool.Query(ctx, `
		SELECT 
			s.node_id AS source_id,
			sn.label AS source_label,
			t.node_id AS target_id,
			tn.label AS target_label,
			1 - (s.embedding <=> t.embedding) AS similarity
		FROM node_embeddings s
		JOIN nodes sn ON sn.id = s.node_id AND sn.valid_to IS NULL AND sn.namespace = $1
		CROSS JOIN LATERAL (
			SELECT ne.node_id, ne.embedding
			FROM node_embeddings ne
			JOIN nodes n ON n.id = ne.node_id AND n.valid_to IS NULL AND n.namespace = $2
			WHERE ne.node_id != s.node_id
			ORDER BY s.embedding <=> ne.embedding
			LIMIT $3
		) t
		JOIN nodes tn ON tn.id = t.node_id AND tn.valid_to IS NULL
		WHERE 1 - (s.embedding <=> t.embedding) >= $4
		ORDER BY s.node_id, similarity DESC
	`, sourceNS, targetNS, limitPerNode, threshold)
	if err != nil {
		return nil, fmt.Errorf("find similar across namespaces: %w", err)
	}
	defer rows.Close()

	var results []struct {
		SourceID    string  `json:"source_id"`
		SourceLabel string `json:"source_label"`
		TargetID    string  `json:"target_id"`
		TargetLabel string `json:"target_label"`
		Similarity  float32 `json:"similarity"`
	}
	for rows.Next() {
		var r struct {
			SourceID    string  `json:"source_id"`
			SourceLabel string `json:"source_label"`
			TargetID    string  `json:"target_id"`
			TargetLabel string `json:"target_label"`
			Similarity  float32 `json:"similarity"`
		}
		if err := rows.Scan(&r.SourceID, &r.SourceLabel, &r.TargetID, &r.TargetLabel, &r.Similarity); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// List returns current nodes with optional filters.
func (r *NodeRepo) List(ctx context.Context, workspace, namespace string, nodeType models.NodeType, skill string, sortField, sortOrder string, limit, offset int) ([]models.Node, error) {
	if limit <= 0 || limit > 5000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Validate sort field to prevent SQL injection
	allowedSortFields := map[string]string{
		"created_at": "created_at",
		"updated_at": "updated_at",
		"label":      "label",
		"importance": "importance",
		"access_count": "access_count",
	}
	orderBy := "updated_at" // default
	if sf, ok := allowedSortFields[sortField]; ok {
		orderBy = sf
	}

	// Validate sort order
	if sortOrder != "asc" {
		sortOrder = "desc" // default
	}

	query := `
		SELECT id, workspace_name, namespace, label, node_type, content, summary, topic, metadata,
		       importance, access_count, last_accessed, valid_from, valid_to, version,
		       predecessor_id, created_at, updated_at, epistemic_label
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
	if skill != "" {
		query += fmt.Sprintf(" AND metadata->'skills' ? $%d", argN)
		args = append(args, skill)
		argN++
	}

	query += fmt.Sprintf(" ORDER BY %s %s LIMIT $%d OFFSET $%d", orderBy, sortOrder, argN, argN+1)
	args = append(args, limit, offset)

	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []models.Node
	for rows.Next() {
		var n models.Node
		err := rows.Scan(&n.ID, &n.WorkspaceName, &n.Namespace, &n.Label, &n.NodeType,
			&n.Content, &n.Summary, &n.Topic, &n.Metadata, &n.Importance, &n.AccessCount,
			&n.LastAccessed, &n.ValidFrom, &n.ValidTo, &n.Version,
			&n.PredecessorID, &n.CreatedAt, &n.UpdatedAt, &n.EpistemicLabel)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// GetHistory returns all versions of a node (temporal history) using recursive CTE.
func (r *NodeRepo) GetHistory(ctx context.Context, id string) ([]models.NodeHistoryEntry, error) {
	// Both walks carry a depth counter capped at 1000: with UNION ALL, a
	// predecessor_id cycle (possible via manual data repair) would otherwise
	// recurse until statement timeout or OOM.
	rows, err := r.Pool.Query(ctx, `
		WITH RECURSIVE version_chain AS (
			-- Start from the given node
			SELECT id, label, content, version, valid_from, valid_to, predecessor_id, 0 AS depth
			FROM nodes WHERE id = $1

			UNION ALL

			-- Walk backward through predecessor chain
			SELECT n.id, n.label, n.content, n.version, n.valid_from, n.valid_to, n.predecessor_id, vc.depth + 1
			FROM nodes n
			JOIN version_chain vc ON n.id = vc.predecessor_id
			WHERE vc.depth < 1000
		),
		forward_chain AS (
			-- Also walk forward (nodes that point to our chain)
			SELECT id, label, content, version, valid_from, valid_to, predecessor_id, 0 AS depth
			FROM nodes WHERE id = $1

			UNION ALL

			SELECT n.id, n.label, n.content, n.version, n.valid_from, n.valid_to, n.predecessor_id, fc.depth + 1
			FROM nodes n
			JOIN forward_chain fc ON n.predecessor_id = fc.id
			WHERE fc.depth < 1000
		)
		SELECT DISTINCT id, label, content, version, valid_from, valid_to, predecessor_id
		FROM (
			SELECT id, label, content, version, valid_from, valid_to, predecessor_id FROM version_chain
			UNION ALL
			SELECT id, label, content, version, valid_from, valid_to, predecessor_id FROM forward_chain
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
	tag, err := r.Pool.Exec(ctx, `
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

// CountNodes returns the number of active (non-deleted) nodes.
func (r *NodeRepo) CountNodes(ctx context.Context) (int, error) {
	var count int
	err := r.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count nodes: %w", err)
	}
	return count, nil
}
