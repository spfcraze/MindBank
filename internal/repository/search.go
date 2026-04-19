package repository

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"mindbank/internal/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SearchRepo struct {
	pool   *pgxpool.Pool
	cache  *EmbeddingCache
}

func NewSearchRepo(pool *pgxpool.Pool) *SearchRepo {
	return &SearchRepo{
		pool:  pool,
		cache: NewEmbeddingCache(),
	}
}

// FullTextSearch performs PostgreSQL FTS using ts_rank_cd with synonym expansion.
func (r *SearchRepo) FullTextSearch(ctx context.Context, query string, workspace, namespace string, limit int) ([]models.SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	// Expand query with synonyms
	expandedQuery, terms := ExpandQuery(query)

	// Try websearch first with expanded query
	rows, err := r.pool.Query(ctx, `
		SELECT id, label, node_type::text, content, namespace,
		       ts_rank_cd(search_vector, websearch_to_tsquery('english', $1)) AS rank
		FROM nodes
		WHERE valid_to IS NULL
		  AND search_vector @@ websearch_to_tsquery('english', $1)
		  AND ($2 = '' OR workspace_name = $2)
		  AND ($3 = '' OR namespace = $3)
		ORDER BY rank DESC
		LIMIT $4
	`, expandedQuery, workspace, namespace, limit)
	if err != nil {
		// Fallback: use plainto_tsquery (more lenient)
		rows, err = r.pool.Query(ctx, `
			SELECT id, label, node_type::text, content, namespace,
			       ts_rank_cd(search_vector, plainto_tsquery('english', $1)) AS rank
			FROM nodes
			WHERE valid_to IS NULL
			  AND search_vector @@ plainto_tsquery('english', $1)
			  AND ($2 = '' OR workspace_name = $2)
			  AND ($3 = '' OR namespace = $3)
			ORDER BY rank DESC
			LIMIT $4
		`, expandedQuery, workspace, namespace, limit)
		if err != nil {
			// FTS can't parse this query (special chars, pure unicode, etc.)
			// Skip straight to trigram — don't error out
			slog.Debug("fts query unparseable, falling back to trigram", "query", query)
			return r.trigramSearch(ctx, query, workspace, namespace, limit)
		}
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var sr models.SearchResult
		if err := rows.Scan(&sr.NodeID, &sr.Label, &sr.NodeType, &sr.Content, &sr.Namespace, &sr.FTSScore); err != nil {
			return nil, fmt.Errorf("scan fts result: %w", err)
		}
		results = append(results, sr)
	}

	// If FTS returned nothing, try trigram with original + expanded terms
	if len(results) == 0 {
		// Try each synonym term
		for _, term := range terms {
			trigResults, err := r.trigramSearch(ctx, term, workspace, namespace, limit)
			if err == nil && len(trigResults) > 0 {
				results = append(results, trigResults...)
				if len(results) >= limit {
					break
				}
			}
		}
		if len(results) == 0 {
			return r.trigramSearch(ctx, query, workspace, namespace, limit)
		}
	}

	return results, nil
}

// trigramSearch uses pg_trgm similarity as fallback for FTS misses.
func (r *SearchRepo) trigramSearch(ctx context.Context, query string, workspace, namespace string, limit int) ([]models.SearchResult, error) {
	// Strip non-alphanumeric chars for trigram matching (pg_trgm chokes on %, _, etc.)
	cleanQuery := sanitizeForTrigram(query)
	if cleanQuery == "" {
		// Nothing usable — return empty, not error
		return []models.SearchResult{}, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, label, node_type::text, content, namespace,
		       GREATEST(
		           similarity(lower(label), lower($1)),
		           similarity(lower(content), lower($1)),
		           similarity(lower(coalesce(summary,'')), lower($1))
		       ) AS sim
		FROM nodes
		WHERE valid_to IS NULL
		  AND ($2 = '' OR workspace_name = $2)
		  AND ($3 = '' OR namespace = $3)
		  AND (
		      lower(label) % lower($1)
		      OR lower(content) % lower($1)
		      OR lower(coalesce(summary,'')) % lower($1)
		      OR lower(label) LIKE '%' || lower($1) || '%'
		      OR lower(content) LIKE '%' || lower($1) || '%'
		  )
		ORDER BY sim DESC
		LIMIT $4
	`, cleanQuery, workspace, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("trigram search: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var sr models.SearchResult
		if err := rows.Scan(&sr.NodeID, &sr.Label, &sr.NodeType, &sr.Content, &sr.Namespace, &sr.FTSScore); err != nil {
			return nil, fmt.Errorf("scan trigram result: %w", err)
		}
		results = append(results, sr)
	}
	return results, nil
}

// VectorSearch performs semantic search using pgvector cosine similarity.
func (r *SearchRepo) VectorSearch(ctx context.Context, embedding []float32, workspace, namespace string, limit int) ([]models.SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	vecStr := vectorToLiteral(embedding)

	rows, err := r.pool.Query(ctx, `
		SELECT n.id, n.label, n.node_type::text, n.content, n.namespace,
		       1 - (ne.embedding <=> $1::vector) AS similarity
		FROM node_embeddings ne
		JOIN nodes n ON n.id = ne.node_id
		WHERE n.valid_to IS NULL
		  AND ($2 = '' OR n.workspace_name = $2)
		  AND ($3 = '' OR n.namespace = $3)
		ORDER BY ne.embedding <=> $1::vector
		LIMIT $4
	`, vecStr, workspace, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var sr models.SearchResult
		if err := rows.Scan(&sr.NodeID, &sr.Label, &sr.NodeType, &sr.Content, &sr.Namespace, &sr.VecScore); err != nil {
			return nil, fmt.Errorf("scan vec result: %w", err)
		}
		results = append(results, sr)
	}
	return results, nil
}

// GetCachedEmbedding returns a cached embedding or fetches a new one.
func (r *SearchRepo) GetCachedEmbedding(ctx context.Context, query string, embFn func(context.Context, string) ([]float32, error)) ([]float32, error) {
	// Check cache first
	if cached, ok := r.cache.Get(query); ok {
		return cached, nil
	}
	
	// Fetch from embedder
	embedding, err := embFn(ctx, query)
	if err != nil {
		return nil, err
	}
	
	// Cache the result
	r.cache.Set(query, embedding)
	return embedding, nil
}

// CacheStats returns embedding cache statistics.
func (r *SearchRepo) CacheStats() (size int, maxAge time.Duration) {
	return r.cache.Stats()
}

// HybridSearch combines FTS + vector results using Reciprocal Rank Fusion.
func (r *SearchRepo) HybridSearch(ctx context.Context, query string, embedding []float32, workspace, namespace string, limit int, edgeRepo *EdgeRepo) ([]models.SearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	// Fetch more candidates for RRF fusion
	candidateLimit := limit * 3

	ftsResults, err := r.FullTextSearch(ctx, query, workspace, namespace, candidateLimit)
	if err != nil {
		slog.Warn("fts failed in hybrid, falling back to vector only", "error", err)
		ftsResults = nil
	}

	vecResults, err := r.VectorSearch(ctx, embedding, workspace, namespace, candidateLimit)
	if err != nil {
		slog.Warn("vector failed in hybrid, falling back to fts only", "error", err)
		vecResults = nil
	}

	// If both failed, try trigram fallback before giving up
	if ftsResults == nil && vecResults == nil {
		slog.Warn("both fts and vector failed, trying trigram fallback")
		trigramResults, err := r.trigramSearch(ctx, query, workspace, namespace, limit)
		if err == nil && len(trigramResults) > 0 {
			return trigramResults, nil
		}
		// No results from any method — return empty, not error
		return []models.SearchResult{}, nil
	}

	// RRF fusion: score = sum(1 / (k + rank_i)) where k=60
	const k = 60.0
	scores := make(map[string]float32)
	labelMap := make(map[string]string)
	typeMap := make(map[string]string)
	contentMap := make(map[string]string)
	nsMap := make(map[string]string)

	for rank, r := range ftsResults {
		scores[r.NodeID] += float32(1.0 / (k + float64(rank+1)))
		labelMap[r.NodeID] = r.Label
		typeMap[r.NodeID] = r.NodeType
		contentMap[r.NodeID] = r.Content
		nsMap[r.NodeID] = r.Namespace
	}

	for rank, r := range vecResults {
		scores[r.NodeID] += float32(1.0 / (k + float64(rank+1)))
		if _, exists := labelMap[r.NodeID]; !exists {
			labelMap[r.NodeID] = r.Label
			typeMap[r.NodeID] = r.NodeType
			contentMap[r.NodeID] = r.Content
			nsMap[r.NodeID] = r.Namespace
		}
	}

	// Namespace boost: when a namespace filter is set, boost matching nodes by 50%
	// This prevents cross-namespace results from outranking project-specific ones
	if namespace != "" {
		boostFactor := float32(1.5)
		for id, ns := range nsMap {
			if ns == namespace {
				scores[id] *= boostFactor
			}
		}
	}

	// Sort by RRF score
	type scoredNode struct {
		id    string
		score float32
	}
	var sorted []scoredNode
	for id, score := range scores {
		sorted = append(sorted, scoredNode{id, score})
	}
	// Simple sort (descending)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].score > sorted[i].score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Take top N
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}

	var results []models.SearchResult
	for _, s := range sorted {
		results = append(results, models.SearchResult{
			NodeID:    s.id,
			Label:     labelMap[s.id],
			NodeType:  typeMap[s.id],
			Content:   contentMap[s.id],
			Namespace: nsMap[s.id],
			RRFScore:  s.score,
		})
	}

	// Graph expansion: boost results with graph-connected nodes
	results = r.GraphExpand(ctx, results, edgeRepo, limit)

	return results, nil
}

// ImportanceScore computes a multi-factor importance score for a node.
func (r *SearchRepo) ImportanceScore(ctx context.Context, nodeID string) (float32, error) {
	var result float32
	err := r.pool.QueryRow(ctx, `
		SELECT (
			-- Recency: 30-day decay (30%)
			0.30 * COALESCE(
				1.0 - EXTRACT(EPOCH FROM (now() - n.last_accessed)) / 2592000.0,
				0.5
			)::real
			-- Frequency: normalized access count (25%)
			+ 0.25 * LEAST(n.access_count::real / 100.0, 1.0)::real
			-- Connectivity: edge count normalized (20%)
			+ 0.20 * LEAST(
				(SELECT COUNT(*)::real / 20.0 FROM edges
				 WHERE source_id = n.id OR target_id = n.id),
				1.0
			)::real
			-- Explicit importance (15%)
			+ 0.15 * n.importance
			-- Type weight (10%)
			+ 0.10 * CASE n.node_type
				WHEN 'decision'   THEN 1.0
				WHEN 'preference' THEN 0.9
				WHEN 'problem'    THEN 0.9
				WHEN 'advice'     THEN 0.8
				WHEN 'fact'       THEN 0.7
				WHEN 'person'     THEN 0.7
				WHEN 'project'    THEN 0.7
				WHEN 'event'      THEN 0.5
				WHEN 'topic'      THEN 0.4
				WHEN 'concept'    THEN 0.3
				ELSE 0.5
			END::real
		)::real AS score
		FROM nodes n
		WHERE n.id = $1 AND n.valid_to IS NULL
	`, nodeID).Scan(&result)
	if err != nil {
		return 0, fmt.Errorf("importance score: %w", err)
	}
	return float32(math.Round(float64(result)*1000) / 1000), nil
}

// GraphExpand expands search results by finding 1-hop graph neighbors of top text results.
// Neighbors are scored and merged into the result set with a bias toward text results.
func (r *SearchRepo) GraphExpand(ctx context.Context, textResults []models.SearchResult, edgeRepo *EdgeRepo, limit int) []models.SearchResult {
	if len(textResults) == 0 || edgeRepo == nil {
		return textResults
	}

	// Take top 5 text results as expansion anchors
	anchorCount := 5
	if len(textResults) < anchorCount {
		anchorCount = len(textResults)
	}
	anchors := textResults[:anchorCount]

	// Collect anchor node IDs
	anchorIDs := make([]string, anchorCount)
	for i, a := range anchors {
		anchorIDs[i] = a.NodeID
	}

	// Build a set of existing result IDs to avoid duplicates
	existing := make(map[string]bool)
	for _, r := range textResults {
		existing[r.NodeID] = true
	}

	// Batch lookup neighbors
	neighborsByAnchor, err := edgeRepo.GetNeighborsByNodeIDs(ctx, anchorIDs)
	if err != nil {
		slog.Warn("graph expand: neighbor lookup failed, returning text results", "error", err)
		return textResults
	}

	// Build anchor relevance lookup (use RRF score as proxy for relevance)
	anchorRelevance := make(map[string]float32)
	for _, a := range anchors {
		score := a.RRFScore
		if score == 0 {
			score = a.FTSScore
		}
		if score == 0 {
			score = a.VecScore
		}
		if score == 0 {
			score = 1.0 // fallback: assume some relevance for anchors
		}
		anchorRelevance[a.NodeID] = score
	}

	// Normalize anchor relevance scores to [0,1]
	var maxRel float32
	for _, v := range anchorRelevance {
		if v > maxRel {
			maxRel = v
		}
	}
	if maxRel > 0 {
		for k, v := range anchorRelevance {
			anchorRelevance[k] = v / maxRel
		}
	}

	// Collect unique expanded neighbors with their best score
	type expandedNode struct {
		result models.SearchResult
		score  float32
	}
	expanded := make(map[string]expandedNode) // dedup by node ID

	for anchorID, neighbors := range neighborsByAnchor {
		anchorRel := anchorRelevance[anchorID]

		for _, nw := range neighbors {
			// Skip nodes already in results
			if existing[nw.ID] {
				continue
			}

			// graph_score = edge_weight * 0.5 + importance * 0.3 + (1.0/access_count_boost) * 0.2
			accessBoost := float32(nw.AccessCount)
			if accessBoost < 1 {
				accessBoost = 1
			}
			graphScore := nw.EdgeWeight*0.5 + nw.Importance*0.3 + (1.0/accessBoost)*0.2

			// final_score = graph_score * (0.3 + 0.7 * anchor_relevance_score)
			finalScore := graphScore * (0.3 + 0.7*anchorRel)

			// Keep the best score if this neighbor is reachable from multiple anchors
			if prev, ok := expanded[nw.ID]; ok {
				if finalScore > prev.score {
					expanded[nw.ID] = expandedNode{
						result: models.SearchResult{
							NodeID:    nw.ID,
							Label:     nw.Label,
							NodeType:  string(nw.NodeType),
							Content:   nw.Content,
							Namespace: nw.Namespace,
							RRFScore:  finalScore,
						},
						score: finalScore,
					}
				}
			} else {
				expanded[nw.ID] = expandedNode{
					result: models.SearchResult{
						NodeID:    nw.ID,
						Label:     nw.Label,
						NodeType:  string(nw.NodeType),
						Content:   nw.Content,
						Namespace: nw.Namespace,
						RRFScore:  finalScore,
					},
					score: finalScore,
				}
			}
		}
	}

	if len(expanded) == 0 {
		return textResults
	}

	// Sort expanded results by score DESC
	var sortedExpanded []expandedNode
	for _, en := range expanded {
		sortedExpanded = append(sortedExpanded, en)
	}
	sort.Slice(sortedExpanded, func(i, j int) bool {
		return sortedExpanded[i].score > sortedExpanded[j].score
	})

	// Take top (limit/2) expanded results, min 3
	maxExpanded := limit / 2
	if maxExpanded < 3 {
		maxExpanded = 3
	}
	if len(sortedExpanded) > maxExpanded {
		sortedExpanded = sortedExpanded[:maxExpanded]
	}

	// Merge: text results first, then graph-expanded results
	merged := make([]models.SearchResult, 0, len(textResults)+len(sortedExpanded))
	merged = append(merged, textResults...)
	for _, en := range sortedExpanded {
		merged = append(merged, en.result)
	}

	return merged
}

func vectorToLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, f := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%g", f)
	}
	s += "]"
	return s
}

// sanitizeForTrigram strips characters that break pg_trgm queries.
// Keeps alphanumeric, spaces, and common punctuation. Removes %, _, etc.
func sanitizeForTrigram(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == ' ' || r == '-' || r == '.' || r == ',' || r == '\'' || r > 127 {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
