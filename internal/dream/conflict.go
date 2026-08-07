package dream

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConflictDetector finds nodes with high semantic similarity but contradictory attributes.
type ConflictDetector struct {
	pool DBTX
}

// NewConflictDetector creates a conflict detector.
func NewConflictDetector(pool DBTX) *ConflictDetector {
	return &ConflictDetector{pool: pool}
}

// Conflict represents a detected contradiction between two nodes.
type Conflict struct {
	ID           int64
	NodeAID      string
	NodeBID      string
	Similarity   float64
	NodeAValue   map[string]interface{}
	NodeBValue   map[string]interface{}
	AttributePath string
	Resolution   string
	WinnerID     string
	DetectedAt   string
}

// DetectConflicts finds all conflicts in the graph.
// Uses pgvector similarity >= 0.85 to find candidates, then checks for contradictory attributes.
func (cd *ConflictDetector) DetectConflicts(ctx context.Context) ([]Conflict, error) {
	// Step 1: Find high-similarity node pairs via pgvector.
	// Pairs must share a workspace: memories in different tenants can't
	// contradict each other, and resolving such a "conflict" supersedes a
	// node in another tenant's recall.
	rows, err := cd.pool.Query(ctx, `
		SELECT
			a.id AS node_a_id,
			b.id AS node_b_id,
			1 - (na.embedding <=> nb.embedding) AS similarity
		FROM nodes a
		JOIN node_embeddings na ON na.node_id = a.id
		JOIN nodes b ON b.id != a.id AND b.valid_to IS NULL AND b.workspace_name = a.workspace_name
		JOIN node_embeddings nb ON nb.node_id = b.id
		WHERE a.valid_to IS NULL
		  AND 1 - (na.embedding <=> nb.embedding) >= 0.85
		  AND NOT EXISTS (
			SELECT 1 FROM conflicts c
			WHERE (c.node_a_id = a.id AND c.node_b_id = b.id)
			   OR (c.node_a_id = b.id AND c.node_b_id = a.id)
		  )
		ORDER BY similarity DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("find similar pairs: %w", err)
	}
	defer rows.Close()

	var candidates []struct {
		NodeAID    string
		NodeBID    string
		Similarity float64
	}
	for rows.Next() {
		var c struct {
			NodeAID    string
			NodeBID    string
			Similarity float64
		}
		if err := rows.Scan(&c.NodeAID, &c.NodeBID, &c.Similarity); err != nil {
			slog.Debug("conflict candidate scan error", "error", err)
			continue
		}
		candidates = append(candidates, c)
	}

	// Step 2: Check each candidate for contradictory attributes
	var conflicts []Conflict
	for _, candidate := range candidates {
		conflict, err := cd.checkContradiction(ctx, candidate.NodeAID, candidate.NodeBID, candidate.Similarity)
		if err != nil {
			slog.Debug("contradiction check failed", "node_a", candidate.NodeAID, "node_b", candidate.NodeBID, "error", err)
			continue
		}
		if conflict != nil {
			conflicts = append(conflicts, *conflict)
		}
	}

	return conflicts, nil
}

// checkContradiction examines two nodes for contradictory attributes.
// Currently checks: status, confidence, category, boolean flags in metadata.
func (cd *ConflictDetector) checkContradiction(ctx context.Context, nodeAID, nodeBID string, similarity float64) (*Conflict, error) {
	// Get both nodes' data
	var aLabel, bLabel string
	var aMetadata, bMetadata map[string]interface{}
	
	err := cd.pool.QueryRow(ctx, `
		SELECT label, metadata FROM nodes WHERE id = $1 AND valid_to IS NULL
	`, nodeAID).Scan(&aLabel, &aMetadata)
	if err != nil {
		return nil, err
	}
	
	err = cd.pool.QueryRow(ctx, `
		SELECT label, metadata FROM nodes WHERE id = $1 AND valid_to IS NULL
	`, nodeBID).Scan(&bLabel, &bMetadata)
	if err != nil {
		return nil, err
	}

	// Check for contradictions in metadata
	attributePath, aValue, bValue := cd.findContradiction(aMetadata, bMetadata)
	if attributePath == "" {
		// Check for status contradictions in labels
		attributePath, aValue, bValue = cd.findLabelContradiction(aLabel, bLabel)
	}
	
	if attributePath == "" {
		return nil, nil // No contradiction found
	}

	return &Conflict{
		NodeAID:       nodeAID,
		NodeBID:       nodeBID,
		Similarity:    similarity,
		NodeAValue:    map[string]interface{}{attributePath: aValue},
		NodeBValue:    map[string]interface{}{attributePath: bValue},
		AttributePath: attributePath,
		Resolution:    "open",
	}, nil
}

// findContradiction looks for contradictory keys in metadata.
func (cd *ConflictDetector) findContradiction(a, b map[string]interface{}) (string, interface{}, interface{}) {
	// Check for status contradictions
	aStatus, aHasStatus := a["status"]
	bStatus, bHasStatus := b["status"]
	if aHasStatus && bHasStatus {
		if cd.isContradictoryStatus(fmt.Sprint(aStatus), fmt.Sprint(bStatus)) {
			return "status", aStatus, bStatus
		}
	}

	// Check for confidence contradictions
	aConf, aHasConf := a["confidence"]
	bConf, bHasConf := b["confidence"]
	if aHasConf && bHasConf {
		// Large confidence difference = contradiction
		if aConfFloat, ok := aConf.(float64); ok {
			if bConfFloat, ok := bConf.(float64); ok {
				if abs(aConfFloat-bConfFloat) > 0.5 {
					return "confidence", aConf, bConf
				}
			}
		}
	}

	// Check for boolean flags
	for key, aVal := range a {
		if bVal, ok := b[key]; ok {
			if aBool, ok := aVal.(bool); ok {
				if bBool, ok := bVal.(bool); ok {
					if aBool != bBool {
						return key, aVal, bVal
					}
				}
			}
		}
	}

	return "", nil, nil
}

// findLabelContradiction checks for status words in labels.
func (cd *ConflictDetector) findLabelContradiction(aLabel, bLabel string) (string, interface{}, interface{}) {
	aLower := strings.ToLower(aLabel)
	bLower := strings.ToLower(bLabel)

	// Check for explicit status contradictions
	if strings.Contains(aLower, "confirmed") && strings.Contains(bLower, "refuted") {
		return "label_status", "confirmed", "refuted"
	}
	if strings.Contains(aLower, "true") && strings.Contains(bLower, "false") {
		return "label_truth", "true", "false"
	}
	if strings.Contains(aLower, "success") && strings.Contains(bLower, "failure") {
		return "label_outcome", "success", "failure"
	}
	if strings.Contains(aLower, "active") && strings.Contains(bLower, "deprecated") {
		return "label_state", "active", "deprecated"
	}

	return "", nil, nil
}

// isContradictoryStatus checks if two statuses are contradictory.
func (cd *ConflictDetector) isContradictoryStatus(a, b string) bool {
	contradictoryPairs := [][2]string{
		{"confirmed", "refuted"},
		{"supported", "contradicted"},
		{"active", "inactive"},
		{"true", "false"},
		{"success", "failure"},
		{"valid", "invalid"},
		{"open", "closed"},
		{"approved", "rejected"},
	}
	
	for _, pair := range contradictoryPairs {
		if (a == pair[0] && b == pair[1]) || (a == pair[1] && b == pair[0]) {
			return true
		}
	}
	return false
}

// SaveConflict persists a detected conflict to the database.
func (cd *ConflictDetector) SaveConflict(ctx context.Context, c *Conflict) error {
	_, err := cd.pool.Exec(ctx, `
		INSERT INTO conflicts (node_a_id, node_b_id, similarity, node_a_value, node_b_value, attribute_path, resolution, detected_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'conflict_engine_v1')
		ON CONFLICT (node_a_id, node_b_id, attribute_path) DO NOTHING
	`, c.NodeAID, c.NodeBID, c.Similarity, c.NodeAValue, c.NodeBValue, c.AttributePath, c.Resolution)
	return err
}

// ResolveConflict marks a conflict as resolved with a winner.
func (cd *ConflictDetector) ResolveConflict(ctx context.Context, conflictID int64, resolution, winnerID string) error {
	_, err := cd.pool.Exec(ctx, `
		UPDATE conflicts
		SET resolution = $1, winner_id = $2, resolved_at = now()
		WHERE id = $3
	`, resolution, winnerID, conflictID)
	return err
}

// GetOpenConflicts returns all unresolved conflicts.
func (cd *ConflictDetector) GetOpenConflicts(ctx context.Context) ([]Conflict, error) {
	rows, err := cd.pool.Query(ctx, `
		SELECT id, node_a_id, node_b_id, similarity, node_a_value, node_b_value, attribute_path, resolution, winner_id, detected_at
		FROM conflicts
		WHERE resolution IS NULL OR resolution = 'open'
		ORDER BY similarity DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conflicts []Conflict
	for rows.Next() {
		var c Conflict
		var winnerID *string
		if err := rows.Scan(&c.ID, &c.NodeAID, &c.NodeBID, &c.Similarity, &c.NodeAValue, &c.NodeBValue, &c.AttributePath, &c.Resolution, &winnerID, &c.DetectedAt); err != nil {
			continue
		}
		if winnerID != nil {
			c.WinnerID = *winnerID
		}
		conflicts = append(conflicts, c)
	}
	return conflicts, nil
}

// SupersedeNode marks a node as superseded by a newer version.
// All steps run in one transaction when backed by a pool: the previous
// three-statement version could fail after the loser was already
// soft-deleted (edge redirect hitting the unique constraint), leaving live
// edges dangling on a dead node with no rollback.
func (cd *ConflictDetector) SupersedeNode(ctx context.Context, oldNodeID, newNodeID, reason string) error {
	if pool, ok := cd.pool.(*pgxpool.Pool); ok {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin supersede tx: %w", err)
		}
		defer tx.Rollback(ctx)
		if err := cd.supersedeNodeTx(ctx, tx, oldNodeID, newNodeID, reason); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	// Already inside a caller-managed transaction (DBTX is a pgx.Tx).
	return cd.supersedeNodeTx(ctx, cd.pool, oldNodeID, newNodeID, reason)
}

func (cd *ConflictDetector) supersedeNodeTx(ctx context.Context, db DBTX, oldNodeID, newNodeID, reason string) error {
	// Create memory revision record (using 063 schema: superseded_id, superseding_id)
	_, err := db.Exec(ctx, `
		INSERT INTO memory_revisions (superseded_id, superseding_id, reason)
		VALUES ($1, $2, $3)
	`, oldNodeID, newNodeID, reason)
	if err != nil {
		return fmt.Errorf("create memory revision: %w", err)
	}

	// Redirect edges via insert-select + delete: a plain UPDATE violates
	// UNIQUE(source,target,type) whenever the winner already has the
	// equivalent edge. Old<->new edges are dropped, not redirected (they
	// would become self-loops).
	_, err = db.Exec(ctx, `
		INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight, metadata)
		SELECT workspace_name,
		       CASE WHEN source_id = $1 THEN $2 ELSE source_id END,
		       CASE WHEN target_id = $1 THEN $2 ELSE target_id END,
		       edge_type, weight, metadata
		FROM edges
		WHERE (source_id = $1 OR target_id = $1) AND valid_to IS NULL
		  AND source_id <> $2 AND target_id <> $2
		ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
	`, oldNodeID, newNodeID)
	if err != nil {
		return fmt.Errorf("redirect edges: %w", err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM edges WHERE source_id = $1 OR target_id = $1`, oldNodeID); err != nil {
		return fmt.Errorf("drop old edges: %w", err)
	}

	// Clean up the loser's recall surface: its embedding would keep
	// occupying vector-search candidate slots, and its dedup hash would
	// keep future saves deduping against a dead node.
	if _, err = db.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, oldNodeID); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	if _, err = db.Exec(ctx, `UPDATE node_hashes SET node_id = $2 WHERE node_id = $1`, oldNodeID, newNodeID); err != nil {
		return fmt.Errorf("repoint hashes: %w", err)
	}

	// Soft-delete the old node last
	_, err = db.Exec(ctx, `
		UPDATE nodes SET valid_to = now() WHERE id = $1 AND valid_to IS NULL
	`, oldNodeID)
	if err != nil {
		return fmt.Errorf("soft-delete old node: %w", err)
	}

	return nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
