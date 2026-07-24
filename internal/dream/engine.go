package dream

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is the interface for database operations within transactions.
type DBTX interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
}

// compile-time check that pgxpool.Pool implements DBTX
var _ DBTX = (*pgxpool.Pool)(nil)

// compile-time check that pgx.Tx implements DBTX
var _ DBTX = (pgx.Tx)(nil)

// compile-time check that *pgxpool.Conn implements DBTX
var _ DBTX = (*pgxpool.Conn)(nil)

// compile-time check that pgx.Conn implements DBTX
var _ DBTX = (*pgx.Conn)(nil)

// Engine performs MindBank's neural consolidation (sleep-inspired graph maintenance).
// It runs three phases: NREM (strengthen/prune), REM (bridge isolated), Insight (community detection).
type Engine struct {
	pool *pgxpool.Pool
}

// NewEngine creates a new dream consolidation engine.
func NewEngine(pool *pgxpool.Pool) *Engine {
	return &Engine{pool: pool}
}

// dreamCycleLockKey serializes dream cycles cluster-wide.
const dreamCycleLockKey = 0x4452454D // "DREM"

// Cycle runs a full dream consolidation cycle.
func (e *Engine) Cycle(ctx context.Context) (*CycleResult, error) {
	// Only one cycle at a time: a manual trigger racing the nightly cron
	// double-applies edge strengthening and creates duplicate cluster nodes.
	lockConn, err := e.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock conn: %w", err)
	}
	defer lockConn.Release()
	var gotLock bool
	if err := lockConn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", dreamCycleLockKey).Scan(&gotLock); err != nil {
		return nil, fmt.Errorf("acquire dream lock: %w", err)
	}
	if !gotLock {
		return nil, fmt.Errorf("a dream cycle is already running")
	}
	defer lockConn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", dreamCycleLockKey)

	// Use a transaction for the entire cycle to ensure atomicity
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	cycleID, err := e.startCycleTx(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("start cycle: %w", err)
	}

	result := &CycleResult{CycleID: cycleID}

	// Phase 1: NREM — strengthen active edges, prune dead ones
	slog.Info("dream cycle: NREM phase starting")
	nremResult, err := e.nremPhaseTx(ctx, tx)
	if err != nil {
		// Don't failCycle here - just rollback and return
		return nil, fmt.Errorf("nrem phase: %w", err)
	}
	result.NREM = nremResult

	// Phase 2: REM — bridge isolated memories
	slog.Info("dream cycle: REM phase starting")
	remResult, err := e.remPhaseTx(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("rem phase: %w", err)
	}
	result.REM = remResult

	// Phase 3: Insight — community detection and cluster materialization
	slog.Info("dream cycle: Insight phase starting")
	insightResult, err := e.insightPhaseTx(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("insight phase: %w", err)
	}
	result.Insight = insightResult

	// Complete cycle
	if err := e.completeCycleTx(ctx, tx, cycleID, result); err != nil {
		return nil, fmt.Errorf("complete cycle: %w", err)
	}

	// Commit the entire cycle atomically
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cycle: %w", err)
	}

	slog.Info("dream cycle completed",
		"cycle_id", cycleID,
		"edges_strengthened", nremResult.Strengthened,
		"edges_pruned", nremResult.Pruned,
		"edges_bridged", remResult.Bridged,
		"clusters_found", insightResult.ClustersFound,
	)

	return result, nil
}

// CycleResult captures the outcome of a dream consolidation cycle.
type CycleResult struct {
	CycleID int
	NREM    NREMResult
	REM     REMResult
	Insight InsightResult
}

// NREMResult captures NREM phase outcomes.
type NREMResult struct {
	Strengthened int
	Pruned       int
	Weakened     int
}

// REMResult captures REM phase outcomes.
type REMResult struct {
	Bridged int
	IsolatedChecked int
}

// InsightResult captures Insight phase outcomes.
type InsightResult struct {
	ClustersFound int
	ClusterNodesCreated int
}

// startCycleTx records the beginning of a dream cycle within a transaction.
func (e *Engine) startCycleTx(ctx context.Context, tx pgx.Tx) (int, error) {
	var cycleID int
	err := tx.QueryRow(ctx, `
		INSERT INTO dream_cycles (started_at, status)
		VALUES (now(), 'running')
		RETURNING id
	`).Scan(&cycleID)
	return cycleID, err
}

// failCycleTx marks a cycle as failed within a transaction.
func (e *Engine) failCycleTx(ctx context.Context, tx pgx.Tx, cycleID int, cycleErr error) {
	_, err := tx.Exec(ctx, `
		UPDATE dream_cycles
		SET status = 'failed', error_message = $1, completed_at = now()
		WHERE id = $2
	`, cycleErr.Error(), cycleID)
	if err != nil {
		slog.Error("failed to mark dream cycle as failed", "cycle_id", cycleID, "error", err)
	}
}

// completeCycleTx marks a cycle as completed with results within a transaction.
func (e *Engine) completeCycleTx(ctx context.Context, tx pgx.Tx, cycleID int, result *CycleResult) error {
	_, err := tx.Exec(ctx, `
		UPDATE dream_cycles
		SET status = 'completed',
		    completed_at = now(),
		    phase_nrem_count = $1,
		    phase_rem_count = $2,
		    phase_insight_count = $3,
		    edges_strengthened = $4,
		    edges_pruned = $5,
		    edges_bridged = $6,
		    clusters_found = $7
		WHERE id = $8
	`, result.NREM.Strengthened + result.NREM.Pruned + result.NREM.Weakened,
		result.REM.Bridged,
		result.Insight.ClustersFound,
		result.NREM.Strengthened,
		result.NREM.Pruned,
		result.REM.Bridged,
		result.Insight.ClustersFound,
		cycleID)
	return err
}

// startCycle records the beginning of a dream cycle.
func (e *Engine) startCycle(ctx context.Context) (int, error) {
	var cycleID int
	err := e.pool.QueryRow(ctx, `
		INSERT INTO dream_cycles (started_at, status)
		VALUES (now(), 'running')
		RETURNING id
	`).Scan(&cycleID)
	return cycleID, err
}

// failCycle marks a cycle as failed.
func (e *Engine) failCycle(ctx context.Context, cycleID int, cycleErr error) {
	_, err := e.pool.Exec(ctx, `
		UPDATE dream_cycles
		SET status = 'failed', error_message = $1, completed_at = now()
		WHERE id = $2
	`, cycleErr.Error(), cycleID)
	if err != nil {
		slog.Error("failed to mark dream cycle as failed", "cycle_id", cycleID, "error", err)
	}
}

// completeCycle marks a cycle as completed with results.
func (e *Engine) completeCycle(ctx context.Context, cycleID int, result *CycleResult) error {
	_, err := e.pool.Exec(ctx, `
		UPDATE dream_cycles
		SET status = 'completed',
		    completed_at = now(),
		    phase_nrem_count = $1,
		    phase_rem_count = $2,
		    phase_insight_count = $3,
		    edges_strengthened = $4,
		    edges_pruned = $5,
		    edges_bridged = $6,
		    clusters_found = $7
		WHERE id = $8
	`, result.NREM.Strengthened + result.NREM.Pruned + result.NREM.Weakened,
		result.REM.Bridged,
		result.Insight.ClustersFound,
		result.NREM.Strengthened,
		result.NREM.Pruned,
		result.REM.Bridged,
		result.Insight.ClustersFound,
		cycleID)
	return err
}

// nremPhase performs NREM consolidation: strengthen active edges, prune dead ones.
func (e *Engine) nremPhase(ctx context.Context, cycleID int) (NREMResult, error) {
	return e.nremPhaseTx(ctx, e.pool)
}

// nremPhaseTx performs NREM consolidation within a transaction.
func (e *Engine) nremPhaseTx(ctx context.Context, tx DBTX) (NREMResult, error) {
	result := NREMResult{}

	// Sample seeds using three-slice mix
	seeds, err := e.sampleForDreamTx(ctx, tx)
	if err != nil {
		return result, fmt.Errorf("sample seeds: %w", err)
	}

	slog.Info("nremPhase: processing seeds", "count", len(seeds))

	for _, seedID := range seeds {
		// Spreading activation: find related cluster via BFS
		cluster, err := e.spreadingActivationTx(ctx, tx, seedID, 3)
		if err != nil {
			slog.Error("spreading activation failed", "seed", seedID, "error", err)
			continue
		}

		if len(cluster) == 0 {
			slog.Debug("empty cluster for seed", "seed", seedID)
			continue
		}

		slog.Debug("processing cluster", "seed", seedID, "cluster_size", len(cluster))

		// Strengthen edges inside cluster
		strengthened, err := e.strengthenClusterEdgesTx(ctx, tx, cluster)
		if err != nil {
			slog.Error("strengthen cluster failed", "seed", seedID, "cluster_size", len(cluster), "error", err)
			continue
		}
		result.Strengthened += strengthened

		// Weaken edges outside cluster
		weakened, err := e.weakenOutsideEdgesTx(ctx, tx, seedID, cluster)
		if err != nil {
			slog.Error("weaken outside failed", "seed", seedID, "cluster_size", len(cluster), "error", err)
			continue
		}
		result.Weakened += weakened
	}

	// Prune dead edges (salience < 0.05)
	slog.Info("nremPhase: pruning dead edges")
	pruned, err := e.pruneDeadEdgesTx(ctx, tx, 0.05)
	if err != nil {
		return result, fmt.Errorf("prune dead edges: %w", err)
	}
	result.Pruned = pruned

	return result, nil
}

// remPhase performs REM bridge discovery: connect isolated memories.
func (e *Engine) remPhase(ctx context.Context, cycleID int) (REMResult, error) {
	return e.remPhaseTx(ctx, e.pool)
}

// remPhaseTx performs REM bridge discovery within a transaction.
func (e *Engine) remPhaseTx(ctx context.Context, tx DBTX) (REMResult, error) {
	result := REMResult{}

	// Find isolated nodes (no edges or only weak edges)
	slog.Info("remPhase: finding isolated nodes")
	isolated, err := e.findIsolatedNodesTx(ctx, tx, 50)
	if err != nil {
		slog.Error("findIsolatedNodes failed", "error", err)
		return result, fmt.Errorf("find isolated: %w", err)
	}
	result.IsolatedChecked = len(isolated)
	slog.Info("remPhase: found isolated nodes", "count", len(isolated))

	for _, node := range isolated {
		// Find similar unconnected nodes in the SAME workspace: bridging
		// across workspaces leaks one tenant's memories into another's
		// recall traversals.
		matches, err := e.findSimilarUnconnectedTx(ctx, tx, node.ID, node.Workspace, 0.3, 3)
		if err != nil {
			// Fail the phase: inside a transaction, the first error aborts
			// the tx and every subsequent statement fails anyway.
			return result, fmt.Errorf("find similar for %s: %w", node.ID, err)
		}

		for _, match := range matches {
			// Create bridge edge in the node's own workspace
			_, err := tx.Exec(ctx, `
				INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight, salience, created_at)
				VALUES ($4, $1, $2, 'relates_to', $3, 0.5, now())
				ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
			`, node.ID, match.ID, match.Similarity*0.3, node.Workspace)
			if err != nil {
				return result, fmt.Errorf("bridge %s -> %s: %w", node.ID, match.ID, err)
			}
			result.Bridged++
		}
	}

	return result, nil
}

// insightPhase performs Insight community detection and cluster materialization.
func (e *Engine) insightPhase(ctx context.Context, cycleID int) (InsightResult, error) {
	return e.insightPhaseTx(ctx, e.pool)
}

// insightPhaseTx performs Insight community detection within a transaction.
func (e *Engine) insightPhaseTx(ctx context.Context, tx DBTX) (InsightResult, error) {
	result := InsightResult{}

	// Find connected components via BFS
	slog.Info("insightPhase: finding connected components")
	components, err := e.findConnectedComponentsTx(ctx, tx, 4)
	if err != nil {
		slog.Error("findConnectedComponents failed", "error", err)
		return result, fmt.Errorf("find components: %w", err)
	}
	result.ClustersFound = len(components)
	slog.Info("insightPhase: found components", "count", len(components))

	for _, component := range components {
		if len(component) < 4 {
			continue
		}

		// Idempotency: skip components an existing cluster node already
		// covers — without this, every nightly cycle spawned a fresh
		// "Cluster: ..." node for the same component, unboundedly.
		var alreadyClustered bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM edges e
				JOIN nodes c ON c.id = e.source_id AND c.valid_to IS NULL
				WHERE c.metadata->>'derived' = 'true'
				  AND e.edge_type = 'contains' AND e.valid_to IS NULL
				  AND e.target_id = ANY($1)
				GROUP BY c.id
				HAVING COUNT(*) >= $2
			)
		`, component, (len(component)+1)/2).Scan(&alreadyClustered)
		if err != nil {
			return result, fmt.Errorf("check existing cluster: %w", err)
		}
		if alreadyClustered {
			continue
		}

		// Create derived cluster node
		clusterID, workspace, err := e.createClusterNodeTx(ctx, tx, component)
		if err != nil {
			return result, fmt.Errorf("create cluster (size %d): %w", len(component), err)
		}
		result.ClusterNodesCreated++

		// Link cluster node to all members
		for _, memberID := range component {
			_, err := tx.Exec(ctx, `
				INSERT INTO edges (workspace_name, source_id, target_id, edge_type, weight, salience, created_at)
				VALUES ($3, $1, $2, 'contains', 0.8, 1.0, now())
				ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
			`, clusterID, memberID, workspace)
			if err != nil {
				return result, fmt.Errorf("cluster edge %s -> %s: %w", clusterID, memberID, err)
			}
		}
	}

	return result, nil
}

// sampleForDream uses three-slice mixing: 50% recent, 30% random, 20% low-salience rescue.
func (e *Engine) sampleForDream(ctx context.Context, total int) ([]string, error) {
	return e.sampleForDreamTx(ctx, e.pool)
}

// sampleForDreamTx uses three-slice mixing within a transaction.
func (e *Engine) sampleForDreamTx(ctx context.Context, tx DBTX) ([]string, error) {
	recentCount := int(float64(100) * 0.5)
	randomCount := int(float64(100) * 0.3)
	rescueCount := 100 - recentCount - randomCount

	var seeds []string
	seen := make(map[string]bool)

	// Recent slice
	recentRows, err := tx.Query(ctx, `
		SELECT id FROM nodes
		WHERE valid_to IS NULL
		ORDER BY created_at DESC
		LIMIT $1
	`, recentCount)
	if err != nil {
		return nil, fmt.Errorf("recent sample: %w", err)
	}
	defer recentRows.Close()
	for recentRows.Next() {
		var id string
		if err := recentRows.Scan(&id); err != nil {
			slog.Debug("sample scan error", "slice", "recent", "error", err)
			continue
		}
		if !seen[id] {
			seeds = append(seeds, id)
			seen[id] = true
		}
	}

	// Random slice
	randomRows, err := tx.Query(ctx, `
		SELECT id FROM nodes
		WHERE valid_to IS NULL
		ORDER BY random()
		LIMIT $1
	`, randomCount)
	if err != nil {
		return nil, fmt.Errorf("random sample: %w", err)
	}
	defer randomRows.Close()
	for randomRows.Next() {
		var id string
		if err := randomRows.Scan(&id); err != nil {
			slog.Debug("sample scan error", "slice", "random", "error", err)
			continue
		}
		if !seen[id] {
			seeds = append(seeds, id)
			seen[id] = true
		}
	}

	// Rescue slice (lowest importance + access_count)
	rescueRows, err := tx.Query(ctx, `
		SELECT id FROM nodes
		WHERE valid_to IS NULL
		ORDER BY importance ASC NULLS LAST, access_count ASC NULLS LAST
		LIMIT $1
	`, rescueCount)
	if err != nil {
		return nil, fmt.Errorf("rescue sample: %w", err)
	}
	defer rescueRows.Close()
	for rescueRows.Next() {
		var id string
		if err := rescueRows.Scan(&id); err != nil {
			slog.Debug("sample scan error", "slice", "rescue", "error", err)
			continue
		}
		if !seen[id] {
			seeds = append(seeds, id)
			seen[id] = true
		}
	}

	return seeds, nil
}

// spreadingActivation finds related nodes via BFS from a seed.
func (e *Engine) spreadingActivation(ctx context.Context, seedID string, maxDepth int) ([]string, error) {
	return e.spreadingActivationTx(ctx, e.pool, seedID, maxDepth)
}

// spreadingActivationTx finds related nodes via BFS within a transaction.
func (e *Engine) spreadingActivationTx(ctx context.Context, tx DBTX, seedID string, maxDepth int) ([]string, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE activation AS (
			SELECT target_id AS id, 1 AS depth
			FROM edges
			WHERE source_id = $1 AND valid_to IS NULL
			UNION
			SELECT source_id AS id, 1 AS depth
			FROM edges
			WHERE target_id = $1 AND valid_to IS NULL
			UNION ALL
			SELECT 
				CASE WHEN e.source_id = a.id THEN e.target_id ELSE e.source_id END,
				a.depth + 1
			FROM edges e
			JOIN activation a ON (e.source_id = a.id OR e.target_id = a.id)
			WHERE a.depth < $2
			  AND e.valid_to IS NULL
		)
		SELECT DISTINCT id FROM activation
	`, seedID, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cluster []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Debug("spreading activation scan error", "error", err)
			continue
		}
		cluster = append(cluster, id)
	}
	return cluster, nil
}

// strengthenClusterEdges increases weight for edges inside a cluster.
func (e *Engine) strengthenClusterEdges(ctx context.Context, cluster []string) (int, error) {
	return e.strengthenClusterEdgesTx(ctx, e.pool, cluster)
}

// strengthenClusterEdgesTx increases weight for edges inside a cluster within a transaction.
func (e *Engine) strengthenClusterEdgesTx(ctx context.Context, tx DBTX, cluster []string) (int, error) {
	if len(cluster) == 0 {
		return 0, nil
	}

	// Build placeholders for IN clause
	if len(cluster) == 0 {
		return 0, nil
	}
	
	placeholders := make([]string, len(cluster))
	args := make([]interface{}, len(cluster))
	for i, id := range cluster {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	
	// Build IN clause with proper formatting
	inClause := "(" + placeholders[0] + ")"
	if len(placeholders) > 1 {
		inClause = "(" + placeholders[0]
		for i := 1; i < len(placeholders); i++ {
			inClause += "," + placeholders[i]
		}
		inClause += ")"
	}

	// Strengthen edges where both endpoints are in cluster
	query := fmt.Sprintf(`
		UPDATE edges
		SET weight = LEAST(weight + 0.05, 2.0),
		    salience = LEAST(salience + 0.05, 2.0),
		    last_accessed = now()
		WHERE valid_to IS NULL
		  AND source_id IN %s
		  AND target_id IN %s
	`, inClause, inClause)

	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// weakenOutsideEdges decreases weight for edges from seed to outside cluster.
func (e *Engine) weakenOutsideEdges(ctx context.Context, seedID string, cluster []string) (int, error) {
	return e.weakenOutsideEdgesTx(ctx, e.pool, seedID, cluster)
}

// weakenOutsideEdgesTx decreases weight for edges from seed to outside cluster within a transaction.
func (e *Engine) weakenOutsideEdgesTx(ctx context.Context, tx DBTX, seedID string, cluster []string) (int, error) {
	if len(cluster) == 0 {
		return 0, nil
	}
	
	placeholders := make([]string, len(cluster))
	args := make([]interface{}, len(cluster)+1)
	args[0] = seedID
	for i, id := range cluster {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}
	
	// Build IN clause with proper formatting
	inClause := "(" + placeholders[0] + ")"
	if len(placeholders) > 1 {
		inClause = "(" + placeholders[0]
		for i := 1; i < len(placeholders); i++ {
			inClause += "," + placeholders[i]
		}
		inClause += ")"
	}

	query := fmt.Sprintf(`
		UPDATE edges
		SET weight = GREATEST(weight - 0.01, 0.01),
		    salience = GREATEST(salience - 0.01, 0.01)
		WHERE valid_to IS NULL
		  AND (source_id = $1 OR target_id = $1)
		  AND source_id NOT IN %s
		  AND target_id NOT IN %s
	`, inClause, inClause)

	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// pruneDeadEdges removes edges with salience below threshold.
func (e *Engine) pruneDeadEdges(ctx context.Context, threshold float64) (int, error) {
	return e.pruneDeadEdgesTx(ctx, e.pool, threshold)
}

// pruneDeadEdgesTx removes edges with salience below threshold within a transaction.
func (e *Engine) pruneDeadEdgesTx(ctx context.Context, tx DBTX, threshold float64) (int, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE edges
		SET valid_to = now()
		WHERE valid_to IS NULL AND salience < $1
	`, threshold)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// findIsolatedNodes finds nodes with no edges or only weak edges.
func (e *Engine) findIsolatedNodes(ctx context.Context, limit int) ([]isolatedNode, error) {
	return e.findIsolatedNodesTx(ctx, e.pool, limit)
}

// isolatedNode is a node with no (or only weak) edges, plus the tenant info
// needed to keep bridging within its workspace.
type isolatedNode struct {
	ID        string
	Label     string
	Namespace string
	Workspace string
}

// findIsolatedNodesTx finds nodes with no edges or only weak edges within a transaction.
func (e *Engine) findIsolatedNodesTx(ctx context.Context, tx DBTX, limit int) ([]isolatedNode, error) {
	rows, err := tx.Query(ctx, `
		SELECT n.id, n.label, n.namespace, n.workspace_name
		FROM nodes n
		LEFT JOIN edges e ON (n.id = e.source_id OR n.id = e.target_id) AND e.valid_to IS NULL
		WHERE n.valid_to IS NULL
		GROUP BY n.id, n.label, n.namespace, n.workspace_name
		HAVING COUNT(e.id) = 0 OR MAX(e.weight) < 0.1
		ORDER BY n.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []isolatedNode
	for rows.Next() {
		var n isolatedNode
		if err := rows.Scan(&n.ID, &n.Label, &n.Namespace, &n.Workspace); err != nil {
			slog.Debug("find isolated scan error", "error", err)
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// SimilarMatch represents a similar unconnected node.
type SimilarMatch struct {
	ID         string
	Similarity float32
}

// findSimilarUnconnected finds semantically similar nodes not already connected.
func (e *Engine) findSimilarUnconnected(ctx context.Context, nodeID, workspace string, minSimilarity float64, limit int) ([]SimilarMatch, error) {
	return e.findSimilarUnconnectedTx(ctx, e.pool, nodeID, workspace, minSimilarity, limit)
}

// findSimilarUnconnectedTx finds semantically similar nodes not already connected within a transaction.
// Matches are restricted to the given workspace so consolidation never links
// memories across tenant boundaries.
func (e *Engine) findSimilarUnconnectedTx(ctx context.Context, tx DBTX, nodeID, workspace string, minSimilarity float64, limit int) ([]SimilarMatch, error) {
	rows, err := tx.Query(ctx, `
		SELECT n.id, 1 - (ne.embedding <=> seed.embedding) AS similarity
		FROM nodes n
		JOIN node_embeddings ne ON ne.node_id = n.id
		CROSS JOIN (SELECT embedding FROM node_embeddings WHERE node_id = $1) seed
		WHERE n.valid_to IS NULL
		  AND n.id != $1
		  AND n.workspace_name = $4
		  AND 1 - (ne.embedding <=> seed.embedding) > $2
		  AND NOT EXISTS (
			SELECT 1 FROM edges e
			WHERE e.valid_to IS NULL
			  AND ((e.source_id = $1 AND e.target_id = n.id)
			   OR (e.source_id = n.id AND e.target_id = $1))
		  )
		ORDER BY ne.embedding <=> seed.embedding
		LIMIT $3
	`, nodeID, minSimilarity, limit, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []SimilarMatch
	for rows.Next() {
		var m SimilarMatch
		if err := rows.Scan(&m.ID, &m.Similarity); err != nil {
			slog.Debug("find similar scan error", "error", err)
			continue
		}
		matches = append(matches, m)
	}
	return matches, nil
}

// findConnectedComponents finds connected components in the graph.
func (e *Engine) findConnectedComponents(ctx context.Context, minSize int) ([][]string, error) {
	return e.findConnectedComponentsTx(ctx, e.pool, minSize)
}

// findConnectedComponentsTx finds connected components in the graph within a transaction.
// Uses a non-recursive approach to avoid infinite loops with cyclic graphs.
func (e *Engine) findConnectedComponentsTx(ctx context.Context, tx DBTX, minSize int) ([][]string, error) {
	// Use a two-pass approach: first get all node pairs, then build components.
	// Both endpoints must be live, non-derived nodes: including derived
	// cluster nodes let each cycle's cluster join the component and get
	// re-clustered by the next cycle (unbounded nesting), and edges to
	// invalidated nodes inflated component sizes.
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT
			LEAST(e.source_id, e.target_id) AS n1,
			GREATEST(e.source_id, e.target_id) AS n2
		FROM edges e
		JOIN nodes s ON s.id = e.source_id AND s.valid_to IS NULL AND coalesce(s.metadata->>'derived','') <> 'true'
		JOIN nodes t ON t.id = e.target_id AND t.valid_to IS NULL AND coalesce(t.metadata->>'derived','') <> 'true'
		WHERE e.valid_to IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Build adjacency list
	adj := make(map[string][]string)
	for rows.Next() {
		var n1, n2 string
		if err := rows.Scan(&n1, &n2); err != nil {
			continue
		}
		adj[n1] = append(adj[n1], n2)
		adj[n2] = append(adj[n2], n1)
	}

	// BFS to find components
	visited := make(map[string]bool)
	var components [][]string
	
	for node := range adj {
		if visited[node] {
			continue
		}
		
		component := []string{}
		queue := []string{node}
		visited[node] = true
		
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			component = append(component, current)
			
			for _, neighbor := range adj[current] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
				}
			}
		}
		
		if len(component) >= minSize {
			components = append(components, component)
		}
	}
	
	return components, nil
}

// createClusterNode creates a derived cluster summary node.
func (e *Engine) createClusterNode(ctx context.Context, members []string) (string, error) {
	id, _, err := e.createClusterNodeTx(ctx, e.pool, members)
	return id, err
}

// createClusterNodeTx creates a derived cluster summary node within a transaction.
// Returns the cluster node ID and the workspace it was created in (derived
// from its members rather than hardcoded).
func (e *Engine) createClusterNodeTx(ctx context.Context, tx DBTX, members []string) (string, string, error) {
	if len(members) == 0 {
		return "", "", fmt.Errorf("empty member list")
	}

	// Get member labels for cluster name
	placeholders := make([]string, len(members))
	args := make([]interface{}, len(members))
	for i, id := range members {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := placeholders[0]
	for i := 1; i < len(placeholders); i++ {
		inClause += "," + placeholders[i]
	}

	var labels []string
	workspace := ""
	labelRows, err := tx.Query(ctx, fmt.Sprintf(`
		SELECT label, workspace_name FROM nodes
		WHERE id IN (%s) AND valid_to IS NULL
	`, inClause), args...)
	if err != nil {
		return "", "", err
	}
	defer labelRows.Close()
	for labelRows.Next() {
		var label, ws string
		if err := labelRows.Scan(&label, &ws); err != nil {
			slog.Debug("create cluster label scan error", "error", err)
			continue
		}
		labels = append(labels, label)
		if workspace == "" {
			workspace = ws
		}
	}

	if len(labels) == 0 {
		return "", "", fmt.Errorf("no valid labels found for cluster")
	}

	clusterLabel := "Cluster: " + labels[0]
	if len(labels) > 1 {
		clusterLabel += fmt.Sprintf(" +%d more", len(labels)-1)
	}

	content := fmt.Sprintf("Derived cluster containing %d nodes: %s", len(members), labels)

	var clusterID string
	metadata := fmt.Sprintf(`{"derived": true, "cluster_size": %d}`, len(members))
	err = tx.QueryRow(ctx, `
		INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, importance, metadata)
		VALUES ($5, 'derived', $1, 'concept', $2, $3, 0.5, $4::jsonb)
		RETURNING id
	`, clusterLabel, content, "Auto-generated cluster from dream consolidation", metadata, workspace).Scan(&clusterID)
	if err != nil {
		return "", "", err
	}

	return clusterID, workspace, nil
}

// GetCycleStatus returns the status of the most recent dream cycle.
func (e *Engine) GetCycleStatus(ctx context.Context) (map[string]interface{}, error) {
	row := e.pool.QueryRow(ctx, `
		SELECT id, started_at, completed_at, status,
		       edges_strengthened, edges_pruned, edges_bridged, clusters_found
		FROM dream_cycles
		ORDER BY started_at DESC
		LIMIT 1
	`)

	var id int
	var started, completed time.Time
	var cycleStatus string
	var strengthened, pruned, bridged, clusters int
	var completedPtr *time.Time

	err := row.Scan(&id, &started, &completedPtr, &cycleStatus,
		&strengthened, &pruned, &bridged, &clusters)
	if err != nil {
		if err == pgx.ErrNoRows {
			return map[string]interface{}{
				"status": "never_run",
				"message": "No dream cycles have been run yet",
			}, nil
		}
		return nil, err
	}

	if completedPtr != nil {
		completed = *completedPtr
	}

	return map[string]interface{}{
		"cycle_id":           id,
		"started_at":         started,
		"completed_at":       completed,
		"status":             cycleStatus,
		"edges_strengthened": strengthened,
		"edges_pruned":       pruned,
		"edges_bridged":      bridged,
		"clusters_found":     clusters,
	}, nil
}

// GetCycleHistory returns the last N dream cycles.
func (e *Engine) GetCycleHistory(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := e.pool.Query(ctx, `
		SELECT id, started_at, completed_at, status,
		       edges_strengthened, edges_pruned, edges_bridged, clusters_found,
		       error_message
		FROM dream_cycles
		ORDER BY started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var id int
		var started time.Time
		var completedPtr *time.Time
		var status string
		var strengthened, pruned, bridged, clusters int
		var errorMsg *string

		if err := rows.Scan(&id, &started, &completedPtr, &status,
			&strengthened, &pruned, &bridged, &clusters, &errorMsg); err != nil {
			continue
		}

		cycle := map[string]interface{}{
			"cycle_id":           id,
			"started_at":         started,
			"status":             status,
			"edges_strengthened": strengthened,
			"edges_pruned":       pruned,
			"edges_bridged":      bridged,
			"clusters_found":     clusters,
		}
		if completedPtr != nil {
			cycle["completed_at"] = *completedPtr
		}
		if errorMsg != nil {
			cycle["error"] = *errorMsg
		}
		history = append(history, cycle)
	}
	return history, nil
}
