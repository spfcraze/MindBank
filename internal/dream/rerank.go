package dream

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
)

// TokenReranker implements late-interaction token-level reranking.
// Equivalent to ColBERT but using MindBank's naming.
type TokenReranker struct {
	pool DBTX
}

// NewTokenReranker creates a token reranker.
func NewTokenReranker(pool DBTX) *TokenReranker {
	return &TokenReranker{pool: pool}
}

// RerankResult represents a reranked search result.
type RerankResult struct {
	NodeID     string
	Label      string
	Score      float64
	TokenScore float64
	OriginalRank int
}

// Rerank reorders top-K candidates using token-level similarity.
// query: search query string
// candidates: initial top-K results from vector search
// Returns: reranked results sorted by token-level score.
func (tr *TokenReranker) Rerank(ctx context.Context, query string, candidates []string) ([]RerankResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	// Tokenize query
	queryTokens := tr.tokenize(query)
	if len(queryTokens) == 0 {
		return nil, fmt.Errorf("empty query after tokenization")
	}

	// Get query token embeddings (using node_embeddings as proxy)
	queryEmbedding, err := tr.getQueryEmbedding(ctx, query)
	if err != nil {
		slog.Debug("query embedding failed, using fallback", "error", err)
		queryEmbedding = nil
	}

	// Score each candidate
	results := make([]RerankResult, 0, len(candidates))
	for i, nodeID := range candidates {
		score, err := tr.scoreTokenSimilarity(ctx, nodeID, queryTokens, queryEmbedding)
		if err != nil {
			slog.Debug("token scoring failed", "node", nodeID, "error", err)
			continue
		}

		// Get node label
		var label string
		tr.pool.QueryRow(ctx, `SELECT label FROM nodes WHERE id = $1 AND valid_to IS NULL`, nodeID).Scan(&label)

		results = append(results, RerankResult{
			NodeID:       nodeID,
			Label:        label,
			Score:        score,
			OriginalRank: i + 1,
		})
	}

	// Sort by score descending
	tr.sortResults(results)

	return results, nil
}

// tokenize splits text into tokens.
func (tr *TokenReranker) tokenize(text string) []string {
	// Simple whitespace tokenization with normalization
	text = strings.ToLower(text)
	text = strings.ReplaceAll(text, ".", " ")
	text = strings.ReplaceAll(text, ",", " ")
	text = strings.ReplaceAll(text, "!", " ")
	text = strings.ReplaceAll(text, "?", " ")
	text = strings.ReplaceAll(text, ";", " ")
	text = strings.ReplaceAll(text, ":", " ")
	
	tokens := strings.Fields(text)
	
	// Remove stopwords
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true, "can": true,
		"need": true, "dare": true, "ought": true, "used": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "between": true,
		"under": true, "and": true, "but": true, "or": true, "yet": true,
		"so": true, "if": true, "because": true, "although": true,
		"though": true, "while": true, "where": true, "when": true,
		"that": true, "which": true, "who": true, "whom": true,
		"whose": true, "what": true, "this": true, "these": true,
		"those": true, "i": true, "me": true, "my": true, "mine": true,
		"myself": true, "you": true, "your": true, "yours": true,
		"yourself": true, "he": true, "him": true, "his": true,
		"himself": true, "she": true, "her": true, "hers": true,
		"herself": true, "it": true, "its": true, "itself": true,
		"we": true, "us": true, "our": true, "ours": true,
		"ourselves": true, "they": true, "them": true, "their": true,
		"theirs": true, "themselves": true,
	}
	
	var filtered []string
	for _, t := range tokens {
		if !stopwords[t] && len(t) > 2 {
			filtered = append(filtered, t)
		}
	}
	
	return filtered
}

// getQueryEmbedding gets the embedding for a query string.
func (tr *TokenReranker) getQueryEmbedding(ctx context.Context, query string) ([]float32, error) {
	// Try to find existing embedding for similar text
	var embeddingText string
	err := tr.pool.QueryRow(ctx, `
		SELECT embedding::text
		FROM node_embeddings
		WHERE content = $1
		LIMIT 1
	`, query).Scan(&embeddingText)
	if err != nil {
		return nil, err
	}
	return parseVector(embeddingText), nil
}

// scoreTokenSimilarity computes token-level similarity between query and node.
func (tr *TokenReranker) scoreTokenSimilarity(ctx context.Context, nodeID string, queryTokens []string, queryEmbedding []float32) (float64, error) {
	// Get node content
	var content string
	err := tr.pool.QueryRow(ctx, `
		SELECT COALESCE(content, '') || ' ' || COALESCE(label, '') || ' ' || COALESCE(summary, '')
		FROM nodes WHERE id = $1 AND valid_to IS NULL
	`, nodeID).Scan(&content)
	if err != nil {
		return 0, err
	}

	// Tokenize node content
	nodeTokens := tr.tokenize(content)
	if len(nodeTokens) == 0 {
		return 0, nil
	}

	// Compute MaxSim (ColBERT-style late interaction)
	score := tr.maxSim(queryTokens, nodeTokens)

	// If we have embeddings, add cosine similarity as bonus
	if queryEmbedding != nil {
		nodeEmbedding, err := tr.getNodeEmbedding(ctx, nodeID)
		if err == nil && nodeEmbedding != nil {
			cosine := tr.cosineSimilarity(queryEmbedding, nodeEmbedding)
			score = 0.7*score + 0.3*cosine // Blend token + vector
		}
	}

	return score, nil
}

// maxSim computes MaxSim score between two token sets.
func (tr *TokenReranker) maxSim(queryTokens, nodeTokens []string) float64 {
	if len(queryTokens) == 0 || len(nodeTokens) == 0 {
		return 0
	}

	// Build node token frequency map
	nodeFreq := make(map[string]int)
	for _, t := range nodeTokens {
		nodeFreq[t]++
	}

	// For each query token, find best matching node token
	totalScore := 0.0
	for _, qt := range queryTokens {
		bestMatch := 0.0
		for nt, count := range nodeFreq {
			sim := tr.tokenSimilarity(qt, nt)
			if sim > bestMatch {
				bestMatch = sim
			}
			// Boost for frequency
			if sim > 0.8 {
				bestMatch += float64(count-1) * 0.05
			}
		}
		totalScore += bestMatch
	}

	return totalScore / float64(len(queryTokens))
}

// tokenSimilarity computes similarity between two tokens.
func (tr *TokenReranker) tokenSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	
	// Check for substring match
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 0.8
	}
	
	// Check for prefix/suffix match
	if len(a) > 3 && len(b) > 3 {
		if strings.HasPrefix(a, b[:3]) || strings.HasPrefix(b, a[:3]) {
			return 0.6
		}
	}
	
	return 0.0
}

// getNodeEmbedding gets the embedding for a node.
func (tr *TokenReranker) getNodeEmbedding(ctx context.Context, nodeID string) ([]float32, error) {
	var embeddingText string
	err := tr.pool.QueryRow(ctx, `
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

// cosineSimilarity computes cosine similarity between two vectors.
func (tr *TokenReranker) cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	
	dot := 0.0
	normA := 0.0
	normB := 0.0
	
	for i := 0; i < len(a); i++ {
		dot += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	
	if normA == 0 || normB == 0 {
		return 0
	}
	
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// sortResults sorts results by score descending.
func (tr *TokenReranker) sortResults(results []RerankResult) {
	// Simple bubble sort (small arrays)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// SearchAndRerank performs vector search then reranks with token-level scoring.
func (tr *TokenReranker) SearchAndRerank(ctx context.Context, query string, topK int) ([]RerankResult, error) {
	// Step 1: Vector search (top-100 candidates)
	candidates, err := tr.vectorSearch(ctx, query, 100)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// Step 2: Token rerank (top-K)
	results, err := tr.Rerank(ctx, query, candidates)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}

	// Return top-K
	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// vectorSearch finds top candidates using pgvector similarity.
func (tr *TokenReranker) vectorSearch(ctx context.Context, query string, limit int) ([]string, error) {
	// Get query embedding (reuse from node_embeddings if exact match exists)
	var queryEmbeddingText string
	err := tr.pool.QueryRow(ctx, `
		SELECT embedding::text
		FROM node_embeddings
		WHERE content = $1
		LIMIT 1
	`, query).Scan(&queryEmbeddingText)
	queryEmbedding := parseVector(queryEmbeddingText)

	if err != nil || queryEmbedding == nil {
		// Fallback: use text search
		rows, err := tr.pool.Query(ctx, `
			SELECT id
			FROM nodes
			WHERE valid_to IS NULL
			  AND search_vector @@ plainto_tsquery('english', $1)
			ORDER BY ts_rank_cd(search_vector, plainto_tsquery('english', $1)) DESC
			LIMIT $2
		`, query, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		
		var candidates []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				continue
			}
			candidates = append(candidates, id)
		}
		return candidates, nil
	}

	// Use vector search, restricted to current nodes: ghost embeddings of
	// superseded versions would otherwise occupy candidate slots.
	rows, err := tr.pool.Query(ctx, `
		SELECT ne.node_id
		FROM node_embeddings ne
		JOIN nodes n ON n.id = ne.node_id AND n.valid_to IS NULL
		ORDER BY ne.embedding <=> $1::vector
		LIMIT $2
	`, vectorLiteral(queryEmbedding), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		candidates = append(candidates, id)
	}
	return candidates, nil
}
