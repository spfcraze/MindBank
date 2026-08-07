package dream

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
)

// GraphEmbedder creates graph-aware embeddings by incorporating neighbor context.
// Equivalent to DAE (Graph-Aware Embeddings) but using MindBank's naming.
type GraphEmbedder struct {
	pool DBTX
}

// NewGraphEmbedder creates a graph embedder.
func NewGraphEmbedder(pool DBTX) *GraphEmbedder {
	return &GraphEmbedder{pool: pool}
}

// GraphEmbedding represents a graph-aware embedding for a node.
type GraphEmbedding struct {
	NodeID      string
	Embedding   []float32
	NeighborIDs []string
	HopCount    int
}

// ComputeEmbedding creates a graph-aware embedding for a node.
// Combines the node's own embedding with weighted neighbor embeddings.
func (ge *GraphEmbedder) ComputeEmbedding(ctx context.Context, nodeID string, hopCount int) (*GraphEmbedding, error) {
	// Get the node's own embedding
	ownEmbedding, err := ge.getNodeEmbedding(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get node embedding: %w", err)
	}
	if ownEmbedding == nil {
		return nil, fmt.Errorf("no embedding for node %s", nodeID)
	}

	// Collect neighbors up to hopCount
	neighbors, neighborEmbeddings, err := ge.collectNeighbors(ctx, nodeID, hopCount)
	if err != nil {
		return nil, fmt.Errorf("collect neighbors: %w", err)
	}

	// Combine embeddings with decay weights
	combined := ge.combineEmbeddings(ownEmbedding, neighborEmbeddings, hopCount)

	// Save to database
	embeddingStr := ge.vectorToString(combined)
	_, err = ge.pool.Exec(ctx, `
		INSERT INTO graph_embeddings (node_id, embedding, neighbor_ids, hop_count, model_version)
		VALUES ($1, $2::vector, $3, $4, 'graph-nomic-v1')
		ON CONFLICT (node_id, hop_count, model_version) DO UPDATE
		SET embedding = $2::vector, neighbor_ids = $3, created_at = now()
	`, nodeID, embeddingStr, neighbors, hopCount)
	if err != nil {
		return nil, fmt.Errorf("save graph embedding: %w", err)
	}

	return &GraphEmbedding{
		NodeID:      nodeID,
		Embedding:   combined,
		NeighborIDs: neighbors,
		HopCount:    hopCount,
	}, nil
}

// getNodeEmbedding retrieves a node's base embedding.
func (ge *GraphEmbedder) getNodeEmbedding(ctx context.Context, nodeID string) ([]float32, error) {
	var embeddingText string
	err := ge.pool.QueryRow(ctx, `
		SELECT embedding::text
		FROM node_embeddings
		WHERE node_id = $1
		LIMIT 1
	`, nodeID).Scan(&embeddingText)
	if err != nil {
		return nil, err
	}
	return parseVector(embeddingText), nil
}

// collectNeighbors gathers neighbor embeddings up to specified hops.
func (ge *GraphEmbedder) collectNeighbors(ctx context.Context, nodeID string, hopCount int) ([]string, [][]float32, error) {
	visited := make(map[string]bool)
	visited[nodeID] = true

	var allNeighbors []string
	var allEmbeddings [][]float32
	currentLevel := []string{nodeID}

	for hop := 1; hop <= hopCount; hop++ {
		nextLevel := []string{}
		
		for _, currentID := range currentLevel {
			// Find neighbors
			rows, err := ge.pool.Query(ctx, `
				SELECT 
					CASE 
						WHEN e.source_id = $1 THEN e.target_id 
						ELSE e.source_id 
					END AS neighbor_id
				FROM edges e
				WHERE (e.source_id = $1 OR e.target_id = $1)
				  AND e.valid_to IS NULL
			`, currentID)
			if err != nil {
				continue
			}

			for rows.Next() {
				var neighborID string
				if err := rows.Scan(&neighborID); err != nil {
					continue
				}
				
				if !visited[neighborID] {
					visited[neighborID] = true
					nextLevel = append(nextLevel, neighborID)
					allNeighbors = append(allNeighbors, neighborID)
					
					// Get neighbor embedding
					emb, err := ge.getNodeEmbedding(ctx, neighborID)
					if err == nil && emb != nil {
						allEmbeddings = append(allEmbeddings, emb)
					}
				}
			}
			rows.Close()
		}
		
		currentLevel = nextLevel
		if len(currentLevel) == 0 {
			break
		}
	}

	return allNeighbors, allEmbeddings, nil
}

// combineEmbeddings merges the node's embedding with neighbor embeddings.
// Uses hop-based decay: closer neighbors get higher weight.
func (ge *GraphEmbedder) combineEmbeddings(own []float32, neighbors [][]float32, hopCount int) []float32 {
	if len(own) == 0 {
		return nil
	}

	result := make([]float32, len(own))
	copy(result, own)

	// Weight decay per hop
	decay := 0.5 // neighbors at hop 1 get 50% weight
	
	for _, neighbor := range neighbors {
		if len(neighbor) != len(own) {
			continue
		}
		
		for i := 0; i < len(own); i++ {
			result[i] += float32(decay) * neighbor[i]
		}
	}

	// Normalize
	ge.normalize(result)

	return result
}

// normalize L2-normalizes a vector.
func (ge *GraphEmbedder) normalize(v []float32) {
	norm := 0.0
	for i := 0; i < len(v); i++ {
		norm += float64(v[i] * v[i])
	}
	
	if norm == 0 {
		return
	}
	
	norm = math.Sqrt(norm)
	for i := 0; i < len(v); i++ {
		v[i] = v[i] / float32(norm)
	}
}

// vectorToString converts a float32 slice to a PostgreSQL vector string.
func (ge *GraphEmbedder) vectorToString(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%f", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// SearchWithGraphEmbedding searches using graph-aware embeddings.
func (ge *GraphEmbedder) SearchWithGraphEmbedding(ctx context.Context, query string, limit int) ([]struct {
	NodeID string
	Score  float64
	Label  string
}, error) {
	// Get query embedding
	var queryEmbeddingText string
	err := ge.pool.QueryRow(ctx, `
		SELECT embedding::text
		FROM node_embeddings
		WHERE content = $1
		LIMIT 1
	`, query).Scan(&queryEmbeddingText)
	queryEmbedding := parseVector(queryEmbeddingText)

	if err != nil || queryEmbedding == nil {
		return nil, fmt.Errorf("no embedding for query: %w", err)
	}

	// Search using graph embeddings
	queryEmbeddingStr := ge.vectorToString(queryEmbedding)
	rows, err := ge.pool.Query(ctx, `
		SELECT 
			ge.node_id,
			1 - (ge.embedding <=> $1::vector) AS score,
			n.label
		FROM graph_embeddings ge
		JOIN nodes n ON n.id = ge.node_id
		WHERE n.valid_to IS NULL
		ORDER BY ge.embedding <=> $1::vector
		LIMIT $2
	`, queryEmbeddingStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []struct {
		NodeID string
		Score  float64
		Label  string
	}
	for rows.Next() {
		var r struct {
			NodeID string
			Score  float64
			Label  string
		}
		if err := rows.Scan(&r.NodeID, &r.Score, &r.Label); err != nil {
			continue
		}
		results = append(results, r)
	}

	return results, nil
}

// BatchCompute computes graph embeddings for all nodes.
func (ge *GraphEmbedder) BatchCompute(ctx context.Context, hopCount int) (int, error) {
	// Get all nodes with embeddings
	rows, err := ge.pool.Query(ctx, `
		SELECT DISTINCT ne.node_id
		FROM node_embeddings ne
		JOIN nodes n ON n.id = ne.node_id
		WHERE n.valid_to IS NULL
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			continue
		}

		_, err := ge.ComputeEmbedding(ctx, nodeID, hopCount)
		if err != nil {
			slog.Debug("graph embedding compute failed", "node", nodeID, "error", err)
			continue
		}
		count++
	}

	return count, nil
}
