package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// sessionNode represents a session node for clustering
type sessionNode struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Content   string    `json:"content"`
	Embedding []float64 `json:"embedding"`
}

// ClusterSessions handles POST /api/v1/analyze/cluster-sessions
// Groups session nodes by embedding similarity into episodic clusters.
func (h *AnalyzeHandler) ClusterSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Namespace string  `json:"namespace"`
		Threshold float64 `json:"threshold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "global"
	}

	threshold := req.Threshold
	if threshold == 0 {
		threshold = 0.85 // default cosine similarity threshold
	}

	ctx := r.Context()

	// Query all session nodes with embeddings in the namespace
	// Limit to 100 sessions for performance (O(n²) clustering)
	rows, err := h.pool.Query(ctx, `
		SELECT n.id, n.label, n.content, ne.embedding::text
		FROM nodes n
		JOIN node_embeddings ne ON n.id = ne.node_id
		WHERE n.node_type = 'session'
		  AND n.namespace = $1
		  AND n.valid_to IS NULL
		ORDER BY n.created_at DESC
		LIMIT 100
	`, namespace)
	if err != nil {
		respondError(w, 500, "session query failed")
		return
	}
	defer rows.Close()

	var sessions []sessionNode
	for rows.Next() {
		var s sessionNode
		var embStr string
		if err := rows.Scan(&s.ID, &s.Label, &s.Content, &embStr); err != nil {
			continue
		}
		s.Embedding = parseVectorString(embStr)
		sessions = append(sessions, s)
	}

	if len(sessions) == 0 {
		respondJSON(w, 200, map[string]any{
			"namespace": namespace,
			"threshold": threshold,
			"clusters":  []any{},
			"count":     0,
			"note":      "no sessions found",
		})
		return
	}

	// Warn if limit was hit - return early with warning before clustering
	if len(sessions) >= 100 {
		respondJSON(w, 200, map[string]any{
			"namespace":      namespace,
			"threshold":      threshold,
			"clusters":       []any{},
			"count":          0,
			"session_count":  len(sessions),
			"warning":        "limited to 100 most recent sessions for performance",
		})
		return
	}

	// Greedy clustering by cosine similarity
	var clusters []map[string]any
	visited := make(map[string]bool)

	for _, s := range sessions {
		if visited[s.ID] {
			continue
		}

		// Start a new cluster with this session as centroid
		clusterMembers := []sessionNode{s}
		visited[s.ID] = true

		// Find all unvisited sessions similar to this one
		for _, other := range sessions {
			if visited[other.ID] {
				continue
			}
			sim := cosineSimilarity(s.Embedding, other.Embedding)
			if sim >= threshold {
				clusterMembers = append(clusterMembers, other)
				visited[other.ID] = true
			}
		}

		// Compute centroid (average embedding)
		centroid := computeCentroid(clusterMembers)

		// Build member list
		var memberIDs []string
		var memberLabels []string
		for _, m := range clusterMembers {
			memberIDs = append(memberIDs, m.ID)
			memberLabels = append(memberLabels, m.Label)
		}

		clusters = append(clusters, map[string]any{
			"id":               "cluster_" + s.ID[:8],
			"label":            s.Label,
			"member_count":     len(clusterMembers),
			"member_ids":       memberIDs,
			"member_labels":    memberLabels,
			"centroid_preview": centroid[:5], // first 5 dims for brevity
		})
	}

	respondJSON(w, 200, map[string]any{
		"namespace": namespace,
		"threshold": threshold,
		"clusters":  clusters,
		"count":     len(clusters),
	})
}

// parseVectorString parses a pgvector text representation like "[0.1,0.2,...]" into []float64
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

// cosineSimilarity computes cosine similarity between two vectors
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

// computeCentroid computes the average embedding of a set of nodes
func computeCentroid(nodes []sessionNode) []float64 {
	if len(nodes) == 0 {
		return nil
	}
	dim := len(nodes[0].Embedding)
	result := make([]float64, dim)
	for _, n := range nodes {
		for i := 0; i < dim && i < len(n.Embedding); i++ {
			result[i] += n.Embedding[i]
		}
	}
	for i := range result {
		result[i] /= float64(len(nodes))
	}
	return result
}
