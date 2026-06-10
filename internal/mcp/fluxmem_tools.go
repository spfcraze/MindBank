package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// toolRefineConnectivity handles the refine_connectivity MCP tool.
// Directly queries the database for maximum performance (no HTTP hop).
func (s *Server) toolRefineConnectivity(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		NodeID   string `json:"node_id"`
		Feedback string `json:"feedback"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if req.NodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	feedback := strings.ToLower(strings.TrimSpace(req.Feedback))
	if feedback != "missing_context" && feedback != "too_much_noise" && feedback != "granularity_mismatch" {
		return nil, fmt.Errorf("feedback must be 'missing_context', 'too_much_noise', or 'granularity_mismatch'")
	}

	switch feedback {
	case "missing_context":
		return s.refineMissingContextDirect(ctx, req.NodeID)
	case "too_much_noise":
		return s.refineTooMuchNoiseDirect(ctx, req.NodeID)
	case "granularity_mismatch":
		return s.refineGranularityMismatchDirect(ctx, req.NodeID)
	}
	return nil, fmt.Errorf("unknown feedback type: %s", feedback)
}

// refineMissingContextDirect finds semantically similar unconnected nodes and creates edges.
func (s *Server) refineMissingContextDirect(ctx context.Context, nodeID string) (string, error) {
	// Get source embedding and namespace
	var sourceEmbeddingStr, sourceNS string
	err := s.pool.QueryRow(ctx, `
		SELECT ne.embedding::text, n.namespace
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.id = $1 AND n.valid_to IS NULL
	`, nodeID).Scan(&sourceEmbeddingStr, &sourceNS)
	if err != nil {
		return "", fmt.Errorf("node not found or has no embedding")
	}

	// Find top-5 unconnected nodes by similarity
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, n.namespace,
			ne.embedding <=> $1::vector AS distance
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.valid_to IS NULL
		  AND n.id != $2
		  AND n.namespace = $3
		  AND NOT EXISTS (
			SELECT 1 FROM edges
			WHERE (source_id = $2 AND target_id = n.id)
			   OR (source_id = n.id AND target_id = $2)
		  )
		ORDER BY distance ASC
		LIMIT 5
	`, sourceEmbeddingStr, nodeID, sourceNS)
	if err != nil {
		return "", fmt.Errorf("similarity search failed: %w", err)
	}
	defer rows.Close()

	type suggestion struct {
		NodeID   string
		Label    string
		Distance float64
		Created  bool
	}

	var suggestions []suggestion
	var createdCount int

	for rows.Next() {
		var sug suggestion
		var ns, nt string
		if err := rows.Scan(&sug.NodeID, &sug.Label, &nt, &ns, &sug.Distance); err != nil {
			continue
		}
		suggestions = append(suggestions, sug)

		// Create edge if similarity > 0.6 (distance < 0.4)
		if sug.Distance < 0.4 {
			_, err := s.pool.Exec(ctx, `
				INSERT INTO edges (source_id, target_id, edge_type, weight, metadata)
				VALUES ($1, $2, 'relates_to', $3, '{"auto_connected":true,"reason":"embedding_similarity"}')
				ON CONFLICT (source_id, target_id, edge_type) DO NOTHING
			`, nodeID, sug.NodeID, 1.0-sug.Distance)
			if err == nil {
				sug.Created = true
				createdCount++
			}
		}
	}

	if createdCount == 0 {
		return "No new connections found. The node is well-connected.", nil
	}
	return fmt.Sprintf("Created %d new connection(s) based on semantic similarity.", createdCount), nil
}

// refineTooMuchNoiseDirect prunes low-weight edges.
func (s *Server) refineTooMuchNoiseDirect(ctx context.Context, nodeID string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, weight
		FROM edges
		WHERE (source_id = $1 OR target_id = $1)
		  AND weight < 0.3
		ORDER BY weight ASC
		LIMIT 20
	`, nodeID)
	if err != nil {
		return "", fmt.Errorf("prune query failed: %w", err)
	}
	defer rows.Close()

	var prunedCount int
	for rows.Next() {
		var edgeID string
		var weight float64
		if err := rows.Scan(&edgeID, &weight); err != nil {
			continue
		}
		_, delErr := s.pool.Exec(ctx, `DELETE FROM edges WHERE id = $1`, edgeID)
		if delErr == nil {
			prunedCount++
		}
	}

	if prunedCount == 0 {
		return "No weak connections found. The node has clean edges.", nil
	}
	return fmt.Sprintf("Pruned %d weak connection(s).", prunedCount), nil
}

// refineGranularityMismatchDirect reviews content length.
func (s *Server) refineGranularityMismatchDirect(ctx context.Context, nodeID string) (string, error) {
	var content string
	err := s.pool.QueryRow(ctx, `
		SELECT content
		FROM nodes
		WHERE id = $1 AND valid_to IS NULL
	`, nodeID).Scan(&content)
	if err != nil {
		return "", fmt.Errorf("node not found")
	}

	contentLen := len(strings.TrimSpace(content))
	if contentLen < 50 {
		return fmt.Sprintf("Content is very short (%d chars) — consider expanding with execution details.", contentLen), nil
	} else if contentLen > 2000 {
		return fmt.Sprintf("Content is very long (%d chars) — consider abstracting into high-level summary.", contentLen), nil
	}
	return "Content length is appropriate. No granularity issues detected.", nil
}

// toolEvolution handles the evolution MCP tool.
// Directly queries the database for maximum performance.
func (s *Server) toolEvolution(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if req.NodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}

	// Get node info
	var label, nodeType string
	var importance float64
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT label, node_type::text, importance, created_at
		FROM nodes
		WHERE id = $1
	`, req.NodeID).Scan(&label, &nodeType, &importance, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("node not found")
	}

	// Query all versions
	rows, err := s.pool.Query(ctx, `
		SELECT access_count, created_at
		FROM nodes
		WHERE id = $1 OR predecessor_id = $1
		ORDER BY created_at ASC
	`, req.NodeID)
	if err != nil {
		return nil, fmt.Errorf("version query failed: %w", err)
	}
	defer rows.Close()

	var totalAccessCount int
	var versionCount int
	var firstCreatedAt time.Time

	for rows.Next() {
		var accessCount int
		var vCreatedAt time.Time
		if err := rows.Scan(&accessCount, &vCreatedAt); err != nil {
			continue
		}
		totalAccessCount += accessCount
		if versionCount == 0 {
			firstCreatedAt = vCreatedAt
		}
		versionCount++
	}

	// Compute metrics
	ageDays := int(time.Since(firstCreatedAt).Hours()/24) + 1
	successRate := float64(totalAccessCount) / float64(ageDays)

	maturityLevel := "volatile"
	if versionCount >= 3 && ageDays > 30 {
		maturityLevel = "stable"
	} else if versionCount >= 2 || ageDays > 7 {
		maturityLevel = "evolving"
	}

	evolutionScore := importance * successRate
	if versionCount > 1 {
		evolutionScore = evolutionScore / float64(versionCount)
	}

	return fmt.Sprintf(
		"Evolution analysis for '%s':\n"+
		"  Score: %.3f\n"+
		"  Maturity: %s\n"+
		"  Versions: %d\n"+
		"  Age: %d days",
		label, evolutionScore, maturityLevel, versionCount, ageDays,
	), nil
}

// toolClusterSessions handles the cluster_sessions MCP tool.
// Directly queries the database for maximum performance.
func (s *Server) toolClusterSessions(ctx context.Context, args json.RawMessage) (any, error) {
	var req struct {
		Namespace string  `json:"namespace,omitempty"`
		Threshold float64 `json:"threshold,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, err
	}
	if req.Namespace == "" {
		req.Namespace = "global"
	}
	if req.Threshold == 0 {
		req.Threshold = 0.85
	}

	// Query session nodes with embeddings
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.label, ne.embedding::text
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.node_type = 'session'
		  AND n.namespace = $1
		  AND n.valid_to IS NULL
		ORDER BY n.created_at DESC
		LIMIT 100
	`, req.Namespace)
	if err != nil {
		return nil, fmt.Errorf("session query failed: %w", err)
	}
	defer rows.Close()

	type sessionNode struct {
		ID        string
		Label     string
		Embedding []float64
	}

	var sessions []sessionNode
	for rows.Next() {
		var s sessionNode
		var embStr string
		if err := rows.Scan(&s.ID, &s.Label, &embStr); err != nil {
			continue
		}
		s.Embedding = parseVectorString(embStr)
		sessions = append(sessions, s)
	}

	if len(sessions) == 0 {
		return "No session clusters found.", nil
	}

	// Greedy clustering
	var clusters []struct {
		Label        string
		MemberCount  int
		MemberLabels []string
	}
	visited := make(map[string]bool)

	for _, s := range sessions {
		if visited[s.ID] {
			continue
		}

		memberLabels := []string{s.Label}
		visited[s.ID] = true

		for _, other := range sessions {
			if visited[other.ID] {
				continue
			}
			if cosineSimilarity(s.Embedding, other.Embedding) >= req.Threshold {
				memberLabels = append(memberLabels, other.Label)
				visited[other.ID] = true
			}
		}

		clusters = append(clusters, struct {
			Label        string
			MemberCount  int
			MemberLabels []string
		}{
			Label:        s.Label,
			MemberCount:  len(memberLabels),
			MemberLabels: memberLabels,
		})
	}

	if len(clusters) == 0 {
		return "No session clusters found.", nil
	}

	out := fmt.Sprintf("Found %d session cluster(s):\n", len(clusters))
	for i, c := range clusters {
		out += fmt.Sprintf("\n%d. %s (%d members)\n", i+1, c.Label, c.MemberCount)
		for _, label := range c.MemberLabels {
			out += fmt.Sprintf("   - %s\n", label)
		}
	}
	return out, nil
}

// parseVectorString parses a pgvector text representation.
func parseVectorString(s string) []float64 {
	s = strings.Trim(s, "[]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []float64
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, err := strconv.ParseFloat(p, 64); err == nil {
			result = append(result, v)
		}
	}
	return result
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// itoa is a helper to convert int to string.
func itoa(n int) string {
	return strconv.Itoa(n)
}
