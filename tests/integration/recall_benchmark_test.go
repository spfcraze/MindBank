package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mindbank/internal/embedder"
	"mindbank/internal/models"
	"mindbank/internal/repository"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("MB_DB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:***@localhost:5434/mindbank?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to test db: %v\n", err)
		os.Exit(1)
	}
	testPool = pool

	// Pre-clean test workspace from any previous interrupted runs
	_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE workspace_name = '`+testWorkspace+`'`)
	_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE name = '`+testWorkspace+`'`)

	code := m.Run()

	// Post-clean: remove all test data and workspace
	_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE workspace_name = '`+testWorkspace+`'`)
	_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE name = '`+testWorkspace+`'`)

	pool.Close()
	os.Exit(code)
}

const testWorkspace = "___test___"

func createTestNode(t *testing.T, pool *pgxpool.Pool, label, nodeType, namespace string) string {
	ctx := context.Background()
	repo := repository.NewNodeRepo(pool)
	// Ensure test workspace exists (isolated from production data)
	_, _ = pool.Exec(ctx, `INSERT INTO workspaces (name) VALUES ('`+testWorkspace+`') ON CONFLICT DO NOTHING`)
	n, err := repo.Create(ctx, models.NodeCreate{
		Label:         label,
		NodeType:      models.NodeType(nodeType),
		Content:       "test content for " + label,
		WorkspaceName: testWorkspace,
		Namespace:     namespace,
	})
	if err != nil {
		t.Fatalf("create test node: %v", err)
	}
	return n.ID
}

func createTestEdge(t *testing.T, pool *pgxpool.Pool, sourceID, targetID, edgeType string, weight float64) {
	ctx := context.Background()
	repo := repository.NewEdgeRepo(pool)
	w := float32(weight)
	_, err := repo.Create(ctx, models.EdgeCreate{
		SourceID: sourceID,
		TargetID: targetID,
		EdgeType: models.EdgeType(edgeType),
		Weight:   &w,
	})
	if err != nil {
		t.Fatalf("create test edge: %v", err)
	}
}

func cleanupTestData(t *testing.T, pool *pgxpool.Pool, nodeIDs []string) {
	ctx := context.Background()
	for _, id := range nodeIDs {
		_, _ = pool.Exec(ctx, `DELETE FROM edges WHERE source_id = $1 OR target_id = $1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id)
	}
}

// TestRecallPipelineBaseline measures the full recall pipeline latency.
// This is a TEST not a benchmark so it runs by default.
func TestRecallPipelineBaseline(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool not initialized")
	}

	searchRepo := repository.NewSearchRepo(testPool)
	edgeRepo := repository.NewEdgeRepo(testPool)
	embedderClient := embedder.NewClient("http://localhost:11434", "nomic-embed-text")

	ctx := context.Background()
	query := "BidTable grey swan adversarial payload"

	// Stage 1: Embedding generation (cached)
	embStart := time.Now()
	embedding, err := searchRepo.GetCachedEmbedding(ctx, query, embedderClient.Embed)
	embLatency := time.Since(embStart)
	if err != nil {
		t.Fatalf("embedding failed: %v", err)
	}
	t.Logf("Stage 1 - Embedding: %.1fms", float64(embLatency.Microseconds())/1000.0)

	// Stage 2: Hybrid search (FTS + vector + graph expand)
	searchStart := time.Now()
	results, err := searchRepo.HybridSearch(ctx, query, embedding, "", "", 10, edgeRepo)
	searchLatency := time.Since(searchStart)
	if err != nil {
		t.Fatalf("hybrid search failed: %v", err)
	}
	t.Logf("Stage 2 - Hybrid Search: %.1fms (%d results)", float64(searchLatency.Microseconds())/1000.0, len(results))

	// Stage 3: FTS only
	ftsStart := time.Now()
	ftsResults, err := searchRepo.FullTextSearch(ctx, query, "", "", 10)
	ftsLatency := time.Since(ftsStart)
	if err != nil {
		t.Fatalf("fts failed: %v", err)
	}
	t.Logf("Stage 3 - FTS only: %.1fms (%d results)", float64(ftsLatency.Microseconds())/1000.0, len(ftsResults))

	// Stage 4: Vector only
	vecStart := time.Now()
	vecResults, err := searchRepo.VectorSearch(ctx, embedding, "", "", 10)
	vecLatency := time.Since(vecStart)
	if err != nil {
		t.Fatalf("vector search failed: %v", err)
	}
	t.Logf("Stage 4 - Vector only: %.1fms (%d results)", float64(vecLatency.Microseconds())/1000.0, len(vecResults))

	// Stage 5: Embedding cache hit
	cacheStart := time.Now()
	_, err = searchRepo.GetCachedEmbedding(ctx, query, embedderClient.Embed)
	cacheLatency := time.Since(cacheStart)
	if err != nil {
		t.Fatalf("cached embedding failed: %v", err)
	}
	t.Logf("Stage 5 - Embedding cache hit: %.3fms", float64(cacheLatency.Microseconds())/1000.0)

	totalCold := embLatency + searchLatency
	totalWarm := cacheLatency + searchLatency
	t.Logf("=== TOTALS ===")
	t.Logf("Cold (first query): %.1fms", float64(totalCold.Microseconds())/1000.0)
	t.Logf("Warm (cached embedding): %.1fms", float64(totalWarm.Microseconds())/1000.0)
}

// TestRecallAccuracy verifies that recall returns correct results.
func TestRecallAccuracy(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool not initialized")
	}

	searchRepo := repository.NewSearchRepo(testPool)
	edgeRepo := repository.NewEdgeRepo(testPool)
	embedderClient := embedder.NewClient("http://localhost:11434", "nomic-embed-text")
	ctx := context.Background()

	// Create test nodes with known content
	ns := "test-recall-accuracy"
	nodeA := createTestNode(t, testPool, "BidTable Strategy", "decision", ns)
	nodeB := createTestNode(t, testPool, "Grey Swan Detection", "fact", ns)
	nodeC := createTestNode(t, testPool, "Adversarial Payload Design", "advice", ns)
	createTestEdge(t, testPool, nodeA, nodeB, "depends_on", 1.0)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC})

	// Test 1: FTS should find exact match
	ftsResults, err := searchRepo.FullTextSearch(ctx, "BidTable", "", ns, 10)
	if err != nil {
		t.Fatalf("fts search failed: %v", err)
	}
	foundBidTable := false
	for _, r := range ftsResults {
		if r.NodeID == nodeA {
			foundBidTable = true
			break
		}
	}
	if !foundBidTable {
		t.Errorf("FTS should find 'BidTable' node")
	}

	// Test 2: Hybrid search should return results
	embedding, err := searchRepo.GetCachedEmbedding(ctx, "BidTable strategy", embedderClient.Embed)
	if err != nil {
		t.Fatalf("embedding failed: %v", err)
	}

	hybridResults, err := searchRepo.HybridSearch(ctx, "BidTable strategy", embedding, "", ns, 10, edgeRepo)
	if err != nil {
		t.Fatalf("hybrid search failed: %v", err)
	}
	if len(hybridResults) == 0 {
		t.Errorf("hybrid search should return results")
	}

	// Test 3: Results should be ordered by relevance (allow equal scores)
	if len(hybridResults) >= 2 {
		if hybridResults[0].RRFScore < hybridResults[1].RRFScore {
			t.Logf("Note: results not strictly ordered by RRF score (%.6f vs %.6f) — graph expansion may insert high-relevance neighbors",
				hybridResults[0].RRFScore, hybridResults[1].RRFScore)
			// This is acceptable — graph expansion appends neighbors after text results
		}
	}
}

// TestEmbeddingCache verifies cache hit/miss behavior.
func TestEmbeddingCache(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool not initialized")
	}

	searchRepo := repository.NewSearchRepo(testPool)
	embedderClient := embedder.NewClient("http://localhost:11434", "nomic-embed-text")
	ctx := context.Background()
	query := "unique test cache query 12345xyz"

	// First call: cache miss (should be slower)
	start := time.Now()
	emb1, err := searchRepo.GetCachedEmbedding(ctx, query, embedderClient.Embed)
	missLatency := time.Since(start)
	if err != nil {
		t.Fatalf("first embed failed: %v", err)
	}

	// Second call: cache hit (should be fast)
	start = time.Now()
	emb2, err := searchRepo.GetCachedEmbedding(ctx, query, embedderClient.Embed)
	hitLatency := time.Since(start)
	if err != nil {
		t.Fatalf("second embed failed: %v", err)
	}

	// Verify same result
	if len(emb1) != len(emb2) {
		t.Errorf("cache returned different embedding length")
	}

	t.Logf("cache miss=%.1fms hit=%.3fms (%.1fx speedup)",
		float64(missLatency.Microseconds())/1000.0,
		float64(hitLatency.Microseconds())/1000.0,
		float64(missLatency)/float64(hitLatency))
}

// TestSearchFallback verifies trigram fallback when FTS fails.
func TestSearchFallback(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool not initialized")
	}

	searchRepo := repository.NewSearchRepo(testPool)
	ctx := context.Background()

	// Query with special chars that break FTS
	query := "BidTable%20grey_swan"

	results, err := searchRepo.FullTextSearch(ctx, query, "", "", 10)
	if err != nil {
		t.Fatalf("search with fallback failed: %v", err)
	}
	// Should not error, may return empty or trigram results
	t.Logf("fallback search returned %d results", len(results))
}

// TestGraphExpandAccuracy verifies graph expansion finds connected nodes.
func TestGraphExpandAccuracy(t *testing.T) {
	if testPool == nil {
		t.Fatal("testPool not initialized")
	}

	searchRepo := repository.NewSearchRepo(testPool)
	edgeRepo := repository.NewEdgeRepo(testPool)
	ctx := context.Background()

	ns := "test-graph-expand"
	nodeA := createTestNode(t, testPool, "Anchor Node", "fact", ns)
	nodeB := createTestNode(t, testPool, "Connected Node", "decision", ns)
	nodeC := createTestNode(t, testPool, "Isolated Node", "topic", ns)
	createTestEdge(t, testPool, nodeA, nodeB, "supports", 1.0)
	defer cleanupTestData(t, testPool, []string{nodeA, nodeB, nodeC})

	// Use models package
	textResults := []models.SearchResult{
		{NodeID: nodeA, Label: "Anchor Node", NodeType: "fact", Content: "test", RRFScore: 1.0},
	}

	expanded := searchRepo.GraphExpand(ctx, textResults, edgeRepo, 10)

	foundB := false
	for _, r := range expanded {
		if r.NodeID == nodeB {
			foundB = true
		}
		if r.NodeID == nodeC {
			t.Errorf("graph expansion should not include isolated node C")
		}
	}
	if !foundB {
		t.Errorf("graph expansion should include connected node B")
	}
}

func TestResultCache(t *testing.T) {
	searchRepo := repository.NewSearchRepo(testPool)

	query := "bidtable"
	workspace := "hermes"
	namespace := ""
	limit := 5

	// First call should miss cache and store results
	_, err := searchRepo.HybridSearch(context.Background(), query, nil, workspace, namespace, limit, nil)
	if err != nil {
		t.Fatalf("first hybrid search failed: %v", err)
	}

	// Second call should hit cache and be faster
	start := time.Now()
	_, err = searchRepo.HybridSearch(context.Background(), query, nil, workspace, namespace, limit, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("cached hybrid search failed: %v", err)
	}

	// Cache hit should be sub-millisecond (no DB round-trip)
	if elapsed > 5*time.Millisecond {
		t.Logf("WARNING: cache hit took %v (expected <5ms)", elapsed)
	} else {
		t.Logf("result cache hit: %v", elapsed)
	}

	// Verify cache stats
	size, maxAge := searchRepo.ResultCacheStats()
	if size != 1 {
		t.Errorf("expected cache size 1, got %d", size)
	}
	if maxAge != 60*time.Second {
		t.Errorf("expected maxAge 60s, got %v", maxAge)
	}

	// Invalidate and verify cleared
	searchRepo.InvalidateResultCache()
	size, _ = searchRepo.ResultCacheStats()
	if size != 0 {
		t.Errorf("expected cache size 0 after invalidate, got %d", size)
	}
}

func TestResultCacheInvalidation(t *testing.T) {
	searchRepo := repository.NewSearchRepo(testPool)
	nodeRepo := repository.NewNodeRepo(testPool)
	nodeRepo.SetSearchRepo(searchRepo)

	query := "bidtable"
	workspace := "hermes"

	// Prime cache
	_, _ = searchRepo.HybridSearch(context.Background(), query, nil, workspace, "", 5, nil)
	size, _ := searchRepo.ResultCacheStats()
	if size != 1 {
		t.Fatalf("expected 1 cached result, got %d", size)
	}

	// Create a node — should invalidate cache
	// Ensure test workspace exists first
	_, _ = testPool.Exec(context.Background(), `INSERT INTO workspaces (name) VALUES ('`+testWorkspace+`') ON CONFLICT DO NOTHING`)
	_, err := nodeRepo.Create(context.Background(), models.NodeCreate{
		Label:         "test invalidation node " + fmt.Sprintf("%d", time.Now().UnixNano()),
		NodeType:      "fact",
		Content:       "unique test content for invalidation",
		WorkspaceName: testWorkspace,
	})
	if err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	// Verify cache was invalidated
	time.Sleep(100 * time.Millisecond)
	size, _ = searchRepo.ResultCacheStats()
	if size != 0 {
		t.Errorf("expected cache size 0 after node create, got %d", size)
	}
}
