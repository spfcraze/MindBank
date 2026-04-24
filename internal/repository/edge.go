package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mindbank/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EdgeRepo struct {
	pool     *pgxpool.Pool
	nodeRepo *NodeRepo
}

func NewEdgeRepo(pool *pgxpool.Pool) *EdgeRepo {
	return &EdgeRepo{pool: pool, nodeRepo: NewNodeRepo(pool)}
}

// Create inserts a new edge and updates the target's materialized path.
func (r *EdgeRepo) Create(ctx context.Context, req models.EdgeCreate) (*models.Edge, error) {
	ws := req.WorkspaceName
	if ws == "" {
		ws = "hermes"
	}
	w := float32(1.0)
	if req.Weight != nil {
		w = *req.Weight
	}
	meta := req.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}

	e := &models.Edge{}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
		RETURNING id, workspace_name, source_id, target_id, edge_type, weight, metadata, created_at
	`, ws, req.SourceID, req.TargetID, req.EdgeType, w, meta,
	).Scan(&e.ID, &e.WorkspaceName, &e.SourceID, &e.TargetID,
		&e.EdgeType, &e.Weight, &e.Metadata, &e.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Edge already exists — fetch and return it
			return r.GetByTriple(ctx, req.SourceID, req.TargetID, req.EdgeType)
		}
		return nil, fmt.Errorf("insert edge: %w", err)
	}

	// Update target's materialized path: target.path = source.path / target.id
	if parentPath, err := r.nodeRepo.GetPath(ctx, req.SourceID); err == nil {
		_ = r.nodeRepo.UpdatePath(ctx, req.TargetID, parentPath)
	}

	return e, nil
}

// GetByTriple returns an existing edge by source, target, and type.
func (r *EdgeRepo) GetByTriple(ctx context.Context, sourceID, targetID string, edgeType models.EdgeType) (*models.Edge, error) {
	e := &models.Edge{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_name, source_id, target_id, edge_type, weight, metadata, created_at
		FROM edges
		WHERE source_id = $1 AND target_id = $2 AND edge_type = $3
		LIMIT 1
	`, sourceID, targetID, edgeType,
	).Scan(&e.ID, &e.WorkspaceName, &e.SourceID, &e.TargetID,
		&e.EdgeType, &e.Weight, &e.Metadata, &e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get edge by triple: %w", err)
	}
	return e, nil
}

// Delete removes an edge by ID.
func (r *EdgeRepo) Delete(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM edges WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete edge: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteOrphaned deletes edges where source or target node is soft-deleted (valid_to IS NOT NULL).
func (r *EdgeRepo) DeleteOrphaned(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM edges
		WHERE source_id IN (SELECT id FROM nodes WHERE valid_to IS NOT NULL)
		   OR target_id IN (SELECT id FROM nodes WHERE valid_to IS NOT NULL)
	`)
	if err != nil {
		return 0, fmt.Errorf("delete orphan edges: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteByType removes all edges of a given type.
func (r *EdgeRepo) DeleteByType(ctx context.Context, edgeType models.EdgeType) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM edges WHERE edge_type = $1`, edgeType)
	if err != nil {
		return 0, fmt.Errorf("delete edges by type: %w", err)
	}
	return tag.RowsAffected(), nil
}

// GetByNode returns all edges touching a node (as source or target).
func (r *EdgeRepo) GetByNode(ctx context.Context, nodeID string) ([]models.Edge, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_name, source_id, target_id, edge_type, weight, metadata, created_at
		FROM edges
		WHERE source_id = $1 OR target_id = $1
		ORDER BY weight DESC, created_at DESC
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get edges by node: %w", err)
	}
	defer rows.Close()

	return scanEdges(rows)
}

// GetByType returns edges filtered by type.
func (r *EdgeRepo) GetByType(ctx context.Context, edgeType models.EdgeType, limit, offset int) ([]models.Edge, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_name, source_id, target_id, edge_type, weight, metadata, created_at
		FROM edges
		WHERE edge_type = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, edgeType, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get edges by type: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetAll returns all edges up to limit.
func (r *EdgeRepo) GetAll(ctx context.Context, limit, offset int) ([]models.Edge, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_name, source_id, target_id, edge_type, weight, metadata, created_at
		FROM edges
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get all edges: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetNeighbors returns nodes connected to a given node (1 hop).
func (r *EdgeRepo) GetNeighbors(ctx context.Context, nodeID string) ([]models.NodeWithEdge, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			n.id, n.workspace_name, n.namespace, n.label, n.node_type, n.content, n.summary,
			n.metadata, n.importance, n.access_count, n.last_accessed, n.valid_from, n.valid_to,
			n.version, n.predecessor_id, n.created_at, n.updated_at,
			e.edge_type::text, e.weight, 1 AS depth
		FROM edges e
		JOIN nodes n ON n.id = CASE WHEN e.source_id = $1 THEN e.target_id ELSE e.source_id END
		WHERE (e.source_id = $1 OR e.target_id = $1)
		  AND n.valid_to IS NULL
		ORDER BY e.weight DESC
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get neighbors: %w", err)
	}
	defer rows.Close()

	var result []models.NodeWithEdge
	for rows.Next() {
		var nw models.NodeWithEdge
		err := rows.Scan(
			&nw.ID, &nw.WorkspaceName, &nw.Namespace, &nw.Label, &nw.NodeType,
			&nw.Content, &nw.Summary, &nw.Metadata, &nw.Importance, &nw.AccessCount,
			&nw.LastAccessed, &nw.ValidFrom, &nw.ValidTo, &nw.Version,
			&nw.PredecessorID, &nw.CreatedAt, &nw.UpdatedAt,
			&nw.EdgeType, &nw.EdgeWeight, &nw.Depth,
		)
		if err != nil {
			return nil, fmt.Errorf("scan neighbor: %w", err)
		}
		result = append(result, nw)
	}
	return result, nil
}

// GetNeighborsByNodeIDs returns a batch lookup of 1-hop neighbors for multiple node IDs.
// Returns a map: anchor_nodeID -> []NodeWithEdge (the neighbors).
func (r *EdgeRepo) GetNeighborsByNodeIDs(ctx context.Context, nodeIDs []string) (map[string][]models.NodeWithEdge, error) {
	result := make(map[string][]models.NodeWithEdge)
	if len(nodeIDs) == 0 {
		return result, nil
	}

	// Build dynamic IN clause placeholders: $1, $2, ..., $N
	placeholders := make([]string, len(nodeIDs))
	args := make([]interface{}, len(nodeIDs))
	for i, id := range nodeIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ", ")

	// The query: for each edge where one endpoint is in nodeIDs, the anchor is the one
	// in the input set and the neighbor is the other endpoint. We join nodes to get neighbor info.
	query := fmt.Sprintf(`
		SELECT
			CASE WHEN e.source_id IN (%s) THEN e.source_id ELSE e.target_id END AS anchor_id,
			n.id, n.workspace_name, n.namespace, n.label, n.node_type, n.content, n.summary,
			n.metadata, n.importance, n.access_count, n.last_accessed, n.valid_from, n.valid_to,
			n.version, n.predecessor_id, n.created_at, n.updated_at,
			e.edge_type::text, e.weight, 1 AS depth
		FROM edges e
		JOIN nodes n ON n.id = CASE WHEN e.source_id IN (%s) THEN e.target_id ELSE e.source_id END
		WHERE (e.source_id IN (%s) OR e.target_id IN (%s))
		  AND n.valid_to IS NULL
		ORDER BY e.weight DESC
	`, inClause, inClause, inClause, inClause)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get neighbors by node IDs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var anchorID string
		var nw models.NodeWithEdge
		err := rows.Scan(
			&anchorID,
			&nw.ID, &nw.WorkspaceName, &nw.Namespace, &nw.Label, &nw.NodeType,
			&nw.Content, &nw.Summary, &nw.Metadata, &nw.Importance, &nw.AccessCount,
			&nw.LastAccessed, &nw.ValidFrom, &nw.ValidTo, &nw.Version,
			&nw.PredecessorID, &nw.CreatedAt, &nw.UpdatedAt,
			&nw.EdgeType, &nw.EdgeWeight, &nw.Depth,
		)
		if err != nil {
			return nil, fmt.Errorf("scan neighbor by node IDs: %w", err)
		}
		result[anchorID] = append(result[anchorID], nw)
	}
	return result, nil
}

// GetNeighborsDeep returns nodes connected up to N hops using recursive CTE.
func (r *EdgeRepo) GetNeighborsDeep(ctx context.Context, nodeID string, depth int) ([]models.NodeWithEdge, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5 // safety cap
	}

	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE neighbors AS (
			-- Base: direct connections
			SELECT
				CASE WHEN e.source_id = $1 THEN e.target_id ELSE e.source_id END AS neighbor_id,
				e.edge_type::text,
				e.weight,
				1 AS depth
			FROM edges e
			WHERE e.source_id = $1 OR e.target_id = $1

			UNION

			-- Recursive: follow connections
			SELECT
				CASE WHEN e.source_id = n.neighbor_id THEN e.target_id ELSE e.source_id END,
				e.edge_type::text,
				e.weight,
				n.depth + 1
			FROM edges e
			JOIN neighbors n ON e.source_id = n.neighbor_id OR e.target_id = n.neighbor_id
			WHERE n.depth < $2
		)
		SELECT DISTINCT
			n.id, n.workspace_name, n.namespace, n.label, n.node_type, n.content, n.summary,
			n.metadata, n.importance, n.access_count, n.last_accessed, n.valid_from, n.valid_to,
			n.version, n.predecessor_id, n.created_at, n.updated_at,
			ne.edge_type, ne.weight, ne.depth
		FROM neighbors ne
		JOIN nodes n ON n.id = ne.neighbor_id
		WHERE n.valid_to IS NULL AND n.id != $1
		ORDER BY ne.depth, ne.weight DESC
	`, nodeID, depth)
	if err != nil {
		return nil, fmt.Errorf("get neighbors deep: %w", err)
	}
	defer rows.Close()

	var result []models.NodeWithEdge
	for rows.Next() {
		var nw models.NodeWithEdge
		err := rows.Scan(
			&nw.ID, &nw.WorkspaceName, &nw.Namespace, &nw.Label, &nw.NodeType,
			&nw.Content, &nw.Summary, &nw.Metadata, &nw.Importance, &nw.AccessCount,
			&nw.LastAccessed, &nw.ValidFrom, &nw.ValidTo, &nw.Version,
			&nw.PredecessorID, &nw.CreatedAt, &nw.UpdatedAt,
			&nw.EdgeType, &nw.EdgeWeight, &nw.Depth,
		)
		if err != nil {
			return nil, fmt.Errorf("scan deep neighbor: %w", err)
		}
		result = append(result, nw)
	}
	return result, nil
}

// FindPath finds shortest path between two nodes using recursive CTE (BFS).
func (r *EdgeRepo) FindPath(ctx context.Context, sourceID, targetID string, maxDepth int) ([]string, error) {
	if maxDepth < 1 {
		maxDepth = 6
	}

	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE paths AS (
			-- Start from source
			SELECT
				ARRAY[$1::text] AS path,
				$1::text AS current,
				1 AS depth

			UNION ALL

			-- Expand
			SELECT
				p.path || next_id,
				next_id,
				p.depth + 1
			FROM paths p
			JOIN (
				SELECT
					CASE WHEN e.source_id = p.current THEN e.target_id ELSE e.source_id END AS next_id
				FROM edges e
				JOIN paths p ON e.source_id = p.current OR e.target_id = p.current
			) nexts ON true
			WHERE p.depth < $3
			  AND next_id != ALL(p.path) -- avoid cycles
		)
		SELECT path FROM paths WHERE current = $2 ORDER BY depth LIMIT 1
	`, sourceID, targetID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("find path: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil // no path found
	}

	var path []string
	if err := rows.Scan(&path); err != nil {
		return nil, fmt.Errorf("scan path: %w", err)
	}
	return path, nil
}

func scanEdges(rows pgx.Rows) ([]models.Edge, error) {
	var edges []models.Edge
	for rows.Next() {
		var e models.Edge
		if err := rows.Scan(&e.ID, &e.WorkspaceName, &e.SourceID, &e.TargetID,
			&e.EdgeType, &e.Weight, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	return edges, nil
}

// Count returns the total number of edges in the database.
func (r *EdgeRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM edges`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count edges: %w", err)
	}
	return count, nil
}

// CountByType returns the number of edges of a given type.
func (r *EdgeRepo) CountByType(ctx context.Context, edgeType models.EdgeType) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM edges WHERE edge_type = $1`, edgeType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count edges by type: %w", err)
	}
	return count, nil
}
